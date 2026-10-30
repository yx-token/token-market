LOCK TABLE proxies, upstreams IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM proxies p JOIN upstreams u ON u.id = p.assigned_upstream_id
        WHERE u.status <> 'removed' AND u.proxy_id IS NOT NULL AND u.proxy_id <> p.id
    ) THEN
        RAISE EXCEPTION 'conflicting legacy proxy bindings; reconcile before migrating';
    END IF;
END $$;

UPDATE upstreams u SET proxy_id = p.id
FROM proxies p
WHERE p.assigned_upstream_id = u.id AND u.proxy_id IS NULL AND u.status <> 'removed';

UPDATE upstreams SET proxy_id = NULL WHERE status = 'removed' AND proxy_id IS NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM proxies p JOIN upstreams u ON u.proxy_id = p.id
        WHERE p.seller_id IS NULL AND u.status <> 'removed'
        GROUP BY p.id HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'platform proxy has multiple active upstreams; reconcile before migrating';
    END IF;
END $$;

ALTER TABLE proxies DROP CONSTRAINT IF EXISTS proxies_assigned_upstream_id_key;
DROP INDEX IF EXISTS upstreams_one_proxy_idx;
CREATE INDEX upstreams_active_proxy_idx ON upstreams(proxy_id, id)
WHERE proxy_id IS NOT NULL AND status <> 'removed';

UPDATE proxies p SET
    assigned_upstream_id = (SELECT min(u.id) FROM upstreams u WHERE u.proxy_id = p.id AND u.status <> 'removed'),
    status = CASE
        WHEN EXISTS (SELECT 1 FROM upstreams u WHERE u.proxy_id = p.id AND u.status <> 'removed') THEN 'assigned'
        WHEN p.status = 'disabled' THEN 'disabled'
        ELSE 'available'
    END;

CREATE FUNCTION guard_upstream_proxy_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    old_proxy_id bigint;
    owner_id bigint;
    proxy_status text;
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
        SELECT seller_id, status INTO owner_id, proxy_status FROM proxies WHERE id = NEW.proxy_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'proxy not found' USING ERRCODE = '23503';
        END IF;
        IF proxy_status = 'disabled' THEN
            RAISE EXCEPTION 'proxy is disabled' USING ERRCODE = '23514';
        END IF;
        IF owner_id IS NOT NULL AND owner_id <> NEW.seller_id THEN
            RAISE EXCEPTION 'proxy belongs to another seller' USING ERRCODE = '23514';
        END IF;
        IF owner_id IS NULL AND EXISTS (
            SELECT 1 FROM upstreams WHERE proxy_id = NEW.proxy_id AND status <> 'removed' AND id <> NEW.id
        ) THEN
            RAISE EXCEPTION 'platform proxy is already assigned' USING ERRCODE = '23505';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION refresh_upstream_proxy_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    old_proxy_id bigint;
    new_proxy_id bigint;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_proxy_id := OLD.proxy_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        new_proxy_id := NEW.proxy_id;
    END IF;
    UPDATE proxies p SET
        assigned_upstream_id = (SELECT min(u.id) FROM upstreams u WHERE u.proxy_id = p.id AND u.status <> 'removed'),
        status = CASE
            WHEN EXISTS (SELECT 1 FROM upstreams u WHERE u.proxy_id = p.id AND u.status <> 'removed') THEN 'assigned'
            WHEN p.status = 'disabled' THEN 'disabled'
            ELSE 'available'
        END
    WHERE p.id IN (old_proxy_id, new_proxy_id);
    RETURN NULL;
END $$;

CREATE TRIGGER upstream_proxy_binding_guard
BEFORE INSERT OR UPDATE OF proxy_id, status, seller_id OR DELETE ON upstreams
FOR EACH ROW EXECUTE FUNCTION guard_upstream_proxy_binding();

CREATE TRIGGER upstream_proxy_binding_refresh
AFTER INSERT OR UPDATE OF proxy_id, status OR DELETE ON upstreams
FOR EACH ROW EXECUTE FUNCTION refresh_upstream_proxy_binding();
