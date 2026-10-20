-- 0021 按充值单退款：买家或管理员选定一笔已到账的充值单、输入金额退款。不暂停 Key，直接扣减 sub2api 余额，
-- 扣减成功后原路退回该订单（人工充值、超过退款期限或渠道不可用时人工打款）。按单退款的人工打款部分也计入该订单的
-- refunded_amount，保证一笔订单累计退款不超过实付金额。

ALTER TABLE refunds
    ADD COLUMN kind              text   NOT NULL DEFAULT 'balance' CHECK (kind IN ('balance', 'order', 'pack')),  -- 退全部余额 / 按充值单 / 额度包原路退
    ADD COLUMN recharge_order_id bigint REFERENCES recharge_orders (id);                                           -- kind = order 时退的充值单
UPDATE refunds SET kind = 'pack' WHERE starts_with(note, 'pack_original:');

-- 按单退款与额度包原路退款先生成打款明细再扣减余额；扣减失败时明细作废
ALTER TABLE refund_payouts DROP CONSTRAINT refund_payouts_status_check;
ALTER TABLE refund_payouts ADD CONSTRAINT refund_payouts_status_check
    CHECK (status IN ('pending', 'processing', 'success', 'failed', 'manual', 'cancelled'));
