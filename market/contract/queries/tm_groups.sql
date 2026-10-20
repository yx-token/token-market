-- 状态对账：本系统管理的分组
SELECT id, name, platform, rate_multiplier, is_exclusive, profit_control_enabled, deleted_at
FROM groups
WHERE name LIKE 'tm-%'
ORDER BY id
