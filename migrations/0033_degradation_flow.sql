-- 新增独立的「降智检测」流程：发现厂商把请求静默路由到其他型号的题目单独成流程、单独配模型。
-- 存量策略先继承测智的模型列表与周期，避免线上已有的限定厂商/型号降智题因新流程没有模型而静默停跑；
-- 定时检测保持关闭，由管理员确认检测站与题目后就绪再开启。本迁移可重复执行。

-- 1. 存量策略补 flows.degradation（已存在则不覆盖，管理员改过的配置不会被重置）。
UPDATE detection_policies AS policy
SET flows = jsonb_set(flows, '{degradation}', jsonb_build_object(
        'enabled', true,
        'scheduled_enabled', false,
        'interval_minutes', COALESCE(NULLIF(flows->'intelligence'->>'interval_minutes', '')::int, interval_minutes),
        'triggers', COALESCE(flows->'intelligence'->'triggers', '[]'::jsonb),
        'full_triggers', COALESCE(flows->'intelligence'->'full_triggers', '[]'::jsonb),
        'models', COALESCE(flows->'intelligence'->'models', '[]'::jsonb)
    ), true)
WHERE NOT (flows ? 'degradation');

-- 2. 运行记录分别冻结两个测智类流程的模型集合。
ALTER TABLE probe_runs ADD COLUMN IF NOT EXISTS degradation_models text[] NOT NULL DEFAULT '{}';

-- 3. 降智流程按 next_degradation_at 到期挑选。
ALTER TABLE upstream_quality_state
    ADD COLUMN IF NOT EXISTS next_degradation_at timestamptz,
    ADD COLUMN IF NOT EXISTS last_degradation_at timestamptz;

-- 4. 题目套件放宽到独立的降智套件；先放开约束再迁移题目，避免旧约束挡住 UPDATE。
ALTER TABLE probe_items DROP CONSTRAINT IF EXISTS probe_items_suite_check;
ALTER TABLE probe_items ADD CONSTRAINT probe_items_suite_check CHECK
    (suite IN ('iq', 'reasoning', 'long_context', 'tool_call', 'inspection_authenticity',
              'inspection_capability', 'inspection_degradation', 'inspection_context', 'inspection_function',
              'inspection_output', 'inspection_performance'));

-- 5. 限定厂商或型号的旧测智题实际是降智题：原地迁入独立的降智套件与 D14，保留 ID 与全部历史结果。
--    通用的测智题（platforms 与 models 都为空，如 ID=23/24/25）保持不动。
UPDATE probe_items
SET suite = 'inspection_degradation',
    check_key = 'D14',
    version = version + 1,
    updated_at = now()
WHERE suite = 'inspection_capability'
  AND status = 'active'
  AND (cardinality(platforms) > 0 OR cardinality(models) > 0);
