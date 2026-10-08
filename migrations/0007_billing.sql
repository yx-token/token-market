-- 0007 用量、复式账本、结算、充值、退款、提现、Edge 尝试日志、计费对账异常。

CREATE TABLE usage_records (
    sub2api_usage_id         bigint         PRIMARY KEY,
    sub2api_request_id       text,
    created_at               timestamptz    NOT NULL,
    sub2api_account_id       bigint         NOT NULL,
    sub2api_group_id         bigint,
    sub2api_user_id          bigint         NOT NULL,
    sub2api_api_key_id       bigint         NOT NULL,
    model                    text           NOT NULL,
    input_tokens             int            NOT NULL DEFAULT 0,
    output_tokens            int            NOT NULL DEFAULT 0,
    cache_creation_tokens    int            NOT NULL DEFAULT 0,
    cache_read_tokens        int            NOT NULL DEFAULT 0,
    total_cost               numeric(20,10) NOT NULL,
    actual_cost              numeric(20,10) NOT NULL,
    account_rate_multiplier  numeric(10,4),
    first_token_ms           int,
    duration_ms              int,
    upstream_response_model  text,
    upstream_model_mismatch  boolean,
    -- 入账时解析出的业务归属
    upstream_id              bigint,
    seller_id                bigint,
    listing_id               bigint,
    buyer_user_id            bigint,
    buyer_key_id             bigint,
    channel                  text           NOT NULL CHECK (channel IN ('pool', 'direct', 'probe', 'unknown')),
    payout                   numeric(20,10) NOT NULL DEFAULT 0,   -- total_cost × account_rate_multiplier
    ledger_txn_id            bigint,
    ingested_at              timestamptz    NOT NULL DEFAULT now()
);
CREATE INDEX usage_records_request_idx ON usage_records (sub2api_request_id);
CREATE INDEX usage_records_buyer_key_idx ON usage_records (buyer_key_id, created_at);
CREATE INDEX usage_records_buyer_idx ON usage_records (buyer_user_id, created_at);
CREATE INDEX usage_records_upstream_idx ON usage_records (upstream_id, created_at);
CREATE INDEX usage_records_unposted_idx ON usage_records (sub2api_usage_id) WHERE ledger_txn_id IS NULL;

CREATE TABLE ingest_cursors (
    source      text        PRIMARY KEY,
    last_id     bigint      NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
INSERT INTO ingest_cursors (source) VALUES ('usage_logs');

CREATE TABLE ledger_accounts (
    id          bigserial PRIMARY KEY,
    owner_type  text NOT NULL CHECK (owner_type IN ('buyer', 'seller', 'platform')),
    owner_id    bigint NOT NULL DEFAULT 0,
    kind        text NOT NULL CHECK (kind IN (
                    'buyer_cash', 'buyer_bonus',
                    'seller_pending', 'seller_available', 'seller_withdrawing',
                    'platform_cash', 'platform_revenue', 'platform_probe_cost', 'platform_adjustment')),
    UNIQUE (owner_type, owner_id, kind)
);

CREATE TABLE ledger_txns (
    id          bigserial PRIMARY KEY,
    type        text        NOT NULL CHECK (type IN ('recharge', 'usage', 'unfreeze', 'withdraw_request', 'withdraw_paid',
                                                     'withdraw_reverted', 'refund', 'adjust')),
    ref         text        NOT NULL,
    note        text,
    created_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (type, ref)
);

-- 正数为借、负数为贷；同一交易的分录之和必须为 0（由应用层与下方约束触发器保证）
CREATE TABLE ledger_entries (
    id          bigserial PRIMARY KEY,
    txn_id      bigint         NOT NULL REFERENCES ledger_txns (id),
    account_id  bigint         NOT NULL REFERENCES ledger_accounts (id),
    amount      numeric(20,10) NOT NULL CHECK (amount <> 0),
    created_at  timestamptz    NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_account_idx ON ledger_entries (account_id, created_at);
CREATE INDEX ledger_entries_txn_idx ON ledger_entries (txn_id);

CREATE FUNCTION ledger_txn_balanced() RETURNS trigger AS $$
DECLARE
    total numeric;
BEGIN
    SELECT COALESCE(sum(amount), 0) INTO total FROM ledger_entries WHERE txn_id = NEW.txn_id;
    IF total <> 0 THEN
        RAISE EXCEPTION 'ledger txn % is unbalanced: %', NEW.txn_id, total;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger_txn_balanced();

-- 按"上游 × 小时 × 渠道"汇总的卖家应收，冻结期满后解冻
CREATE TABLE settlement_lines (
    upstream_id    bigint         NOT NULL,
    seller_id      bigint         NOT NULL,
    hour           timestamptz    NOT NULL,
    channel        text           NOT NULL,
    request_count  int            NOT NULL DEFAULT 0,
    total_cost     numeric(20,10) NOT NULL DEFAULT 0,
    payout         numeric(20,10) NOT NULL DEFAULT 0,
    buyer_paid     numeric(20,10) NOT NULL DEFAULT 0,
    unfrozen_payout numeric(20,10) NOT NULL DEFAULT 0,   -- 已解冻部分（入账可能晚于解冻，按差额补解冻）
    PRIMARY KEY (upstream_id, hour, channel)
);
CREATE INDEX settlement_lines_seller_idx ON settlement_lines (seller_id, hour);

CREATE TABLE recharge_orders (
    id            bigserial PRIMARY KEY,
    user_id       bigint         NOT NULL REFERENCES users (id),
    amount        numeric(20,10) NOT NULL CHECK (amount > 0),
    provider      text           NOT NULL,
    provider_ref  text,
    status        text           NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'crediting', 'credited', 'cancelled', 'failed')),
    client_ip     text,
    op_id         bigint,
    created_at    timestamptz    NOT NULL DEFAULT now(),
    paid_at       timestamptz,
    credited_at   timestamptz
);
CREATE INDEX recharge_orders_user_idx ON recharge_orders (user_id, created_at);

