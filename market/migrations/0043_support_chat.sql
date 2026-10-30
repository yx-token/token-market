-- 0043 官方客服轻量会话：用户与官方一对一对话 + 超时/积压预警字段。

CREATE TABLE support_conversations (
    id                  bigserial PRIMARY KEY,
    user_id             bigint      NOT NULL REFERENCES users (id),
    status              text        NOT NULL DEFAULT 'open'
                                    CHECK (status IN ('open', 'closed')),
    last_message_at     timestamptz NOT NULL DEFAULT now(),
    last_preview        text        NOT NULL DEFAULT '',
    last_sender         text        NOT NULL DEFAULT 'system'
                                    CHECK (last_sender IN ('user', 'staff', 'system')),
    user_unread_count   int         NOT NULL DEFAULT 0 CHECK (user_unread_count >= 0),
    staff_unread_count  int         NOT NULL DEFAULT 0 CHECK (staff_unread_count >= 0),
    awaiting_staff_since timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- 每用户仅一条会话（关闭后复用同一行）
CREATE UNIQUE INDEX support_conversations_user_uidx ON support_conversations (user_id);

CREATE INDEX support_conversations_admin_idx
    ON support_conversations (status, last_message_at DESC);
CREATE INDEX support_conversations_awaiting_idx
    ON support_conversations (awaiting_staff_since)
    WHERE awaiting_staff_since IS NOT NULL AND status = 'open';

CREATE TABLE support_messages (
    id               bigserial PRIMARY KEY,
    conversation_id  bigint      NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    sender           text        NOT NULL CHECK (sender IN ('user', 'staff', 'system')),
    sender_user_id   bigint      REFERENCES users (id),
    body             text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX support_messages_conv_idx ON support_messages (conversation_id, id);
