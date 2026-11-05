CREATE TABLE promo_channels (
    id bigserial PRIMARY KEY,
    code text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9_-]{2,32}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    landing_path text NOT NULL DEFAULT '/',
    note text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE promo_channel_visits (
    id bigserial PRIMARY KEY,
    channel_id bigint NOT NULL REFERENCES promo_channels (id),
    visitor_id text NOT NULL CHECK (visitor_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    ip text,
    user_agent text,
    referrer text,
    landing_path text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX promo_visits_channel_time_idx ON promo_channel_visits (channel_id, created_at);
CREATE INDEX promo_visits_channel_visitor_idx ON promo_channel_visits (channel_id, visitor_id, created_at);

CREATE TABLE promo_channel_users (
    user_id bigint PRIMARY KEY REFERENCES users (id),
    channel_id bigint NOT NULL REFERENCES promo_channels (id),
    visitor_id text,
    touched_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX promo_users_channel_time_idx ON promo_channel_users (channel_id, created_at);

INSERT INTO iam_resources (id, parent_id, name, kind, path, icon, sort)
VALUES ('console.ops.promo', 'console.ops', '推广渠道', 'menu', '/admin/market/promo-channels', 'megaphone', 70)
ON CONFLICT (id) DO NOTHING;

INSERT INTO iam_role_resources (role_id, resource_id)
SELECT id, 'console.ops.promo' FROM iam_roles WHERE code IN ('platform_admin', 'operator')
ON CONFLICT DO NOTHING;
