//go:build integration

package migrations_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

const cnyMigration = "0054_cny_currency.sql"

// migrateBefore 执行 name 之前的全部迁移，得到升级前的真实库结构。
func migrateBefore(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := fstest.MapFS{}
	for _, n := range names {
		if n >= name {
			continue
		}
		raw, err := migrations.FS.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		before[n] = &fstest.MapFile{Data: raw}
	}
	if _, err := db.Migrate(context.Background(), pool, before); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

func mustText(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&out); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	return out
}

func jsonEqual(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("decode %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("decode %s: %v", want, err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("json mismatch:\n got  %s\n want %s", gb, wb)
	}
}

// 0054 把市场侧金额改为人民币口径：店铺活动包按兑换率精确换算后去掉兑换率，
// 检测预算与写入引擎的成本额度 ×7，买家 Key 限额只改键名。
func TestCNYMigrationConvertsMarketAmounts(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	migrateBefore(t, pool, cnyMigration)

	// 兑换率取一个乘出 10 位小数的值，验证精确乘积与「买不了的商品下架」。
	mustExec(t, pool, `INSERT INTO settings(key,value) VALUES('platform','{"credit_usd_to_balance_rate":1.23456789,"withdraw_min":12}')`)
	var sellerID, buyerID, upstreamID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('cny-seller@example.test','x') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('cny-buyer@example.test','x') RETURNING id`).Scan(&buyerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id)
		VALUES($1,'anthropic','oauth','listed',301) RETURNING id`, sellerID).Scan(&upstreamID); err != nil {
		t.Fatal(err)
	}

	// 商品：10 → 12.3456789（仍可买）；0.01 → 0.0123456789（超过 8 位小数，下架）；
	// 999999999.99 → 超过 1e9（下架）；已暂停的 1 → 1.23456789（保持暂停）。
	var offerOK, offerTiny, offerHuge, offerPaused int64
	for _, o := range []struct {
		id     *int64
		face   string
		status string
	}{{&offerOK, "10", "active"}, {&offerTiny, "0.01", "active"}, {&offerHuge, "999999999.99", "active"}, {&offerPaused, "1", "paused"}} {
		if err := pool.QueryRow(ctx, `INSERT INTO store_credit_offers(seller_id,title,price_cny,face_value_usd,status)
			VALUES($1,'offer',1,$2::numeric,$3) RETURNING id`, sellerID, o.face, o.status).Scan(o.id); err != nil {
			t.Fatal(err)
		}
	}

	// 活动包 1：兑换率 7，按代码路径已消费 0.5 美元（3.5 单位）。
	// 活动包 2：兑换率 3，已抵扣 2 单位，美元列是 round(units/3,16) 的派生值，乘回去有尾差。
	var pack1, pack2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,remaining_usd,
		remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,status,idempotency_key)
		VALUES($1,$2,$3,'p1',30,10,9.5,66.5,28.5,7,'active','p1') RETURNING id`, offerOK, buyerID, sellerID).Scan(&pack1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,remaining_usd,
		remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,status,idempotency_key)
		VALUES($1,$2,$3,'p2',3,1,round(1::numeric/3,16),1,1,3,'active','p2') RETURNING id`, offerOK, buyerID, sellerID).Scan(&pack2); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO usage_records(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,sub2api_api_key_id,
		model,total_cost,actual_cost,upstream_id,seller_id,buyer_user_id,channel,credit_pack_id,credit_covered_units)
		VALUES(9001,now(),301,201,501,'claude-sonnet-4-5',2,2,$1,$2,$3,'direct',$4,2)`, upstreamID, sellerID, buyerID, pack2)
	mustExec(t, pool, `INSERT INTO store_credit_allocations(usage_id,pack_id,buyer_id,seller_id,model,platform,covered_usd,covered_balance_units,prepaid_cny,cash_cny)
		VALUES(9001,$1,$2,$3,'claude-sonnet-4-5','anthropic',round(2::numeric/3,16),2,2,0)`, pack2, buyerID, sellerID)

	// 检测预算与冻结在未结束批次里的策略快照。
	mustExec(t, pool, `UPDATE detection_policies SET daily_budget_usd=2.5 WHERE platform='anthropic' AND auth_type=''`)
	var queuedRun, doneRun int64
	for _, r := range []struct {
		id     *int64
		status string
	}{{&queuedRun, "queued"}, {&doneRun, "done"}} {
		if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,execution_snapshot)
			VALUES($1,'batch-'||$2,'scheduled',1,'claude-sonnet-4-5',$2,'{"schema_version":1,"policy":{"platform":"anthropic","daily_budget_usd":2.5,"enabled":true}}')
			RETURNING id`, upstreamID, r.status).Scan(r.id); err != nil {
			t.Fatal(err)
		}
	}

	// 写入引擎账号 extra 的成本额度（含数字字符串）与计数类参数。
	const legacyLimits = `{"max_sessions":3,"base_rpm":15,"window_cost_limit":50,"window_cost_sticky_reserve":2.5,
		"quota_limit":100,"quota_daily_limit":"12.5","quota_weekly_limit":0.1,"quota_daily_reset_mode":"fixed"}`
	const wantLimits = `{"max_sessions":3,"base_rpm":15,"window_cost_limit":350,"window_cost_sticky_reserve":17.5,
		"quota_limit":700,"quota_daily_limit":"87.5","quota_weekly_limit":0.7,"quota_daily_reset_mode":"fixed"}`
	mustExec(t, pool, `UPDATE vendor_configs SET extra_limits=$1::jsonb WHERE platform='anthropic'`, legacyLimits)
	mustExec(t, pool, `UPDATE vendor_configs SET extra_limits='null'::jsonb WHERE platform='gemini'`)
	mustExec(t, pool, `INSERT INTO upstream_runtime_params(upstream_id,buyer_facing,group_ids,priority,concurrency,rate_multiplier,extra_limits)
		VALUES($1,true,'{1}',1,2,1,$2::jsonb)`, upstreamID, legacyLimits)

	// 买家 Key 限额：只改键名。
	var limitedKey, plainKey int64
	if err := pool.QueryRow(ctx, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type,limits)
		VALUES($1,'limited','\x01','sk-1','default','{"quota_usd":5,"rate_limit_5h_usd":1.25,"rate_limit_7d_usd":null,"ip_whitelist":["10.0.0.1"],"expires_at":"2030-01-01T00:00:00Z"}')
		RETURNING id`, buyerID).Scan(&limitedKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type,limits)
		VALUES($1,'plain','\x02','sk-2','default','{"ip_whitelist":["10.0.0.2"]}') RETURNING id`, buyerID).Scan(&plainKey); err != nil {
		t.Fatal(err)
	}

	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 || applied[0] != cnyMigration {
		t.Fatalf("expected %s to be applied first, got %v", cnyMigration, applied)
	}

	// 商品面额精确换算，不舍入。
	for _, want := range []struct {
		id     int64
		face   string
		status string
	}{{offerOK, "12.3456789000", "active"}, {offerTiny, "0.0123456789", "paused"},
		{offerHuge, "1234567889.9876543211", "paused"}, {offerPaused, "1.2345678900", "paused"}} {
		got := mustText(t, pool, `SELECT face_value_cny::text||'|'||status FROM store_credit_offers WHERE id=$1`, want.id)
		if got != want.face+"|"+want.status {
			t.Errorf("offer %d: got %s, want %s|%s", want.id, got, want.face, want.status)
		}
	}
	// 活动包面额 = 美元面额 × 快照兑换率；余额沿用精确的平台单位。
	for _, want := range []struct {
		id   int64
		face string
		left string
	}{{pack1, "70.0000000000", "66.5000000000"}, {pack2, "3.0000000000", "1.0000000000"}} {
		got := mustText(t, pool, `SELECT face_value_cny::text||'|'||remaining_balance_units::text FROM store_credit_packs WHERE id=$1`, want.id)
		if got != want.face+"|"+want.left {
			t.Errorf("pack %d: got %s, want %s|%s", want.id, got, want.face, want.left)
		}
	}
	if got := mustText(t, pool, `SELECT covered_balance_units::text FROM store_credit_allocations WHERE usage_id=9001`); got != "2.0000000000" {
		t.Errorf("allocation covered units changed: %s", got)
	}
	if got := mustText(t, pool, `SELECT COALESCE(string_agg(table_name||'.'||column_name, ',' ORDER BY table_name, column_name),'') FROM information_schema.columns
		WHERE table_schema=current_schema() AND column_name IN ('face_value_usd','remaining_usd','usd_to_balance_rate','covered_usd','daily_budget_usd')`); got != "" {
		t.Errorf("legacy USD columns remain: %s", got)
	}
	// 新约束取代 remaining_usd <= face_value_usd。
	if _, err := pool.Exec(ctx, `UPDATE store_credit_packs SET remaining_balance_units=70.0000000001 WHERE id=$1`, pack1); err == nil ||
		!strings.Contains(err.Error(), "store_credit_packs_remaining_le_face") {
		t.Errorf("remaining above face value must be rejected: %v", err)
	}
	jsonEqual(t, mustText(t, pool, `SELECT value::text FROM settings WHERE key='platform'`), `{"withdraw_min":12}`)

	// 检测预算 ×7，默认值同步；只换算未结束批次的快照。
	if got := mustText(t, pool, `SELECT string_agg(platform||'='||daily_budget_cny::text, ',' ORDER BY platform) FROM detection_policies
		WHERE platform IN ('anthropic','openai') AND auth_type=''`); got != "anthropic=17.5000000000,openai=7.0000000000" {
		t.Errorf("detection budgets: %s", got)
	}
	if got := mustText(t, pool, `SELECT column_default FROM information_schema.columns WHERE table_name='detection_policies' AND column_name='daily_budget_cny'`); got != "7" {
		t.Errorf("detection budget default: %s", got)
	}
	jsonEqual(t, mustText(t, pool, `SELECT execution_snapshot->'policy' FROM probe_runs WHERE id=$1`, queuedRun),
		`{"platform":"anthropic","daily_budget_cny":17.5,"enabled":true}`)
	jsonEqual(t, mustText(t, pool, `SELECT execution_snapshot->'policy' FROM probe_runs WHERE id=$1`, doneRun),
		`{"platform":"anthropic","daily_budget_usd":2.5,"enabled":true}`)

	// 成本额度 ×7，计数类参数与非数值不动。
	jsonEqual(t, mustText(t, pool, `SELECT extra_limits::text FROM vendor_configs WHERE platform='anthropic'`), wantLimits)
	jsonEqual(t, mustText(t, pool, `SELECT extra_limits::text FROM upstream_runtime_params WHERE upstream_id=$1`, upstreamID), wantLimits)
	jsonEqual(t, mustText(t, pool, `SELECT extra_limits::text FROM vendor_configs WHERE platform='gemini'`), `null`)
	jsonEqual(t, mustText(t, pool, `SELECT extra_limits::text FROM vendor_configs WHERE platform='openai'`), `{}`)

	// 买家 Key 限额数值不变，键名改为 *_cny。
	jsonEqual(t, mustText(t, pool, `SELECT limits::text FROM buyer_keys WHERE id=$1`, limitedKey),
		`{"quota_cny":5,"rate_limit_5h_cny":1.25,"ip_whitelist":["10.0.0.1"],"expires_at":"2030-01-01T00:00:00Z"}`)
	jsonEqual(t, mustText(t, pool, `SELECT limits::text FROM buyer_keys WHERE id=$1`, plainKey), `{"ip_whitelist":["10.0.0.2"]}`)
}
