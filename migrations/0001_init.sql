-- 0001 基础表：sub2api 写操作出站表、系统设置、兼容自检状态、审计日志。
-- 业务表（用户、上游、挂单、账本等）随各里程碑的迁移加入。

CREATE TABLE sub2api_ops (
    id               bigserial PRIMARY KEY,
    op_type          text        NOT NULL,
    target           text        NOT NULL,
    payload          jsonb       NOT NULL,
    idempotency_key  text        NOT NULL UNIQUE,
    priority         smallint    NOT NULL DEFAULT 100,   -- 越小越先发送；不调度类安全操作用更小的值
    status           text        NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'sending', 'done', 'failed', 'dead')),
    attempts         int         NOT NULL DEFAULT 0,
    max_attempts     int         NOT NULL DEFAULT 10,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    locked_until     timestamptz,                         -- sending 状态的租约，进程崩溃后可被重新领取
    last_error       text,
    result           jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    done_at          timestamptz
);

CREATE INDEX sub2api_ops_ready_idx ON sub2api_ops (priority, next_attempt_at, id)
    WHERE status IN ('pending', 'failed');
CREATE INDEX sub2api_ops_sending_idx ON sub2api_ops (locked_until) WHERE status = 'sending';
CREATE INDEX sub2api_ops_target_idx ON sub2api_ops (target, id);

CREATE TABLE settings (
    key         text PRIMARY KEY,
    value       jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE compat_state (
    id                    smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    sub2api_version       text,
    migration_fingerprint text,
    guard_state           text        NOT NULL DEFAULT 'safe_mode' CHECK (guard_state IN ('normal', 'safe_mode')),
    reason                text,
    checked_at            timestamptz
);
INSERT INTO compat_state (id, guard_state, reason) VALUES (1, 'safe_mode', 'not checked yet');  -- 首次自检通过前不允许写 sub2api

CREATE TABLE audit_logs (
    id          bigserial PRIMARY KEY,
    actor_type  text        NOT NULL,   -- system / admin / seller / buyer
    actor_id    bigint,
    action      text        NOT NULL,
    target      text,
    detail      jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_target_idx ON audit_logs (target, created_at);
