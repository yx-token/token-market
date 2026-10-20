-- 0004 默认分组与调度期望状态。

CREATE TABLE pools (
    id                bigserial PRIMARY KEY,
    name              text          NOT NULL UNIQUE,          -- 同时是 sub2api 分组名，如 tm-pool-claude
    platform          text          NOT NULL,                 -- composite 表示全模型分组
    sub2api_group_id  bigint UNIQUE,
    sell_multiplier   numeric(10,4) NOT NULL,                 -- D
    min_margin        numeric(6,4)  NOT NULL DEFAULT 0.10,
    safety_buffer     numeric(6,4)  NOT NULL DEFAULT 0.02,
    enabled           boolean       NOT NULL DEFAULT true,
    created_at        timestamptz   NOT NULL DEFAULT now(),
    updated_at        timestamptz   NOT NULL DEFAULT now()
);

-- 每个上游在 sub2api 中账号级参数的期望值，由调度器计算，执行器下发
CREATE TABLE upstream_runtime_params (
    upstream_id       bigint        PRIMARY KEY REFERENCES upstreams (id),
    buyer_facing      boolean       NOT NULL,
    group_ids         bigint[]      NOT NULL,
    priority          int           NOT NULL,
    concurrency       int           NOT NULL,
    load_factor       int,
    rate_multiplier   numeric(10,4) NOT NULL,
    window_threshold  smallint,
    extra_limits      jsonb         NOT NULL DEFAULT '{}',
    reason            text,
    computed_at       timestamptz   NOT NULL DEFAULT now(),
    -- 窗口阈值写在 sub2api 账号 credentials 中，只读库不读取凭证，所以记录最近一次下发的值用于比对
    applied_window_threshold smallint,
    applied_at        timestamptz
);
