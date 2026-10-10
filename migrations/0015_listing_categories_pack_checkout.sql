-- 挂单市场分类（卖家发布时选择，驱动市场筛选 / 四宫格推荐）
ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS categories text[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN listings.categories IS '市场专场标签：low_rate / coding / featured；pack 由 mode 推导';

-- 额度包在线支付：先充值下单，到账后履约签发 Key
CREATE TABLE IF NOT EXISTS pack_checkouts (
    id           bigserial PRIMARY KEY,
    recharge_id  bigint      NOT NULL UNIQUE REFERENCES recharge_orders (id),
    listing_id   bigint      NOT NULL REFERENCES listings (id),
    buyer_id     bigint      NOT NULL REFERENCES users (id),
    key_name     text,
    status       text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'fulfilled', 'cancelled', 'failed')),
    pack_id      bigint      REFERENCES quota_packs (id),
    error_reason text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    fulfilled_at timestamptz
);
CREATE INDEX IF NOT EXISTS pack_checkouts_buyer_idx ON pack_checkouts (buyer_id, status);
CREATE INDEX IF NOT EXISTS pack_checkouts_listing_idx ON pack_checkouts (listing_id);
