-- 测智与身份核验分别配置和调度，旧字段保留以兼容历史报告及回退版本。
-- 测智流程默认启用：厂商「是否检测」总开关仍只管上架以外的检测，上架检测始终测智（决策 D18），
-- 不能把旧总开关的关闭状态带进流程开关，否则未开检测的厂商上架时会跳过测智。
ALTER TABLE detection_policies ADD COLUMN flows jsonb NOT NULL DEFAULT '{}';

UPDATE detection_policies AS policy
SET flows = jsonb_build_object(
    'intelligence', jsonb_build_object(
        'enabled', true,
        'scheduled_enabled', policy.scheduled_enabled,
        'interval_minutes', policy.interval_minutes,
        'triggers', jsonb_build_array('onboarding', 'scheduled', 'recovery', 'seller_manual', 'admin', 'adaptive', 'appeal'),
        'full_triggers', to_jsonb(policy.full_triggers),
        'models', COALESCE((
            SELECT jsonb_agg(jsonb_build_object('model', model, 'expected_pass_rate', NULL) ORDER BY ordinal)
            FROM unnest(policy.models) WITH ORDINALITY AS models(model, ordinal)
        ), '[]'::jsonb)
    ),
    'identity', jsonb_build_object(
        'enabled', jsonb_array_length(policy.third_party) > 0,
        'source', 'third_party',
        'scheduled_enabled', policy.scheduled_enabled AND EXISTS (
            SELECT 1 FROM jsonb_array_elements(policy.third_party) AS site
            WHERE COALESCE(site->'triggers', '[]'::jsonb) ? 'scheduled'
        ),
        'interval_minutes', policy.interval_minutes,
        'triggers', COALESCE((
            SELECT jsonb_agg(trigger ORDER BY trigger)
            FROM (
                SELECT DISTINCT trigger
                FROM jsonb_array_elements(policy.third_party) AS site
                CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(site->'triggers', '[]'::jsonb)) AS trigger
            ) AS triggers
        ), '[]'::jsonb),
        'full_triggers', jsonb_build_array('onboarding', 'scheduled', 'recovery', 'seller_manual', 'admin', 'adaptive', 'appeal')
    )
);

-- 运行记录冻结流程、身份来源和全部测智模型；旧运行不补推断出的流程。
ALTER TABLE probe_runs
    ADD COLUMN flows text[] NOT NULL DEFAULT '{}',
    ADD COLUMN identity_source text CHECK (identity_source IN ('third_party', 'self')),
    ADD COLUMN models text[] NOT NULL DEFAULT '{}';

ALTER TABLE upstream_quality_state
    ADD COLUMN next_intelligence_at timestamptz,
    ADD COLUMN next_identity_at timestamptz,
    ADD COLUMN last_intelligence_at timestamptz,
    ADD COLUMN last_identity_at timestamptz,
    ADD COLUMN suspected_flows text[] NOT NULL DEFAULT '{}',
    ADD COLUMN adaptive_attempts integer NOT NULL DEFAULT 0 CHECK (adaptive_attempts >= 0),
    ADD COLUMN deductions_reset_at timestamptz;
UPDATE upstream_quality_state
SET next_intelligence_at = next_scheduled_at,
    next_identity_at = next_scheduled_at;

-- 同次运行重入不能重复向检测站提交。
CREATE UNIQUE INDEX third_party_checks_run_provider_idx ON third_party_checks(run_id, provider);

-- NULL 表示卖家尚未声明白名单；白名单的写入由接入流程负责。
ALTER TABLE upstreams ADD COLUMN model_whitelist text[];

-- 带型号的旧题已有明确能力用途，原地迁移保留历史关联与管理员配置。
UPDATE probe_items
SET suite = 'inspection_capability',
    check_key = 'D07',
    modes = ARRAY['full', 'quick']::text[],
    version = version + 1,
    updated_at = now()
WHERE suite IN ('iq', 'reasoning') AND cardinality(models) > 0;

UPDATE probe_items
SET status = 'retired', version = version + 1, updated_at = now()
WHERE suite IN ('iq', 'reasoning', 'long_context') AND status <> 'retired';
