-- 0010 支付商户改为站内配置：支付宝应用与微信支付商户号的配置保存在库中，私钥与 APIv3 密钥加密存储。

CREATE TABLE payment_merchants (
    provider     text        PRIMARY KEY CHECK (provider IN ('alipay', 'wechat')),
    enabled      boolean     NOT NULL DEFAULT false,   -- 停用后不再创建新订单，已有订单的通知、查单、退款照常处理
    config       jsonb       NOT NULL DEFAULT '{}',    -- 非敏感配置：AppID、商户号、网关、公钥与证书
    secrets_enc  bytea,                                 -- 加密的 JSON：私钥、APIv3 密钥
    updated_by   bigint,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
