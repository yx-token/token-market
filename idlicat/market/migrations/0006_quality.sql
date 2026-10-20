-- 0006 质量检测：检测策略、题库、探测记录、判定、报告、申诉、被动健康事件。

CREATE TABLE detection_policies (
    id                     bigserial PRIMARY KEY,
    platform               text           NOT NULL,
    auth_type              text           NOT NULL DEFAULT '',     -- 空串表示该厂商全部接入方式
    enabled                boolean        NOT NULL DEFAULT false,  -- 是否检测（上架检测之外的检测总开关；上架检测始终执行）
    scheduled_enabled      boolean        NOT NULL DEFAULT false,  -- 是否定时检测
    interval_minutes       int            NOT NULL DEFAULT 360 CHECK (interval_minutes >= 10),
    jitter_minutes         int            NOT NULL DEFAULT 30 CHECK (jitter_minutes >= 0),
    suites                 text[]         NOT NULL DEFAULT '{availability,latency,iq}',
    models                 text[]         NOT NULL DEFAULT '{}',
    daily_budget_usd       numeric(20,10) NOT NULL DEFAULT 1,
    items_per_run          int            NOT NULL DEFAULT 12 CHECK (items_per_run > 0),
    thresholds             jsonb          NOT NULL DEFAULT '{"h0": 0.9, "h1": 0.7, "alpha": 0.05, "beta": 0.1}',
    auto_pause             boolean        NOT NULL DEFAULT true,
    default_grade          text           NOT NULL DEFAULT 'B',
    version                bigint         NOT NULL DEFAULT 1,
    updated_by             bigint,
    updated_at             timestamptz    NOT NULL DEFAULT now(),
    UNIQUE (platform, auth_type)
);

INSERT INTO detection_policies (platform, models) VALUES
    ('anthropic',   '{claude-sonnet-4-5}'),
    ('openai',      '{gpt-5.1}'),
    ('gemini',      '{gemini-2.5-pro}'),
    ('antigravity', '{gemini-2.5-pro}'),
    ('grok',        '{grok-4}'),
    ('kimi',        '{kimi-for-coding}'),
    ('zhipu',       '{glm-4.6}'),
    ('minimax',     '{MiniMax-M2}'),
    ('opencode_go', '{kimi-k2.6}');

CREATE TABLE detection_overrides (
    upstream_id        bigint      PRIMARY KEY REFERENCES upstreams (id),
    enabled            boolean,
    scheduled_enabled  boolean,
    expires_at         timestamptz,
    reason             text,
    updated_by         bigint,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE probe_items (
    id                  bigserial PRIMARY KEY,
    suite               text             NOT NULL CHECK (suite IN ('iq', 'reasoning', 'long_context', 'tool_call')),
    category            text             NOT NULL,
    prompt              text             NOT NULL,
    grader              jsonb            NOT NULL,      -- {"type": "exact|contains|regex|numeric|json_keys|choice", ...}
    platforms           text[]           NOT NULL DEFAULT '{}',   -- 空表示全部平台
    max_tokens          int              NOT NULL DEFAULT 1024,
    reasoning_effort    text,
    irt_difficulty      double precision,
    irt_discrimination  double precision,
    status              text             NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'retired')),
    version             int              NOT NULL DEFAULT 1,
    created_at          timestamptz      NOT NULL DEFAULT now(),
    updated_at          timestamptz      NOT NULL DEFAULT now()
);

CREATE TABLE probe_runs (
    id              bigserial PRIMARY KEY,
    upstream_id     bigint         NOT NULL REFERENCES upstreams (id),
    batch_id        text           NOT NULL,     -- 同一批次的被测上游与基线账号共享，便于同时段对照
    trigger         text           NOT NULL CHECK (trigger IN ('onboarding', 'scheduled', 'recovery', 'seller_manual', 'admin', 'adaptive', 'appeal', 'reference')),
    policy_version  bigint         NOT NULL,
    model           text           NOT NULL,
    status          text           NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'skipped_budget', 'skipped_disabled')),
    cost_usd        numeric(20,10),
    summary         jsonb,
    queued_at       timestamptz    NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz
);
CREATE INDEX probe_runs_upstream_idx ON probe_runs (upstream_id, queued_at DESC);
CREATE INDEX probe_runs_queue_idx ON probe_runs (status, queued_at) WHERE status = 'queued';
CREATE INDEX probe_runs_batch_idx ON probe_runs (batch_id);

CREATE TABLE probe_results (
    id                bigserial PRIMARY KEY,
    run_id            bigint           NOT NULL REFERENCES probe_runs (id),
    item_id           bigint REFERENCES probe_items (id),
    suite             text             NOT NULL,
    passed            boolean,
    http_status       int,
    error_class       text,
    ttft_ms           int,
    duration_ms       int,
    output_tokens     int,
    reasoning_tokens  int,
    output_tps        double precision,
    response_model    text,
    sub2api_request_id text,
    raw_excerpt       text,                     -- 截断的原始输出，仅管理员可见
    created_at        timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX probe_results_run_idx ON probe_results (run_id);

CREATE TABLE quality_verdicts (
    id              bigserial PRIMARY KEY,
    upstream_id     bigint           NOT NULL REFERENCES upstreams (id),
    run_id          bigint REFERENCES probe_runs (id),
    model           text             NOT NULL,
    verdict         text             NOT NULL CHECK (verdict IN ('normal', 'suspected', 'degraded', 'unavailable', 'inconclusive')),
    relative_score  double precision,
    reasoning_ratio double precision,
    llr             double precision,
    attribution     text CHECK (attribution IN ('account', 'proxy', 'vendor_wide', 'unknown')),
    grade           text,
    created_at      timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX quality_verdicts_upstream_idx ON quality_verdicts (upstream_id, created_at DESC);

CREATE TABLE test_reports (
    id           bigserial PRIMARY KEY,
    upstream_id  bigint      NOT NULL REFERENCES upstreams (id),
    run_id       bigint      NOT NULL UNIQUE REFERENCES probe_runs (id),
    trigger      text        NOT NULL,
    verdict      text        NOT NULL,
    summary      jsonb       NOT NULL,     -- 卖家可见的脱敏内容
    impact       jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX test_reports_upstream_idx ON test_reports (upstream_id, created_at DESC);

CREATE TABLE appeals (
    id          bigserial PRIMARY KEY,
    report_id   bigint      NOT NULL REFERENCES test_reports (id),
    seller_id   bigint      NOT NULL REFERENCES users (id),
    reason      text        NOT NULL,
    status      text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'retesting', 'upheld', 'overturned')),
    admin_note  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE TABLE health_events (
    id           bigserial PRIMARY KEY,
    upstream_id  bigint      NOT NULL REFERENCES upstreams (id),
    type         text        NOT NULL,
    source       text        NOT NULL CHECK (source IN ('sub2api_account', 'usage', 'edge', 'probe')),
    detail       jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX health_events_upstream_idx ON health_events (upstream_id, created_at DESC);

-- 质量调度状态：恢复复测的退避与连续正常次数
CREATE TABLE upstream_quality_state (
    upstream_id          bigint      PRIMARY KEY REFERENCES upstreams (id),
    last_run_at          timestamptz,
    next_scheduled_at    timestamptz,
    recovery_attempts    int         NOT NULL DEFAULT 0,
    next_recovery_at     timestamptz,
    consecutive_normal   int         NOT NULL DEFAULT 0,
    manual_runs_today    int         NOT NULL DEFAULT 0,
    manual_runs_date     date
);
