-- 额度包：固定面额商品，买家整包购买后从包内扣费；不进保障池。
ALTER TABLE listings DROP CONSTRAINT listings_mode_check;
ALTER TABLE listings ADD CONSTRAINT listings_mode_check CHECK (mode IN ('consignment', 'buyback', 'pack'));

ALTER TABLE listings
    ADD COLUMN sku_kind      text          NOT NULL DEFAULT 'channel'
        CHECK (sku_kind IN ('channel', 'pack')),
    ADD COLUMN face_value    numeric(12,2),
    ADD COLUMN stock         int,
    ADD COLUMN stock_sold    int           NOT NULL DEFAULT 0,
    ADD COLUMN min_amount    numeric(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN title         text,
    ADD COLUMN description   text;

UPDATE listings SET sku_kind = 'channel' WHERE mode IN ('consignment', 'buyback');

CREATE TABLE quota_packs (
    id              bigserial PRIMARY KEY,
    listing_id      bigint        NOT NULL REFERENCES listings (id),
    buyer_id        bigint        NOT NULL REFERENCES users (id),
    buyer_key_id    bigint        REFERENCES buyer_keys (id),
    face_value      numeric(12,2) NOT NULL,
    remaining       numeric(12,2) NOT NULL,
    status          text          NOT NULL CHECK (status IN ('active', 'depleted', 'refunding', 'refunded')),
    paid_amount     numeric(12,2) NOT NULL,
    pay_channel     text,
    recharge_id     bigint,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    refunded_at     timestamptz
);
CREATE INDEX quota_packs_buyer_idx ON quota_packs (buyer_id, status);
CREATE INDEX quota_packs_listing_idx ON quota_packs (listing_id);