CREATE TABLE refunds (
    id                bigserial PRIMARY KEY,
    user_id           bigint         NOT NULL REFERENCES users (id),
    status            text           NOT NULL DEFAULT 'locking' CHECK (status IN ('locking', 'subtracting', 'paying', 'done', 'failed')),
    amount            numeric(20,10),                    -- 实际退款金额，扣减 sub2api 余额后确定
    lock_until        timestamptz    NOT NULL,           -- 暂停 Key 后等待进行中请求结束
    op_id             bigint,
    payout_ref        text,
    note              text,
    created_at        timestamptz    NOT NULL DEFAULT now(),
    finished_at       timestamptz
);
CREATE UNIQUE INDEX refunds_one_active_idx ON refunds (user_id) WHERE status NOT IN ('done', 'failed');

CREATE TABLE withdrawals (
    id            bigserial PRIMARY KEY,
    seller_id     bigint         NOT NULL REFERENCES users (id),
    amount        numeric(20,10) NOT NULL CHECK (amount > 0),
    channel       text           NOT NULL,
    account_info_enc bytea       NOT NULL,              -- 收款账号，加密存储
    status        text           NOT NULL DEFAULT 'pending_review' CHECK (status IN ('pending_review', 'approved', 'paid', 'rejected', 'failed')),
    review_reason text,
    reviewed_by   bigint,
    payout_ref    text,
    created_at    timestamptz    NOT NULL DEFAULT now(),
    reviewed_at   timestamptz,
    paid_at       timestamptz
);
CREATE INDEX withdrawals_seller_idx ON withdrawals (seller_id, created_at);

CREATE TABLE edge_attempts (
    id                  bigserial PRIMARY KEY,
    edge_request_id     text        NOT NULL,
    buyer_key_id        bigint      NOT NULL,
    hop                 smallint    NOT NULL,
    listing_id          bigint,
    pool_id             bigint,
    sub2api_request_id  text,
    http_status         int,
    committed           boolean     NOT NULL,
    billable            boolean     NOT NULL DEFAULT true,   -- 生成类接口；模型列表、count_tokens 等不产生费用
    end_reason          text        NOT NULL CHECK (end_reason IN ('completed', 'client_cancel', 'upstream_error', 'timeout', 'skipped', 'retry')),
    ttfb_ms             int,
    started_at          timestamptz NOT NULL,
    ended_at            timestamptz NOT NULL
);
CREATE INDEX edge_attempts_request_idx ON edge_attempts (sub2api_request_id);
CREATE INDEX edge_attempts_started_idx ON edge_attempts (started_at);
CREATE INDEX edge_attempts_listing_idx ON edge_attempts (listing_id, started_at);

CREATE TABLE billing_anomalies (
    id                bigserial PRIMARY KEY,
    rule              text        NOT NULL CHECK (rule IN ('R1_failed_with_charge', 'R2_success_without_charge', 'R3_orphan_charge',
                                                           'R4_duplicate', 'balance_mismatch')),
    dedupe_key        text        NOT NULL UNIQUE,
    sub2api_usage_id  bigint,
    edge_attempt_id   bigint,
    buyer_user_id     bigint,
    amount            numeric(20,10),
    detail            jsonb,
    status            text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'explained', 'adjusted', 'ignored')),
    ledger_txn_id     bigint,
    note              text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    resolved_at       timestamptz
);
