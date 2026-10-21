-- 0003 厂商配置、代理库存、上游、挂单、回收报价。

CREATE TABLE vendor_configs (
    platform             text PRIMARY KEY,
    enabled              boolean       NOT NULL DEFAULT false,   -- 是否允许卖家接入
    auth_types           text[]        NOT NULL,
    consignment_min      numeric(10,4) NOT NULL DEFAULT 0.05,    -- 寄售倍率区间
    consignment_max      numeric(10,4) NOT NULL DEFAULT 1.5,
    buyback_base         jsonb         NOT NULL DEFAULT '{}',    -- 订阅档位 → 回收基础倍率，"default" 为兜底
    default_concurrency  int           NOT NULL DEFAULT 2,
    extra_limits         jsonb         NOT NULL DEFAULT '{}',    -- 写入 sub2api accounts.extra 的限流参数
    updated_at           timestamptz   NOT NULL DEFAULT now()
);

INSERT INTO vendor_configs (platform, auth_types, buyback_base, default_concurrency, extra_limits) VALUES
    ('anthropic',   '{oauth,setup-token}', '{"default": 0.15}', 2, '{"max_sessions": 3, "base_rpm": 15}'),
    ('openai',      '{oauth}',             '{"default": 0.15}', 2, '{}'),
    ('gemini',      '{oauth}',             '{"default": 0.10}', 2, '{}'),
    ('antigravity', '{oauth}',             '{"default": 0.10}', 2, '{}'),
    ('grok',        '{oauth}',             '{"default": 0.10}', 2, '{}'),
    ('kimi',        '{coding}',            '{"default": 0.20}', 2, '{}'),
    ('zhipu',       '{coding}',            '{"default": 0.20}', 2, '{}'),
    ('minimax',     '{coding}',            '{"default": 0.20}', 2, '{}'),
    ('opencode_go', '{go}',                '{"default": 0.20}', 2, '{}');

CREATE TABLE proxies (
    id                    bigserial PRIMARY KEY,
    name                  text        NOT NULL,
    protocol              text        NOT NULL CHECK (protocol IN ('http', 'https', 'socks5', 'socks5h')),
    host                  text        NOT NULL,
    port                  int         NOT NULL CHECK (port BETWEEN 1 AND 65535),
    username              text,
    password_enc          bytea,
    region                text,
    sub2api_proxy_id      bigint UNIQUE,
    status                text        NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'assigned', 'disabled')),
    assigned_upstream_id  bigint UNIQUE,
    created_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE upstreams (
    id                    bigserial PRIMARY KEY,
    seller_id             bigint      NOT NULL REFERENCES users (id),
    platform              text        NOT NULL REFERENCES vendor_configs (platform),
    auth_type             text        NOT NULL,
    is_reference          boolean     NOT NULL DEFAULT false,   -- 平台自备的基线账号，永不面向买家
    status                text        NOT NULL DEFAULT 'draft' CHECK (status IN (
                              'draft', 'authorizing', 'verifying', 'verify_failed', 'ready', 'listed',
                              'suspended', 'failed', 'removing', 'removed')),
    health                text        NOT NULL DEFAULT 'unknown' CHECK (health IN (
                              'unknown', 'healthy', 'limited', 'suspected', 'degraded', 'unavailable')),
    health_reason         text,
    health_changed_at     timestamptz,
    identity_hash         text,                                 -- 官方账号身份的哈希，用于去重
    sub2api_account_id    bigint UNIQUE,
    proxy_id              bigint REFERENCES proxies (id),
    probe_group_id        bigint,
    tier                  text,
    privacy_ok            boolean,
    self_reserve_percent  smallint    NOT NULL DEFAULT 0 CHECK (self_reserve_percent BETWEEN 0 AND 90),
    grade                 text,
    failure_reason        text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    removed_at            timestamptz
);
CREATE UNIQUE INDEX upstreams_identity_active_idx ON upstreams (identity_hash)
    WHERE identity_hash IS NOT NULL AND status NOT IN ('removed', 'failed');
CREATE INDEX upstreams_seller_idx ON upstreams (seller_id);
CREATE INDEX upstreams_proxy_idx ON upstreams (proxy_id);

CREATE TABLE upstream_oauth_sessions (
    upstream_id  bigint      PRIMARY KEY REFERENCES upstreams (id),
    session_id   text        NOT NULL,
    state        text,
    auth_type    text        NOT NULL,
    auth_url     text        NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE listings (
    id                  bigserial PRIMARY KEY,
    upstream_id         bigint        NOT NULL REFERENCES upstreams (id),
    seller_id           bigint        NOT NULL REFERENCES users (id),
    mode                text          NOT NULL CHECK (mode IN ('consignment', 'buyback')),
    status              text          NOT NULL CHECK (status IN ('pending_quote', 'pending_confirm', 'active', 'paused', 'closed')),
    paused_by           text CHECK (paused_by IN ('seller', 'system')),
    multiplier          numeric(10,4),                          -- 寄售为 m_s，回收为 m_b
    join_default_pool   boolean       NOT NULL DEFAULT true,
    listing_group_id    bigint,                                 -- sub2api tm-l-<id>，被买家指定时才创建
    listing_group_rate  numeric(10,4),
    price_changed_at    timestamptz,
    created_at          timestamptz   NOT NULL DEFAULT now(),
    updated_at          timestamptz   NOT NULL DEFAULT now(),
    closed_at           timestamptz
);
CREATE UNIQUE INDEX listings_one_open_idx ON listings (upstream_id) WHERE closed_at IS NULL;
CREATE INDEX listings_status_idx ON listings (status, mode);

CREATE TABLE listing_price_versions (
    id              bigserial PRIMARY KEY,
    listing_id      bigint        NOT NULL REFERENCES listings (id),
    multiplier      numeric(10,4) NOT NULL,
    buyer_price     numeric(10,4),
    effective_from  timestamptz   NOT NULL DEFAULT now(),
    source          text          NOT NULL CHECK (source IN ('seller', 'quote', 'admin'))
);
CREATE INDEX listing_price_versions_idx ON listing_price_versions (listing_id, effective_from);

CREATE TABLE buyback_quotes (
    id           bigserial PRIMARY KEY,
    upstream_id  bigint        NOT NULL REFERENCES upstreams (id),
    listing_id   bigint REFERENCES listings (id),
    grade        text          NOT NULL,
    multiplier   numeric(10,4) NOT NULL,
    status       text          NOT NULL CHECK (status IN ('offered', 'accepted', 'rejected', 'expired', 'superseded')),
    valid_until  timestamptz   NOT NULL,
    created_at   timestamptz   NOT NULL DEFAULT now(),
    decided_at   timestamptz
);
CREATE INDEX buyback_quotes_upstream_idx ON buyback_quotes (upstream_id, created_at);
