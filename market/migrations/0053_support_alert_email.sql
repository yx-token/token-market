-- 0053 客服超时/积压预警邮件状态：避免重复轰炸管理员邮箱。

ALTER TABLE support_conversations
    ADD COLUMN IF NOT EXISTS overdue_alerted_at timestamptz;

CREATE TABLE IF NOT EXISTS support_alert_state (
    key         text PRIMARY KEY,
    fired_at    timestamptz,
    meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO support_alert_state (key) VALUES ('backlog')
ON CONFLICT (key) DO NOTHING;
