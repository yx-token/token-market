-- 状态对账：本系统管理的账号（名称以 tm- 开头）
SELECT id, name, platform, type, status, error_message, schedulable, priority, concurrency,
       load_factor, rate_multiplier, rate_limit_reset_at, overload_until,
       temp_unschedulable_until, temp_unschedulable_reason, extra, deleted_at, session_window_end
FROM accounts
WHERE name LIKE 'tm-%'
ORDER BY id
