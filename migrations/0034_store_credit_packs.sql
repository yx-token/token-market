-- 店铺活动额度与原 quota_packs 独立，金额精确记账；历史 D16 钱包单位不变。
CREATE TABLE store_credit_offers (
    id bigserial PRIMARY KEY,
    seller_id bigint NOT NULL REFERENCES users(id),
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    price_cny numeric(12,2) NOT NULL CHECK (price_cny > 0),
    face_value_usd numeric(12,2) NOT NULL CHECK (face_value_usd > 0),
    allowed_platforms text[] NOT NULL DEFAULT '{}',
    allowed_models text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX store_credit_offers_seller_idx ON store_credit_offers(seller_id,status,id);

CREATE TABLE store_credit_packs (
    id bigserial PRIMARY KEY,
    offer_id bigint NOT NULL REFERENCES store_credit_offers(id),
    buyer_id bigint NOT NULL REFERENCES users(id),
    seller_id bigint NOT NULL REFERENCES users(id),
    title text NOT NULL,
    price_cny numeric(12,2) NOT NULL CHECK (price_cny > 0),
    face_value_usd numeric(12,2) NOT NULL CHECK (face_value_usd > 0),
    remaining_usd numeric(30,16) NOT NULL CHECK (remaining_usd >= 0 AND remaining_usd <= face_value_usd),
    remaining_balance_units numeric(20,10) NOT NULL CHECK (remaining_balance_units >= 0),
    prepaid_remaining_cny numeric(20,10) NOT NULL CHECK (prepaid_remaining_cny >= 0 AND prepaid_remaining_cny <= price_cny),
    usd_to_balance_rate numeric(16,8) NOT NULL CHECK (usd_to_balance_rate > 0),
    allowed_platforms text[] NOT NULL DEFAULT '{}',
    allowed_models text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning','active','depleted','refunding','refunded')),
    idempotency_key text NOT NULL,
    purchase_op_id bigint REFERENCES sub2api_ops(id),
    funding_op_id bigint REFERENCES sub2api_ops(id),
    refund_op_id bigint REFERENCES sub2api_ops(id),
    refund_credit_op_id bigint REFERENCES sub2api_ops(id),
    refund_requested_at timestamptz,
    refunded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(buyer_id,idempotency_key)
);
CREATE INDEX store_credit_packs_buyer_idx ON store_credit_packs(buyer_id,status,id);

ALTER TABLE shadow_identities DROP CONSTRAINT shadow_identities_kind_check;
ALTER TABLE shadow_identities ADD CONSTRAINT shadow_identities_kind_check CHECK(kind IN ('buyer','probe','credit_pack'));

CREATE TABLE store_credit_keys (
    id bigserial PRIMARY KEY,
    pack_id bigint NOT NULL REFERENCES store_credit_packs(id),
    buyer_key_id bigint NOT NULL REFERENCES buyer_keys(id),
    listing_id bigint NOT NULL REFERENCES listings(id),
    sub2api_group_id bigint NOT NULL,
    sub2api_key_id bigint UNIQUE,
    key_enc bytea NOT NULL,
    key_hash bytea NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'provisioning' CHECK(status IN ('provisioning','active','frozen','deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(pack_id,buyer_key_id,listing_id)
);
CREATE INDEX store_credit_keys_route_idx ON store_credit_keys(buyer_key_id,listing_id,pack_id) WHERE status='active';

ALTER TABLE ledger_accounts DROP CONSTRAINT ledger_accounts_kind_check;
ALTER TABLE ledger_accounts ADD CONSTRAINT ledger_accounts_kind_check CHECK (kind IN (
    'buyer_cash','buyer_bonus','buyer_store_credit','seller_pending','seller_available','seller_withdrawing',
    'platform_cash','platform_revenue','platform_probe_cost','platform_adjustment'));
-- 使用既有 adjust 类型与唯一 ref 标识购买/退款，保留历史 ledger_txns 类型约束。

ALTER TABLE usage_records
    ADD COLUMN credit_pack_id bigint REFERENCES store_credit_packs(id),
    ADD COLUMN credit_covered_units numeric(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN credit_prepaid_cny numeric(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN cash_cost numeric(20,10);
CREATE INDEX usage_records_credit_pack_idx ON usage_records(credit_pack_id,created_at) WHERE credit_pack_id IS NOT NULL;

CREATE TABLE store_credit_allocations (
    usage_id bigint PRIMARY KEY REFERENCES usage_records(sub2api_usage_id),
    pack_id bigint NOT NULL REFERENCES store_credit_packs(id),
    buyer_id bigint NOT NULL REFERENCES users(id),
    seller_id bigint NOT NULL REFERENCES users(id),
    model text NOT NULL,
    platform text NOT NULL,
    covered_usd numeric(30,16) NOT NULL CHECK(covered_usd >= 0),
    covered_balance_units numeric(20,10) NOT NULL CHECK(covered_balance_units >= 0),
    prepaid_cny numeric(20,10) NOT NULL CHECK(prepaid_cny >= 0),
    cash_cny numeric(20,10) NOT NULL CHECK(cash_cny >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX store_credit_allocations_pack_idx ON store_credit_allocations(pack_id,usage_id);
