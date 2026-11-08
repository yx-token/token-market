-- 0054 全系统改按人民币结算（2026-11-08，统一口径见 market/docs/2026-11-08-cny-accounting.md）。
-- 汇率固定 1 USD = 7 CNY。用户资产与用户额度：数值不变，只把单位改成 CNY；
-- 平台按官方成本计量的量（检测预算、写入引擎账号的成本额度）：×7。
-- 所有换算都在 numeric 中做精确乘法，目标列精度放得下乘积，不做任何舍入。

-- ---------------------------------------------------------------------------
-- 1. 店铺活动包（B 类）：美元面额改为平台余额单位（人民币），兑换率恒为 1。
-- ---------------------------------------------------------------------------

-- 1.1 商品：按当前 credit_usd_to_balance_rate 设置换算（未设置时为默认值 1）。
--     numeric(12,2) × numeric(16,8) 的乘积最多 10 位小数、18 位整数，numeric(28,10) 能精确放下。
ALTER TABLE store_credit_offers ALTER COLUMN face_value_usd TYPE numeric(28,10);
UPDATE store_credit_offers o SET face_value_usd = o.face_value_usd * r.rate
FROM (SELECT COALESCE((SELECT (value->>'credit_usd_to_balance_rate')::numeric FROM settings WHERE key = 'platform'), 1) AS rate) r;
ALTER TABLE store_credit_offers RENAME COLUMN face_value_usd TO face_value_cny;
ALTER TABLE store_credit_offers RENAME CONSTRAINT store_credit_offers_face_value_usd_check TO store_credit_offers_face_value_cny_check;
-- 换算前购买时就会校验「面额 × 兑换率」在 0.00000001～1000000000 之间且最多 8 位小数，
-- 不满足的商品本来就买不了。购买侧去掉这项校验后，把它们下架，卖家须按人民币重新填写面额
-- （新面额走商品保存校验：最多两位小数、不超过 1000000000）后才能再上架。
UPDATE store_credit_offers SET status = 'paused', updated_at = now()
WHERE status = 'active'
  AND (face_value_cny < 0.00000001 OR face_value_cny > 1000000000 OR face_value_cny <> round(face_value_cny, 8));

-- 1.2 已购活动包：面额 = 美元面额 × 购买时快照的兑换率。
--     余额与每笔已抵扣额一直按平台单位精确记在 remaining_balance_units / covered_balance_units，
--     退款、对账、隔离钱包开通都以它们为准；remaining_usd / covered_usd 只是
--     round(平台单位 / 兑换率, 16) 的派生展示值，乘回兑换率与平台单位最多差 兑换率 × 5e-17。
--     因此不另存「remaining_usd × 兑换率」这份带尾差的副本：API 的 remaining_cny / covered_cny
--     直接取平台单位列，美元列删除。下面的检查在 psql 中执行时会打印差异，供人工核对。
DO $$
DECLARE
    pack_rows bigint;
    pack_max numeric;
    alloc_rows bigint;
    alloc_max numeric;
BEGIN
    SELECT count(*), max(abs(remaining_usd * usd_to_balance_rate - remaining_balance_units))
      INTO pack_rows, pack_max
      FROM store_credit_packs
     WHERE remaining_usd * usd_to_balance_rate <> remaining_balance_units;
    SELECT count(*), max(abs(a.covered_usd * p.usd_to_balance_rate - a.covered_balance_units))
      INTO alloc_rows, alloc_max
      FROM store_credit_allocations a JOIN store_credit_packs p ON p.id = a.pack_id
     WHERE a.covered_usd * p.usd_to_balance_rate <> a.covered_balance_units;
    RAISE NOTICE 'store credit CNY migration: % packs differ from remaining_balance_units (max %), % allocations differ from covered_balance_units (max %)',
        pack_rows, COALESCE(pack_max, 0), alloc_rows, COALESCE(alloc_max, 0);
END $$;

-- 先删 remaining_usd（连同它与面额比较的 CHECK），否则兑换率 < 1 时缩小面额会违反旧约束。
ALTER TABLE store_credit_packs DROP COLUMN remaining_usd;
-- 购买时 remaining_balance_units = 面额 × 兑换率 写入 numeric(20,10)，乘积最多 10 位小数，numeric(28,10) 精确放下。
ALTER TABLE store_credit_packs ALTER COLUMN face_value_usd TYPE numeric(28,10);
UPDATE store_credit_packs SET face_value_usd = face_value_usd * usd_to_balance_rate;
ALTER TABLE store_credit_packs RENAME COLUMN face_value_usd TO face_value_cny;
ALTER TABLE store_credit_packs RENAME CONSTRAINT store_credit_packs_face_value_usd_check TO store_credit_packs_face_value_cny_check;
ALTER TABLE store_credit_packs DROP COLUMN usd_to_balance_rate;
-- 取代旧的 remaining_usd <= face_value_usd。历史行已由旧约束保证（平台单位最多 10 位小数，
-- 越界至少差 1e-10，远大于美元列的舍入误差）；NOT VALID 只是避免人工改过的异常行阻断升级，新写入照常受约束。
ALTER TABLE store_credit_packs ADD CONSTRAINT store_credit_packs_remaining_le_face
    CHECK (remaining_balance_units <= face_value_cny) NOT VALID;

