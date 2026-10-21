-- 商家清单按商家身份保存，而不是按某一挂单保存。
CREATE TABLE buyer_merchants (
    user_id bigint NOT NULL REFERENCES users(id),
    seller_id bigint NOT NULL REFERENCES users(id),
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, seller_id)
);
CREATE INDEX buyer_merchants_seller_idx ON buyer_merchants(seller_id, user_id);

-- 兼容现有 Key：部署后既有显式挂单路由仍有对应商家授权。
INSERT INTO buyer_merchants(user_id, seller_id)
SELECT DISTINCT k.user_id, l.seller_id
FROM buyer_keys k JOIN buyer_key_hops h ON h.key_id = k.id
JOIN listings l ON l.id = h.listing_id
WHERE k.status <> 'deleted'
ON CONFLICT DO NOTHING;

-- 升级前已下单但尚未签发 Key 的旧在线额度包，同样保留买家的商家选择。
INSERT INTO buyer_merchants(user_id, seller_id)
SELECT DISTINCT c.buyer_id, l.seller_id
FROM pack_checkouts c JOIN listings l ON l.id = c.listing_id
WHERE c.status = 'pending'
ON CONFLICT DO NOTHING;
