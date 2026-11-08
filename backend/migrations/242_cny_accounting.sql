-- Accounting cutover: provider/catalog and administrator prices become CNY.
-- B-class user assets, limits and historical usage amounts MUST NOT be scaled.
-- The migration runner executes this file once, atomically, under its lock.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $$
DECLARE
    target_table text;
    assignments text;
    subscription_rate numeric;
BEGIN
    -- Fixed official-price exchange rate. Multipliers and interval boundaries
    -- are deliberately excluded, including time_pricing and reasoning maps.
    FOREACH target_table IN ARRAY ARRAY[
        'channel_model_pricing', 'channel_pricing_intervals',
        'channel_account_stats_model_pricing', 'channel_account_stats_pricing_intervals'
    ] LOOP
        SELECT string_agg(format('%I = %I * 7', column_name, column_name), ', ')
        INTO assignments
        FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = target_table
          AND column_name = ANY(ARRAY['input_price', 'output_price', 'cache_write_price',
              'cache_write_1h_price', 'cache_read_price', 'image_input_price',
              'image_output_price', 'per_request_price']);
        EXECUTE format('UPDATE %I SET %s', target_table, assignments);
    END LOOP;

    -- Preserve what customers actually paid under the old optional rate.
    SELECT CASE WHEN trim(value) ~ '^[+]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$'
                THEN value::numeric ELSE 0 END
    INTO subscription_rate FROM settings WHERE key = 'SUBSCRIPTION_USD_TO_CNY_RATE';
    IF subscription_rate > 0 THEN
        UPDATE subscription_plans
        SET price = round(price * subscription_rate, 2),
            original_price = round(original_price * subscription_rate, 2);
    END IF;
    UPDATE subscription_plans SET currency = 'CNY';
    DELETE FROM settings WHERE key = 'SUBSCRIPTION_USD_TO_CNY_RATE';
END $$;

-- This temporary recursive function touches absolute price leaves only. It
-- preserves NULL, explicit zeroes, array ordering, tokens, IDs and multipliers.
CREATE OR REPLACE FUNCTION pg_temp.cny_prices(value jsonb) RETURNS jsonb
LANGUAGE plpgsql AS $$
DECLARE
    result jsonb;
