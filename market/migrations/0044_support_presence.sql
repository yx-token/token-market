-- 0044 客服会话：已读水位 + 输入中心跳。

ALTER TABLE support_conversations
    ADD COLUMN IF NOT EXISTS user_last_read_msg_id  bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS staff_last_read_msg_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_typing_at         timestamptz,
    ADD COLUMN IF NOT EXISTS staff_typing_at        timestamptz;
