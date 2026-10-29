-- 0042 买家评价：商家为主、挂单为辅；短评选填；支持追评与管理端隐藏。

CREATE TABLE reviews (
    id           bigserial PRIMARY KEY,
    buyer_id     bigint      NOT NULL REFERENCES users (id),
    seller_id    bigint      NOT NULL REFERENCES users (id),
    listing_id   bigint      REFERENCES listings (id),
    parent_id    bigint      REFERENCES reviews (id),
    rating       smallint,   -- 主评 1–5；追评可空（不改星）
    content      text        NOT NULL DEFAULT '',
    tags         text[]      NOT NULL DEFAULT '{}',
    status       text        NOT NULL DEFAULT 'visible'
                             CHECK (status IN ('visible', 'hidden', 'deleted')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    hidden_at    timestamptz,
    hidden_by    bigint      REFERENCES users (id),
    hide_reason  text,
    CONSTRAINT reviews_rating_range CHECK (rating IS NULL OR (rating BETWEEN 1 AND 5)),
    CONSTRAINT reviews_main_has_rating CHECK (parent_id IS NOT NULL OR rating IS NOT NULL),
    CONSTRAINT reviews_not_self CHECK (buyer_id <> seller_id)
);

-- 同一买家对同一卖家仅一条主评（visible/hidden 都占坑，deleted 可重评）
CREATE UNIQUE INDEX reviews_one_main_uidx
    ON reviews (buyer_id, seller_id)
    WHERE parent_id IS NULL AND status <> 'deleted';

-- 同一主评仅一条追评
CREATE UNIQUE INDEX reviews_one_followup_uidx
    ON reviews (parent_id)
    WHERE parent_id IS NOT NULL AND status <> 'deleted';

CREATE INDEX reviews_seller_visible_idx ON reviews (seller_id, created_at DESC)
    WHERE status = 'visible' AND parent_id IS NULL;
CREATE INDEX reviews_listing_visible_idx ON reviews (listing_id, created_at DESC)
    WHERE status = 'visible' AND listing_id IS NOT NULL;
CREATE INDEX reviews_admin_idx ON reviews (status, created_at DESC);

-- 商家评分汇总（仅 visible 主评）
CREATE TABLE seller_rating_stats (
    seller_id     bigint PRIMARY KEY REFERENCES users (id),
    avg_rating    numeric(3,2) NOT NULL DEFAULT 0,
    rating_count  int          NOT NULL DEFAULT 0,
    updated_at    timestamptz  NOT NULL DEFAULT now()
);
