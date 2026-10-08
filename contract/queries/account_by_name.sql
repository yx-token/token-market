-- 账号创建重试：按名称查找已创建的账号
SELECT id, name, platform, type, status, error_message, schedulable, priority, concurrency,
       load_factor, rate_multiplier, rate_limit_reset_at, overload_until,
       temp_unschedulable_until, temp_unschedulable_reason, extra, deleted_at
FROM accounts
WHERE name = $1 AND deleted_at IS NULL
