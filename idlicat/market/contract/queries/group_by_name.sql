-- 分组创建重试：按名称查找已创建的分组
SELECT id, name, platform, rate_multiplier, is_exclusive, profit_control_enabled, deleted_at
FROM groups
WHERE name = $1 AND deleted_at IS NULL
