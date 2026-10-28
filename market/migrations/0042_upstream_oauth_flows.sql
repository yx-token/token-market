ALTER TABLE upstream_oauth_sessions
    ALTER COLUMN auth_url DROP NOT NULL,
    ADD COLUMN kind text NOT NULL DEFAULT 'link',
    ADD COLUMN provider text,
    ADD COLUMN user_code text,
    ADD COLUMN verification_uri text,
    ADD COLUMN interval_seconds int,
    ADD COLUMN next_poll_at timestamptz,
    ADD COLUMN last_status text;

UPDATE vendor_configs
SET auth_types = array_append(auth_types, 'oauth'), updated_at = now()
WHERE platform IN ('kimi', 'zhipu') AND NOT ('oauth' = ANY(auth_types));

UPDATE vendor_configs
SET auth_types = array_append(auth_types, 'oauth-intl'), updated_at = now()
WHERE platform = 'zhipu' AND NOT ('oauth-intl' = ANY(auth_types));
