-- Move the legacy global seller-proxy policy to each vendor and enable a
-- separately managed, shareable platform proxy pool.
ALTER TABLE vendor_configs
    ADD COLUMN require_seller_proxy boolean NOT NULL DEFAULT true;

DO $$
BEGIN
    UPDATE vendor_configs
       SET require_seller_proxy = COALESCE(
           (SELECT (value->>'require_seller_proxy')::boolean FROM settings WHERE key = 'platform'),
           true
       );
END $$;

UPDATE settings
   SET value = value - 'require_seller_proxy', updated_at = now()
 WHERE key = 'platform' AND value ? 'require_seller_proxy';

ALTER TABLE proxies
    ADD COLUMN is_shared boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT proxies_shared_platform_only CHECK (seller_id IS NULL OR NOT is_shared);

CREATE TABLE shared_proxy_rotation (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_proxy_id bigint,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO shared_proxy_rotation (id) VALUES (1);

CREATE OR REPLACE FUNCTION guard_upstream_proxy_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    old_proxy_id bigint;
    owner_id bigint;
    proxy_status text;
    shared boolean;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_proxy_id := OLD.proxy_id;
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.status = 'removed' THEN
        NEW.proxy_id := NULL;
    END IF;
    IF TG_OP = 'DELETE' THEN
        PERFORM id FROM proxies WHERE id = old_proxy_id FOR UPDATE;
        RETURN OLD;
    END IF;
    PERFORM id FROM proxies WHERE id IN (old_proxy_id, NEW.proxy_id) ORDER BY id FOR UPDATE;
    IF NEW.proxy_id IS NOT NULL AND
        (TG_OP = 'INSERT' OR NEW.proxy_id IS DISTINCT FROM old_proxy_id OR NEW.seller_id IS DISTINCT FROM OLD.seller_id) THEN
        SELECT seller_id, status, is_shared INTO owner_id, proxy_status, shared FROM proxies WHERE id = NEW.proxy_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'proxy not found' USING ERRCODE = '23503';
        END IF;
        IF proxy_status = 'disabled' THEN
            RAISE EXCEPTION 'proxy is disabled' USING ERRCODE = '23514';
        END IF;
        IF owner_id IS NOT NULL AND owner_id <> NEW.seller_id THEN
            RAISE EXCEPTION 'proxy belongs to another seller' USING ERRCODE = '23514';
        END IF;
        IF owner_id IS NULL AND NOT shared AND EXISTS (
            SELECT 1 FROM upstreams WHERE proxy_id = NEW.proxy_id AND status <> 'removed' AND id <> NEW.id
        ) THEN
            RAISE EXCEPTION 'platform proxy is already assigned' USING ERRCODE = '23505';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION guard_proxy_policy_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    active_bindings int;
BEGIN
    SELECT count(*) INTO active_bindings FROM upstreams
     WHERE proxy_id = OLD.id AND status <> 'removed';
    IF TG_OP = 'DELETE' THEN
        IF active_bindings > 0 THEN
            RAISE EXCEPTION 'remove all active upstream bindings before deleting proxy' USING ERRCODE = '23514';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.status = 'disabled' AND OLD.status <> 'disabled' AND active_bindings > 0 THEN
        RAISE EXCEPTION 'remove all active upstream bindings before disabling proxy' USING ERRCODE = '23514';
    END IF;
    IF NEW.seller_id IS NULL AND NOT NEW.is_shared AND active_bindings > 1 THEN
        RAISE EXCEPTION 'remove all but one active upstream before disabling proxy sharing' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER proxy_policy_mutation_guard
BEFORE UPDATE OF status, is_shared, seller_id OR DELETE ON proxies
FOR EACH ROW EXECUTE FUNCTION guard_proxy_policy_mutation();