ALTER TABLE store_credit_allocations DROP COLUMN covered_usd;

-- 兑换率恒为 1，去掉平台设置项。
UPDATE settings SET value = value - 'credit_usd_to_balance_rate', updated_at = now()
WHERE key = 'platform' AND value ? 'credit_usd_to_balance_rate';

-- ---------------------------------------------------------------------------
-- 2. 检测策略每日预算（A 类，按 usage 官方成本累计）：×7，改为人民币。
-- ---------------------------------------------------------------------------
-- 加宽一位整数，保证历史值 ×7 不溢出。
ALTER TABLE detection_policies ALTER COLUMN daily_budget_usd TYPE numeric(21,10);
UPDATE detection_policies SET daily_budget_usd = daily_budget_usd * 7;
ALTER TABLE detection_policies RENAME COLUMN daily_budget_usd TO daily_budget_cny;
ALTER TABLE detection_policies ALTER COLUMN daily_budget_cny SET DEFAULT 7;
-- 未结束的检测批次沿用 execution_snapshot 里冻结的策略，同步换算，避免旧键被当成 0 预算而全部跳过。
UPDATE probe_runs
   SET execution_snapshot = jsonb_set(execution_snapshot #- '{policy,daily_budget_usd}', '{policy,daily_budget_cny}',
           to_jsonb((execution_snapshot #>> '{policy,daily_budget_usd}')::numeric * 7))
 WHERE status IN ('queued', 'running', 'third_party')
   AND jsonb_typeof(execution_snapshot -> 'policy') = 'object'
   AND jsonb_typeof(execution_snapshot #> '{policy,daily_budget_usd}') = 'number';

-- ---------------------------------------------------------------------------
-- 3. 写入引擎账号 extra 的成本额度（A 类）：×7。
-- ---------------------------------------------------------------------------
-- 引擎按 usage 官方成本（× 账号倍率）累计这些窗口，同一版本里引擎会把 accounts.extra 的同名键 ×7。
-- 市场下发的源配置是 vendor_configs.extra_limits（调度器逐轮比对后写回引擎），
-- upstream_runtime_params.extra_limits 是它的期望值快照；两处必须一起换算，否则调度器会把旧值写回引擎。
-- 只换算按成本计量的键；max_sessions、base_rpm 等计数类键不动。数字字符串保持字符串类型。
UPDATE vendor_configs SET extra_limits = (
    SELECT jsonb_object_agg(k, CASE
        WHEN k IN ('window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit')
             AND jsonb_typeof(v) = 'number' THEN to_jsonb((v #>> '{}')::numeric * 7)
        WHEN k IN ('window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit')
             AND jsonb_typeof(v) = 'string' AND (v #>> '{}') ~ '^\s*-?([0-9]+\.?[0-9]*|\.[0-9]+)\s*$'
             THEN to_jsonb(((v #>> '{}')::numeric * 7)::text)
        ELSE v END)
    FROM jsonb_each(extra_limits) AS t(k, v))
WHERE jsonb_typeof(extra_limits) = 'object'
  AND extra_limits ?| ARRAY['window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit'];

UPDATE upstream_runtime_params SET extra_limits = (
    SELECT jsonb_object_agg(k, CASE
        WHEN k IN ('window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit')
             AND jsonb_typeof(v) = 'number' THEN to_jsonb((v #>> '{}')::numeric * 7)
        WHEN k IN ('window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit')
             AND jsonb_typeof(v) = 'string' AND (v #>> '{}') ~ '^\s*-?([0-9]+\.?[0-9]*|\.[0-9]+)\s*$'
             THEN to_jsonb(((v #>> '{}')::numeric * 7)::text)
        ELSE v END)
    FROM jsonb_each(extra_limits) AS t(k, v))
WHERE jsonb_typeof(extra_limits) = 'object'
  AND extra_limits ?| ARRAY['window_cost_limit', 'window_cost_sticky_reserve', 'quota_limit', 'quota_daily_limit', 'quota_weekly_limit'];

-- ---------------------------------------------------------------------------
-- 4. 买家 Key 限额（B 类）：数值不变，键名从 *_usd 改为 *_cny。
-- ---------------------------------------------------------------------------
-- 这些限额一直与按人民币扣费的 usage_records.actual_cost 比较，/v1/usage 也早已标注 CNY，只是键名沿用了 _usd。
UPDATE buyer_keys
   SET limits = (limits - ARRAY['quota_usd', 'rate_limit_5h_usd', 'rate_limit_1d_usd', 'rate_limit_7d_usd'])
       || jsonb_strip_nulls(jsonb_build_object(
              'quota_cny', limits -> 'quota_usd',
              'rate_limit_5h_cny', limits -> 'rate_limit_5h_usd',
              'rate_limit_1d_cny', limits -> 'rate_limit_1d_usd',
              'rate_limit_7d_cny', limits -> 'rate_limit_7d_usd'))
 WHERE jsonb_typeof(limits) = 'object'
   AND limits ?| ARRAY['quota_usd', 'rate_limit_5h_usd', 'rate_limit_1d_usd', 'rate_limit_7d_usd'];
