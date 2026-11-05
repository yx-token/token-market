-- loxo 任务中心对接：票据绑定与回调事件出站表。设计见 docs/2026-11-06-loxo-reward-tasks.md。
CREATE TABLE reward_tickets (
    id bigserial PRIMARY KEY,
    ticket text NOT NULL UNIQUE CHECK (ticket ~ '^rt_[A-Za-z0-9_-]{8,61}$'),
    user_id bigint NOT NULL REFERENCES users (id),
    new_user boolean NOT NULL DEFAULT false,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'active', 'completed', 'skipped', 'invalid')),
    reason text NOT NULL DEFAULT '',
    task_id bigint,
    task_title text NOT NULL DEFAULT '',
    task_type text NOT NULL DEFAULT '',
    period_key text NOT NULL DEFAULT '',
    expires_at timestamptz,
    attempts int NOT NULL DEFAULT 0,
    next_check_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reward_tickets_pending_idx ON reward_tickets (next_check_at) WHERE state = 'pending';
CREATE INDEX reward_tickets_active_idx ON reward_tickets (user_id) WHERE state = 'active';
CREATE INDEX reward_tickets_user_idx ON reward_tickets (user_id);

CREATE TABLE reward_events (
    id bigserial PRIMARY KEY,
    ticket_id bigint NOT NULL REFERENCES reward_tickets (id),
    event_id text NOT NULL UNIQUE,
    event_type text NOT NULL CHECK (event_type IN ('register', 'recharge')),
    recharge_order_id bigint REFERENCES recharge_orders (id),
    amount numeric(18, 2),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'accepted', 'completed', 'ignored', 'rejected', 'failed')),
    reason text NOT NULL DEFAULT '',
    rewarded boolean NOT NULL DEFAULT false,
    attempts int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    response jsonb,
    sent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (ticket_id, recharge_order_id)
);
CREATE INDEX reward_events_pending_idx ON reward_events (next_attempt_at) WHERE state = 'pending';
CREATE INDEX reward_events_ticket_idx ON reward_events (ticket_id);

INSERT INTO iam_resources (id, parent_id, name, kind, path, icon, sort)
VALUES ('console.ops.reward', 'console.ops', '任务对接', 'menu', '/admin/market/reward-tasks', 'gift', 80)
ON CONFLICT (id) DO NOTHING;

INSERT INTO iam_role_resources (role_id, resource_id)
SELECT id, 'console.ops.reward' FROM iam_roles WHERE code IN ('platform_admin', 'operator')
ON CONFLICT DO NOTHING;
