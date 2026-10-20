-- 0018 重置卡：卖家可以手动使用官方的重置卡（Codex、Claude OAuth）；
-- Codex 账号还可以授权平台在窗口用量达到卖家设定的阈值时自动用卡（由 sub2api 执行）。

ALTER TABLE upstreams
    ADD COLUMN auto_reset_enabled       boolean     NOT NULL DEFAULT false,
    ADD COLUMN auto_reset_5h_percent    smallint CHECK (auto_reset_5h_percent BETWEEN 1 AND 100),  -- 空表示跟随暂停线（100 − 自留比例）
    ADD COLUMN auto_reset_7d_percent    smallint CHECK (auto_reset_7d_percent BETWEEN 1 AND 100),
    ADD COLUMN auto_reset_authorized_at timestamptz;                                                -- 最近一次授权自动用卡的时间

-- 用卡记录：卖家手动用卡，以及从 sub2api 同步回来的自动用卡结果。
CREATE TABLE reset_credit_events (
    id           bigserial PRIMARY KEY,
    upstream_id  bigint      NOT NULL REFERENCES upstreams (id),
    seller_id    bigint      NOT NULL REFERENCES users (id),
    trigger      text        NOT NULL CHECK (trigger IN ('manual', 'auto')),
    request_id   text        NOT NULL,   -- 手动：卖家这次点击的请求号（重试返回首次结果）；自动：auto: + sub2api 记录的结果时间
    status       text        NOT NULL CHECK (status IN ('pending', 'success', 'no_credit', 'failed', 'unknown')),
    windows      text[]      NOT NULL DEFAULT '{}',   -- 被重置（手动）或触发用卡（自动）的窗口
    error_code   text,
    detail       jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    UNIQUE (upstream_id, request_id)
);
-- 同一上游同时只允许一次手动用卡在进行中，避免连点消耗多张卡
CREATE UNIQUE INDEX reset_credit_events_pending_idx ON reset_credit_events (upstream_id) WHERE status = 'pending';
CREATE INDEX reset_credit_events_upstream_idx ON reset_credit_events (upstream_id, created_at DESC);
