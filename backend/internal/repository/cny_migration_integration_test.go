//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/migrations"
)

func TestCNYMigrationConvertsPricesAndPreservesAssets(t *testing.T) {
	body, err := migrations.FS.ReadFile("242_cny_accounting.sql")
	require.NoError(t, err)
	for index, scenario := range []struct {
		rate          string
		expectedPrice float64
	}{
		{"7.15", 71.43}, {"0", 9.99}, {"invalid", 9.99},
		{".5", 5}, {"7.", 69.93}, {"1e1", 99.9}, {"-1", 9.99}, {"", 9.99},
	} {
		t.Run(scenario.rate, func(t *testing.T) {
			ctx := context.Background()
			tx := testTx(t)
			schema := fmt.Sprintf("cny_cutover_%d", index)
			_, err := tx.ExecContext(ctx, "CREATE SCHEMA "+schema)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema+", public")
			require.NoError(t, err)
			// Clone the actual migrated columns, without unrelated NOT NULL/FK
			// constraints so the fixture can focus on currency boundaries.
			for _, table := range []string{"channel_model_pricing", "channel_pricing_intervals",
				"channel_account_stats_model_pricing", "channel_account_stats_pricing_intervals",
				"subscription_plans", "settings", "groups", "accounts", "scheduler_outbox",
				"batch_image_jobs", "users", "payment_orders", "api_keys", "usage_logs",
				"user_subscriptions", "user_platform_quotas", "redeem_codes"} {
				_, err = tx.ExecContext(ctx, "CREATE TABLE "+table+" AS SELECT * FROM public."+table+" WITH NO DATA")
				require.NoError(t, err)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO channel_model_pricing (input_price, cache_write_1h_price, fast_multiplier)
				VALUES (0.125, 0.25, 2);
				INSERT INTO groups (image_price_1k, daily_limit_usd, rate_multiplier, model_pricing, video_model_prices)
				VALUES (0.02, 100, 2,
				 '[{"input_price":0.1,"fast_multiplier":2,"intervals":[{"input_price":0.2,"input_multiplier":3}]}]',
				 '{"grok-imagine-video":{"720p":0.14}}');
				INSERT INTO accounts (id, extra) VALUES (1,
				 '{"quota_limit":"100","quota_used":4,"quota_daily_limit":10,"quota_daily_used":2,"quota_weekly_limit":50,"quota_weekly_used":3,"window_cost_limit":20,"window_cost_sticky_reserve":10,"quota_notify_daily_threshold":"2","quota_notify_weekly_threshold":30,"quota_notify_weekly_threshold_type":"percentage","rate_multiplier":2}');
				INSERT INTO subscription_plans (price, original_price, currency) VALUES (9.99,19.99,'USD');
				INSERT INTO users (balance, frozen_balance) VALUES (12.34,2.5);
				INSERT INTO payment_orders (amount, bonus_amount, pay_amount, provider_snapshot) VALUES (20,3,71.43,'{"currency":"USD"}');
				INSERT INTO api_keys (quota, quota_used, rate_limit_5h) VALUES (100,5,10);
				INSERT INTO usage_logs (total_cost, actual_cost) VALUES (0.5,0.25);
				INSERT INTO user_subscriptions (daily_usage_usd) VALUES (2.5);
				INSERT INTO user_platform_quotas (daily_limit_usd, daily_usage_usd) VALUES (50,5);
				INSERT INTO redeem_codes (value) VALUES (20);
				INSERT INTO batch_image_jobs (currency, base_unit_price, hold_amount) VALUES ('USD',0.02,0.01);
			`)
			require.NoError(t, err)
			if scenario.rate != "" {
				_, err = tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('SUBSCRIPTION_USD_TO_CNY_RATE', $1)", scenario.rate)
				require.NoError(t, err)
			}
			_, err = tx.ExecContext(ctx, string(body))
			require.NoError(t, err)
			checks := map[string]float64{
				"SELECT input_price FROM channel_model_pricing":                                       0.875,
				"SELECT cache_write_1h_price FROM channel_model_pricing":                              1.75,
				"SELECT fast_multiplier FROM channel_model_pricing":                                   2,
				"SELECT image_price_1k FROM groups":                                                   0.14,
				"SELECT (model_pricing->0->'intervals'->0->>'input_price')::numeric FROM groups":      1.4,
				"SELECT (model_pricing->0->'intervals'->0->>'input_multiplier')::numeric FROM groups": 3,
				"SELECT (video_model_prices->'grok-imagine-video'->>'720p')::numeric FROM groups":     0.98,
				"SELECT (extra->>'quota_limit')::numeric FROM accounts":                               700,
				"SELECT (extra->>'quota_used')::numeric FROM accounts":                                28,
				"SELECT (extra->>'quota_daily_limit')::numeric FROM accounts":                         70,
				"SELECT (extra->>'quota_weekly_used')::numeric FROM accounts":                         21,
				"SELECT (extra->>'window_cost_sticky_reserve')::numeric FROM accounts":                70,
				"SELECT (extra->>'quota_notify_daily_threshold')::numeric FROM accounts":              14,
				"SELECT (extra->>'quota_notify_weekly_threshold')::numeric FROM accounts":             30,
				"SELECT balance FROM users":                                                           12.34,
				"SELECT frozen_balance FROM users":                                                    2.5,
				"SELECT daily_limit_usd FROM groups":                                                  100,
				"SELECT amount FROM payment_orders":                                                   20,
				"SELECT bonus_amount FROM payment_orders":                                             3,
				"SELECT pay_amount FROM payment_orders":                                               71.43,
				"SELECT quota FROM api_keys":                                                          100,
				"SELECT quota_used FROM api_keys":                                                     5,
				"SELECT total_cost FROM usage_logs":                                                   0.5,
				"SELECT actual_cost FROM usage_logs":                                                  0.25,
				"SELECT daily_usage_usd FROM user_subscriptions":                                      2.5,
				"SELECT daily_limit_usd FROM user_platform_quotas":                                    50,
				"SELECT value FROM redeem_codes":                                                      20,
				"SELECT base_unit_price FROM batch_image_jobs":                                        0.02,
				"SELECT hold_amount FROM batch_image_jobs":                                            0.01,
				"SELECT count(*) FROM settings":                                                       0,
				"SELECT count(*) FROM scheduler_outbox":                                               1,
			}
			checks["SELECT price FROM subscription_plans"] = scenario.expectedPrice
			checks["SELECT original_price FROM subscription_plans"] = map[string]float64{
				"7.15": 142.93, "0": 19.99, "invalid": 19.99, ".5": 10,
				"7.": 139.93, "1e1": 199.9, "-1": 19.99, "": 19.99,
			}[scenario.rate]
			for query, expected := range checks {
				var actual float64
				require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&actual), query)
				require.InDelta(t, expected, actual, 1e-12, query)
			}
			var label string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT currency FROM subscription_plans").Scan(&label))
			require.Equal(t, "CNY", label)
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT currency FROM batch_image_jobs").Scan(&label))
			require.Equal(t, "CNY", label)
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT provider_snapshot->>'currency' FROM payment_orders").Scan(&label))
			require.Equal(t, "USD", label, "gateway settlement currency must remain refundable")
		})
	}
}
