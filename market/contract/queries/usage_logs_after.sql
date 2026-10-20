-- 用量增量拉取：$1 = 游标 id，$2 = created_at 上限（当前时间减安全延迟），$3 = 批量大小
SELECT id, request_id, created_at, user_id, api_key_id, account_id, group_id, model,
       input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
       total_cost, actual_cost, account_rate_multiplier, first_token_ms, duration_ms,
       upstream_response_model, upstream_model_mismatch
FROM usage_logs
WHERE id > $1 AND created_at < $2
ORDER BY id
LIMIT $3
