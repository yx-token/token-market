-- 卖家代理与平台代理库存分开管理；旧代理继续归平台所有。
ALTER TABLE proxies ADD COLUMN seller_id bigint REFERENCES users (id);
CREATE INDEX proxies_seller_idx ON proxies (seller_id);
-- 同一代理地址不能由卖家重复登记，避免通过不同名称重复分配。
CREATE UNIQUE INDEX proxies_seller_endpoint_idx ON proxies (lower(host), port) WHERE seller_id IS NOT NULL;
-- failed / removing 的账号仍占用代理，只有移除完成后才允许重新分配。
CREATE UNIQUE INDEX upstreams_one_proxy_idx ON upstreams (proxy_id)
    WHERE proxy_id IS NOT NULL AND status <> 'removed';
