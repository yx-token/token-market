-- 0009 第三方检测：检测站配置、检测策略中的第三方检测项、每次检测提交给检测站的记录。

CREATE TABLE detection_providers (
    name               text        PRIMARY KEY,                  -- 与 internal/quality/thirdparty 中登记的名称一致
    enabled            boolean     NOT NULL DEFAULT false,
    base_url           text        NOT NULL,
    api_key_enc        bytea,                                    -- 需要鉴权的检测站使用，加密存储
    options            jsonb       NOT NULL DEFAULT '{}',        -- 检测站的可选参数，以及 allow_insecure_target（仅开发环境）
    max_runs_per_hour  int         NOT NULL DEFAULT 20 CHECK (max_runs_per_hour > 0),
    timeout_minutes    int         NOT NULL DEFAULT 60 CHECK (timeout_minutes >= 5),  -- 从排队到出结果的最长等待
    updated_by         bigint,
    updated_at         timestamptz NOT NULL DEFAULT now()
);
INSERT INTO detection_providers (name, base_url, max_runs_per_hour) VALUES ('bazaarlink', 'https://bazaarlink.ai', 25);

-- 策略中的第三方检测项：[{"provider": "bazaarlink", "triggers": ["onboarding", ...], "model": "", "format": "",
--   "on_mismatch": "degraded", "on_submodel_mismatch": "suspected"}]
ALTER TABLE detection_policies ADD COLUMN third_party jsonb NOT NULL DEFAULT '[]';

-- 内部探测完成后等待第三方检测结果的检测记录处于 third_party 状态
ALTER TABLE probe_runs DROP CONSTRAINT probe_runs_status_check;
ALTER TABLE probe_runs ADD CONSTRAINT probe_runs_status_check
    CHECK (status IN ('queued', 'running', 'third_party', 'done', 'failed', 'skipped_budget', 'skipped_disabled'));

CREATE TABLE third_party_checks (
    id               bigserial PRIMARY KEY,
    run_id           bigint      NOT NULL REFERENCES probe_runs (id),
    upstream_id      bigint      NOT NULL REFERENCES upstreams (id),
    provider         text        NOT NULL,
    model            text        NOT NULL,
    format           text        NOT NULL CHECK (format IN ('openai', 'anthropic')),
    config           jsonb       NOT NULL,                       -- 提交时的策略检测项快照（结论折算规则）
    status           text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'timeout', 'skipped')),
    external_id      text,                                       -- 检测站的检测编号
    token_hash       text,                                       -- 交给检测站的 Edge 临时令牌（sha256），结束后撤销
    token_revoked    boolean     NOT NULL DEFAULT false,
    outcome          text,                                       -- pass / submodel_mismatch / mismatch / spoof / inconclusive
    verdict          text,                                       -- 折算后的结论：normal / suspected / degraded / inconclusive
    summary          jsonb,                                      -- 脱敏后的结果摘要
    report_url       text,
    error            text,
    attempts         int         NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    deadline         timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    submitted_at     timestamptz,
    finished_at      timestamptz
);
CREATE INDEX third_party_checks_run_idx ON third_party_checks (run_id);
CREATE INDEX third_party_checks_due_idx ON third_party_checks (next_attempt_at) WHERE status IN ('pending', 'running');
CREATE INDEX third_party_checks_upstream_idx ON third_party_checks (upstream_id, created_at DESC);
CREATE INDEX third_party_checks_submitted_idx ON third_party_checks (provider, submitted_at);
