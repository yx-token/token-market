-- 上架检测（含上架失败后的重新检测）与卖家手动复测消耗卖家自己的额度，平台不为这部分探测结算收益。
-- 入账时把这类探测用量的 payout 记 0 并打上标记，卖家用量明细据此说明「由卖家承担」；历史记录不回改。
ALTER TABLE usage_records ADD COLUMN seller_borne boolean NOT NULL DEFAULT false;
