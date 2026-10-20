-- 余额对账：游标之后（尚未入账）的消费，$1 = 游标 id，$2 = 用户 id 数组
SELECT user_id, COALESCE(sum(actual_cost), 0)
FROM usage_logs
WHERE id > $1 AND user_id = ANY($2)
GROUP BY user_id
