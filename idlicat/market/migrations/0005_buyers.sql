-- 0005 sub2api 影子身份、买家 Key 与内部 key。

-- 本系统在 sub2api 中的用户身份：每个买家一个影子用户，另有一个平台探测用户（owner_id = 0）
CREATE TABLE shadow_identities (
    id                 bigserial PRIMARY KEY,
    kind               text        NOT NULL CHECK (kind IN ('buyer', 'probe')),
    owner_id           bigint      NOT NULL,
    status             text        NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning', 'active', 'failed')),
    sub2api_user_id    bigint UNIQUE,
    email              text        NOT NULL UNIQUE,
    password_enc       bytea       NOT NULL,
    refresh_token_enc  bytea,
    access_token_enc   bytea,
    access_expires_at  timestamptz,
    allowed_group_ids  bigint[]    NOT NULL DEFAULT '{}',
    concurrency        int         NOT NULL DEFAULT 5,
    keys_locked        boolean     NOT NULL DEFAULT false,      -- 退款期间暂停该买家的全部 Key
    last_error         text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, owner_id)
);

CREATE TABLE buyer_keys (
    id                   bigserial PRIMARY KEY,
    user_id              bigint      NOT NULL REFERENCES users (id),
    name                 text        NOT NULL,
    key_hash             bytea       NOT NULL UNIQUE,
    key_prefix           text        NOT NULL,
    route_type           text        NOT NULL CHECK (route_type IN ('default', 'chain')),
    status               text        NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning', 'active', 'disabled', 'deleted')),
    disabled_reason      text,
    limits               jsonb       NOT NULL DEFAULT '{}',
    limit_blocked_until  timestamptz,                        -- 花费限额触发后的封禁截止时间
    limit_block_reason   text,
    config_version       bigint      NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    deleted_at           timestamptz
);
CREATE INDEX buyer_keys_user_idx ON buyer_keys (user_id);

CREATE TABLE buyer_key_hops (
    key_id      bigint   NOT NULL REFERENCES buyer_keys (id),
    position    smallint NOT NULL CHECK (position BETWEEN 1 AND 5),
    listing_id  bigint REFERENCES listings (id),
    pool_id     bigint REFERENCES pools (id),
    PRIMARY KEY (key_id, position),
    CHECK ((listing_id IS NULL) <> (pool_id IS NULL))
);

CREATE TABLE internal_keys (
    id                bigserial PRIMARY KEY,
    buyer_key_id      bigint      NOT NULL REFERENCES buyer_keys (id),
    buyer_user_id     bigint      NOT NULL REFERENCES users (id),
    sub2api_group_id  bigint      NOT NULL,
    sub2api_key_id    bigint UNIQUE,
    key_enc           bytea       NOT NULL,
    key_hash          bytea       NOT NULL UNIQUE,
    status            text        NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning', 'active', 'frozen', 'deleted')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (buyer_key_id, sub2api_group_id)
);

-- 探测用户名下的 key，每个探测分组一把
CREATE TABLE system_keys (
    id                bigserial PRIMARY KEY,
    purpose           text        NOT NULL CHECK (purpose IN ('probe')),
    sub2api_group_id  bigint      NOT NULL UNIQUE,
    sub2api_key_id    bigint UNIQUE,
    key_enc           bytea       NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);
