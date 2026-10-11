-- 自动用卡记录：sub2api 自动使用 Codex 重置卡（成功、无卡、失败）时写入的审计日志，取最近的 500 条，按 ID 幂等同步
SELECT id, created_at, extra
FROM audit_logs
WHERE action = 'system.openai.reset_credit.auto' AND created_at > $1
ORDER BY id DESC
LIMIT 500
