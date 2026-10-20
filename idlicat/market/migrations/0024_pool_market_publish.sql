-- 0024 默认分组可作为平台商品上架到市场。

ALTER TABLE pools
    ADD COLUMN market_published   boolean NOT NULL DEFAULT false,
    ADD COLUMN market_title       text    NOT NULL DEFAULT '',
    ADD COLUMN market_description text    NOT NULL DEFAULT '';
