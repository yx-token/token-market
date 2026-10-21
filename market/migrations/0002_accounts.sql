-- 0002 平台用户、实名、风控基础表。

CREATE TABLE users (
    id             bigserial PRIMARY KEY,
    email          text        NOT NULL UNIQUE,          -- 统一小写
    password_hash  text        NOT NULL,
    is_admin       boolean     NOT NULL DEFAULT false,
    status         text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'closed')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_login_at  timestamptz
);

CREATE TABLE kyc_profiles (
    user_id         bigint      PRIMARY KEY REFERENCES users (id),
    real_name_enc   bytea       NOT NULL,
    id_number_enc   bytea       NOT NULL,
    id_number_hash  text        NOT NULL,                -- 用于关联识别，不可逆
    status          text        NOT NULL CHECK (status IN ('pending', 'verified', 'rejected')),
    submitted_at    timestamptz NOT NULL DEFAULT now(),
    reviewed_at     timestamptz,
    reviewed_by     bigint,
    note            text
);
CREATE INDEX kyc_profiles_id_hash_idx ON kyc_profiles (id_number_hash);

-- 用户的 IP 事件（注册、登录、充值），用于买卖双方关联识别
CREATE TABLE user_ip_events (
    id          bigserial PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id),
    ip          text        NOT NULL,
    kind        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_ip_events_ip_idx ON user_ip_events (ip, created_at);
CREATE INDEX user_ip_events_user_idx ON user_ip_events (user_id, created_at);

CREATE TABLE risk_events (
    id               bigserial PRIMARY KEY,
    kind             text        NOT NULL,   -- buyer_seller_linkage / direct_concentration / ...
    severity         text        NOT NULL CHECK (severity IN ('low', 'medium', 'high')),
    user_id          bigint,
    related_user_id  bigint,
    upstream_id      bigint,
    dedupe_key       text        UNIQUE,     -- 同一风险只记一次
    detail           jsonb,
    status           text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'ignored')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    resolved_at      timestamptz,
    resolved_by      bigint
);
CREATE INDEX risk_events_user_open_idx ON risk_events (user_id) WHERE status = 'open';
