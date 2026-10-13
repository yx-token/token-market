-- 缓存完整模型目录，并在上架检测前等待卖家选择挂售模型。
-- 白名单列 upstreams.model_whitelist 由 0030_detection_flows.sql 创建。
ALTER TABLE upstreams
    ADD COLUMN model_catalog text[] NOT NULL DEFAULT '{}',
    ADD COLUMN model_catalog_at timestamptz;

-- 0003_listing.sql 的内联 CHECK 在 PostgreSQL 中命名为 upstreams_status_check。
ALTER TABLE upstreams DROP CONSTRAINT IF EXISTS upstreams_status_check;
ALTER TABLE upstreams ADD CONSTRAINT upstreams_status_check CHECK (status IN (
    'draft', 'authorizing', 'selecting_models', 'verifying', 'verify_failed', 'ready', 'listed',
    'suspended', 'failed', 'removing', 'removed'));
