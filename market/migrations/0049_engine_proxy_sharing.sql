-- Sharing is an IdleCat policy, not an engine schema extension. Reuse existing
-- market proxy bindings so legacy upstreams keep their proxy_id unchanged.
CREATE TABLE engine_proxy_sharing (
    proxy_id bigint PRIMARY KEY,
    market_proxy_id bigint NOT NULL UNIQUE REFERENCES proxies(id),
    is_shared boolean NOT NULL DEFAULT false,
    connection_report jsonb,
    connection_checked_at timestamptz,
    engine_updated_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO engine_proxy_sharing(proxy_id, market_proxy_id, is_shared)
SELECT sub2api_proxy_id, id, true FROM proxies
WHERE seller_id IS NULL AND is_shared AND sub2api_proxy_id IS NOT NULL;

CREATE OR REPLACE FUNCTION guard_upstream_proxy_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    old_proxy_id bigint;
    owner_id bigint;
    proxy_status text;
    legacy_shared boolean;
    engine_shared boolean;
BEGIN
    IF TG_OP <> 'INSERT' THEN old_proxy_id := OLD.proxy_id; END IF;
    IF TG_OP <> 'DELETE' AND NEW.status = 'removed' THEN NEW.proxy_id := NULL; END IF;
    IF TG_OP = 'DELETE' THEN
        PERFORM id FROM proxies WHERE id=old_proxy_id FOR UPDATE;
        RETURN OLD;
    END IF;
    PERFORM id FROM proxies WHERE id IN (old_proxy_id,NEW.proxy_id) ORDER BY id FOR UPDATE;
    IF NEW.proxy_id IS NOT NULL AND (TG_OP='INSERT' OR NEW.proxy_id IS DISTINCT FROM old_proxy_id OR NEW.seller_id IS DISTINCT FROM OLD.seller_id) THEN
        SELECT seller_id,status,is_shared INTO owner_id,proxy_status,legacy_shared FROM proxies WHERE id=NEW.proxy_id;
        IF NOT FOUND THEN RAISE EXCEPTION 'proxy not found' USING ERRCODE='23503'; END IF;
        IF proxy_status='disabled' THEN RAISE EXCEPTION 'proxy is disabled' USING ERRCODE='23514'; END IF;
        IF owner_id IS NOT NULL AND owner_id<>NEW.seller_id THEN RAISE EXCEPTION 'proxy belongs to another seller' USING ERRCODE='23514'; END IF;
        SELECT EXISTS(SELECT 1 FROM engine_proxy_sharing WHERE market_proxy_id=NEW.proxy_id AND is_shared) INTO engine_shared;
        IF owner_id IS NULL AND NOT legacy_shared AND NOT engine_shared AND EXISTS (
            SELECT 1 FROM upstreams WHERE proxy_id=NEW.proxy_id AND status<>'removed' AND id<>NEW.id
        ) THEN RAISE EXCEPTION 'platform proxy is already assigned' USING ERRCODE='23505'; END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION guard_proxy_policy_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    active_bindings int;
    engine_shared boolean;
BEGIN
    SELECT count(*) INTO active_bindings FROM upstreams WHERE proxy_id=OLD.id AND status<>'removed';
    IF TG_OP='DELETE' THEN
        IF active_bindings>0 THEN RAISE EXCEPTION 'remove all active upstream bindings before deleting proxy' USING ERRCODE='23514'; END IF;
        RETURN OLD;
    END IF;
    IF NEW.status='disabled' AND OLD.status<>'disabled' AND active_bindings>0 THEN
        RAISE EXCEPTION 'remove all active upstream bindings before disabling proxy' USING ERRCODE='23514';
    END IF;
    SELECT EXISTS(SELECT 1 FROM engine_proxy_sharing WHERE market_proxy_id=OLD.id AND is_shared) INTO engine_shared;
    IF NEW.seller_id IS NULL AND NOT NEW.is_shared AND NOT engine_shared AND active_bindings>1 THEN
        RAISE EXCEPTION 'remove all but one active upstream before disabling proxy sharing' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION guard_engine_proxy_sharing_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (TG_OP = 'DELETE' OR NOT NEW.is_shared OR NEW.market_proxy_id <> OLD.market_proxy_id)
       AND EXISTS(SELECT 1 FROM upstreams WHERE proxy_id=OLD.market_proxy_id AND status<>'removed') THEN
        RAISE EXCEPTION 'remove all active upstream bindings before disabling engine proxy sharing' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER engine_proxy_sharing_mutation_guard BEFORE UPDATE OF is_shared,market_proxy_id OR DELETE
ON engine_proxy_sharing FOR EACH ROW EXECUTE FUNCTION guard_engine_proxy_sharing_mutation();

-- Clear only mapped legacy bits. Un-synced legacy proxies with multiple active
-- bindings retain their legacy bit until those bindings are removed.
UPDATE proxies p SET is_shared=false WHERE EXISTS (
    SELECT 1 FROM engine_proxy_sharing e WHERE e.market_proxy_id=p.id
);

-- Unsynced legacy shared rows are retained but no longer affect new candidate
-- selection. If multiple active bindings exist, keep their legacy bit so the
-- trigger does not reject migration; single-bound/unused rows can be cleared.
UPDATE proxies p SET is_shared=false WHERE seller_id IS NULL AND is_shared AND sub2api_proxy_id IS NULL
AND (SELECT count(*) FROM upstreams u WHERE u.proxy_id=p.id AND u.status<>'removed')<=1;
