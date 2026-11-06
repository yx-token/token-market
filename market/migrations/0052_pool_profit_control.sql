-- 0052 默认分组（保障池）利润控制开关。
-- 开启时：调度只把倍率 ≤ D × (1 − 最低毛利 − 安全缓冲) 的上游放进该池，回收报价也按这个上限封顶，
-- 引擎分组同步开启利润控制（仅引擎支持的平台）。关闭时三者都不生效，最低毛利 / 安全缓冲保留，供再次开启。
-- 新建的池默认关闭；此前创建的池一直按开启运行，迁移时保持开启，不改变线上行为。

ALTER TABLE pools ADD COLUMN IF NOT EXISTS profit_control_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE pools ALTER COLUMN profit_control_enabled SET DEFAULT false;
