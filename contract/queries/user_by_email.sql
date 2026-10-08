-- 影子用户创建重试：按邮箱查找已创建的用户
SELECT id, balance, status
FROM users
WHERE email = $1 AND deleted_at IS NULL
