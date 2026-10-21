-- 余额展示与对账：$1 = 用户 id 数组
SELECT id, balance, status
FROM users
WHERE id = ANY($1) AND deleted_at IS NULL
