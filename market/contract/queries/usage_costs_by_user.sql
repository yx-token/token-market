-- 精确余额复核：$1 = 用户 id，$2 = 上页末尾 id，$3 = 批量大小。
SELECT id, actual_cost
FROM usage_logs
WHERE user_id = $1 AND id > $2
ORDER BY id
LIMIT $3
