-- 0045 默认分组（保障池）软删除。
-- 删除后保留本地行：晚到的用量仍按 sub2api_group_id 归为 pool 渠道，买家 Key 的历史路由也还能显示分组名。
-- 名称唯一只约束未删除的分组，删除后可以用同名重建。

ALTER TABLE pools ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

ALTER TABLE pools DROP CONSTRAINT IF EXISTS pools_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS pools_name_live_key ON pools (name) WHERE deleted_at IS NULL;
