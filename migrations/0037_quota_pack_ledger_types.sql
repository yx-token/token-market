-- 旧固定额度包的购买/退款沿用独立交易类型；补齐账本约束，避免开通后预付记账被拒绝。
ALTER TABLE ledger_txns DROP CONSTRAINT ledger_txns_type_check;
ALTER TABLE ledger_txns ADD CONSTRAINT ledger_txns_type_check CHECK (type IN (
    'recharge', 'usage', 'unfreeze', 'withdraw_request', 'withdraw_paid',
    'withdraw_reverted', 'refund', 'adjust', 'pack_purchase', 'pack_refund'
));
