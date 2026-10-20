-- 内部 key 创建重试：按 key 值查找已创建的 key
SELECT id, user_id, group_id, status
FROM api_keys
WHERE key = $1 AND deleted_at IS NULL
