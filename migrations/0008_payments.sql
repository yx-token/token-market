-- 0008 在线支付：充值单接入支付宝（电脑网站支付、当面付）与微信支付（Native），支付通知日志，余额退款原路退回。

ALTER TABLE recharge_orders
    ADD COLUMN channel         text           NOT NULL DEFAULT 'manual',  -- manual / alipay_page / alipay_f2f / wechat_native
    ADD COLUMN out_trade_no    text,                                       -- 发给渠道的商户订单号
    ADD COLUMN pay_kind        text,                                       -- redirect（跳转收银台）/ qrcode（展示二维码）
    ADD COLUMN pay_payload     text,                                       -- 收银台地址或二维码内容，便于用户回到页面继续支付
    ADD COLUMN expires_at      timestamptz,
    ADD COLUMN close_reason    text,
    ADD COLUMN last_synced_at  timestamptz,                                -- 最近一次主动查单
    ADD COLUMN refunded_amount numeric(20,10) NOT NULL DEFAULT 0;          -- 已原路退回的金额
CREATE UNIQUE INDEX recharge_orders_out_trade_no_idx ON recharge_orders (out_trade_no) WHERE out_trade_no IS NOT NULL;
CREATE INDEX recharge_orders_open_idx ON recharge_orders (created_at) WHERE status IN ('pending', 'cancelled');
ALTER TABLE recharge_orders DROP CONSTRAINT recharge_orders_status_check;
ALTER TABLE recharge_orders ADD CONSTRAINT recharge_orders_status_check
    CHECK (status IN ('pending', 'crediting', 'credited', 'cancelled', 'expired', 'failed'));

-- 渠道异步通知的处理记录（不含原文中的签名），用于对账与排查
CREATE TABLE payment_notifications (
    id            bigserial PRIMARY KEY,
    provider      text        NOT NULL,
    out_trade_no  text,
    trade_no      text,
    state         text,
    amount_fen    bigint,
    result        text        NOT NULL,   -- credited / duplicate / expired / ignored / amount_mismatch / unknown_order / invalid / error
    error         text,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_notifications_order_idx ON payment_notifications (out_trade_no);
CREATE INDEX payment_notifications_created_idx ON payment_notifications (created_at);

-- 余额退款的打款明细：优先原路退回到近期的在线充值单，不足部分人工打款
CREATE TABLE refund_payouts (
    id                 bigserial PRIMARY KEY,
    refund_id          bigint         NOT NULL REFERENCES refunds (id),
    recharge_order_id  bigint REFERENCES recharge_orders (id),
    channel            text           NOT NULL,   -- 原充值单的支付方式；manual 表示人工打款
    amount             numeric(20,10) NOT NULL CHECK (amount > 0),
    out_refund_no      text UNIQUE,
    status             text           NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'success', 'failed', 'manual')),
    provider_ref       text,
    error              text,
    attempts           int            NOT NULL DEFAULT 0,
    next_attempt_at    timestamptz    NOT NULL DEFAULT now(),
    created_at         timestamptz    NOT NULL DEFAULT now(),
    finished_at        timestamptz
);
CREATE INDEX refund_payouts_refund_idx ON refund_payouts (refund_id);
CREATE INDEX refund_payouts_due_idx ON refund_payouts (next_attempt_at) WHERE status IN ('pending', 'processing');

ALTER TABLE billing_anomalies DROP CONSTRAINT billing_anomalies_rule_check;
ALTER TABLE billing_anomalies ADD CONSTRAINT billing_anomalies_rule_check
    CHECK (rule IN ('R1_failed_with_charge', 'R2_success_without_charge', 'R3_orphan_charge', 'R4_duplicate', 'balance_mismatch',
                    'payment_amount_mismatch'));