BEGIN
    IF jsonb_typeof(value) = 'array' THEN
        SELECT COALESCE(jsonb_agg(pg_temp.cny_prices(v) ORDER BY ord), '[]'::jsonb)
        INTO result FROM jsonb_array_elements(value) WITH ORDINALITY AS a(v, ord);
        RETURN result;
    ELSIF jsonb_typeof(value) = 'object' THEN
        SELECT COALESCE(jsonb_object_agg(k,
            CASE WHEN k = ANY(ARRAY['input_price', 'output_price', 'cache_write_price',
                      'cache_write_1h_price', 'cache_read_price', 'image_input_price',
                      'image_output_price', 'per_request_price']) AND jsonb_typeof(v) = 'number'
                 THEN to_jsonb((v #>> '{}')::numeric * 7)
                 ELSE pg_temp.cny_prices(v) END), '{}'::jsonb)
        INTO result FROM jsonb_each(value) AS o(k, v);
        RETURN result;
    END IF;
    RETURN value;
END $$;

UPDATE groups SET
    image_price_1k = image_price_1k * 7,
    image_price_2k = image_price_2k * 7,
    image_price_4k = image_price_4k * 7,
    video_price_480p = video_price_480p * 7,
    video_price_720p = video_price_720p * 7,
    video_price_1080p = video_price_1080p * 7,
    web_search_price_per_call = web_search_price_per_call * 7,
    search_price_per_1k = search_price_per_1k * 7,
    audio_realtime_price_per_min = audio_realtime_price_per_min * 7,
    audio_tts_price_per_million_chars = audio_tts_price_per_million_chars * 7,
    audio_stt_price_per_hour = audio_stt_price_per_hour * 7,
    model_pricing = pg_temp.cny_prices(model_pricing),
    video_model_prices = CASE WHEN jsonb_typeof(video_model_prices) = 'object' THEN COALESCE((
        SELECT jsonb_object_agg(model, (
            SELECT jsonb_object_agg(resolution, to_jsonb(price::numeric * 7))
            FROM jsonb_each_text(tiers) AS tier(resolution, price)
        )) FROM jsonb_each(video_model_prices) AS models(model, tiers)
    ), '{}'::jsonb) ELSE video_model_prices END;

-- These are provider-cost quotas, NOT user/API-key quotas. Both denominators
-- and live counters change together; account multipliers remain unchanged.
UPDATE accounts SET extra = extra || (
    SELECT jsonb_object_agg(k, to_jsonb((v #>> '{}')::numeric * 7))
    FROM jsonb_each(extra) AS e(k, v)
    WHERE k = ANY(ARRAY['quota_limit', 'quota_used', 'quota_daily_limit', 'quota_daily_used',
        'quota_weekly_limit', 'quota_weekly_used', 'window_cost_limit', 'window_cost_sticky_reserve'])
      AND jsonb_typeof(v) IN ('number', 'string')
      AND trim(v #>> '{}') ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$'
)
WHERE EXISTS (
    SELECT 1 FROM jsonb_each(extra) AS e(k, v)
    WHERE k = ANY(ARRAY['quota_limit', 'quota_used', 'quota_daily_limit', 'quota_daily_used',
        'quota_weekly_limit', 'quota_weekly_used', 'window_cost_limit', 'window_cost_sticky_reserve'])
      AND jsonb_typeof(v) IN ('number', 'string')
      AND trim(v #>> '{}') ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$'
);

-- Fixed account-cost alert thresholds scale with their quota; percentages do not.
UPDATE accounts SET extra = extra || (
    SELECT jsonb_object_agg('quota_notify_' || dim || '_threshold',
        to_jsonb((extra->>('quota_notify_' || dim || '_threshold'))::numeric * 7))
    FROM unnest(ARRAY['daily', 'weekly', 'total']) AS dims(dim)
    WHERE jsonb_typeof(extra->('quota_notify_' || dim || '_threshold')) IN ('number', 'string')
      AND trim(extra->>('quota_notify_' || dim || '_threshold')) ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$'
      AND COALESCE(extra->>('quota_notify_' || dim || '_threshold_type'), 'fixed') <> 'percentage'
)
WHERE EXISTS (
    SELECT 1 FROM unnest(ARRAY['daily', 'weekly', 'total']) AS dims(dim)
    WHERE jsonb_typeof(extra->('quota_notify_' || dim || '_threshold')) IN ('number', 'string')
      AND trim(extra->>('quota_notify_' || dim || '_threshold')) ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$'
      AND COALESCE(extra->>('quota_notify_' || dim || '_threshold_type'), 'fixed') <> 'percentage'
);

-- Refresh persisted scheduler snapshots after the cost quota conversion.
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM accounts WHERE deleted_at IS NULL;

ALTER TABLE subscription_plans ALTER COLUMN currency SET DEFAULT 'CNY';
ALTER TABLE batch_image_jobs ALTER COLUMN currency SET DEFAULT 'CNY';
UPDATE batch_image_jobs SET currency = 'CNY' WHERE currency = 'USD';

COMMENT ON COLUMN subscription_plans.price IS 'Subscription sale price in CNY';
COMMENT ON COLUMN subscription_plans.original_price IS 'Original subscription price in CNY';
COMMENT ON COLUMN users.balance IS 'User balance in CNY; cutover preserves the numeric value';
COMMENT ON COLUMN users.frozen_balance IS 'Frozen user balance in CNY; numeric value preserved';
COMMENT ON COLUMN payment_orders.bonus_amount IS 'CNY recharge bonus included in amount; numeric value preserved';

DO $$
DECLARE c record;
BEGIN
    FOR c IN SELECT table_name, column_name FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name IN ('groups', 'user_subscriptions', 'user_platform_quotas')
          AND column_name LIKE '%\_usd' ESCAPE '\'
    LOOP
        EXECUTE format('COMMENT ON COLUMN %I.%I IS %L', c.table_name, c.column_name,
            'CNY amount; legacy _usd column name retained for compatibility; numeric value preserved');
    END LOOP;
END $$;
