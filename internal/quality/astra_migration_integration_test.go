//go:build integration

package quality

import (
	"context"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/internal/testutil"
	"github.com/DueGin/token-market/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

const astraMigration = "0027_astra_pigeonhole_capability.sql"

// 0033 把限定型号的题目移到降智套件并放宽题目套件约束；内置题定义以内置题同步后的最终形状为准，
// 所以要在同步内置题之前把库升到 0033。
const degradationMigration = "0033_degradation_flow.sql"

func preAstraMigrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	if _, err := db.Migrate(context.Background(), pool, migrationsThrough(t, astraMigration, false)); err != nil {
		t.Fatal(err)
	}
	return pool
}

// migrationsThrough 取到指定迁移为止的脚本；只验证 0027 本身，后续新增迁移不影响这些用例。
func migrationsThrough(t *testing.T, last string, inclusive bool) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	picked := fstest.MapFS{}
	for _, name := range names {
		if name > last || name == last && !inclusive {
			continue
		}
		raw, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		picked[name] = &fstest.MapFile{Data: raw}
	}
	return picked
}

func applyAstraMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	through := migrationsThrough(t, astraMigration, true)
	applied, err := db.Migrate(context.Background(), pool, through)
	if err != nil || !reflect.DeepEqual(applied, []string{astraMigration}) {
		t.Fatalf("升级到 0027 失败：%v %v", applied, err)
	}
	if applied, err := db.Migrate(context.Background(), pool, through); err != nil || len(applied) != 0 {
		t.Fatalf("迁移重复执行不应再改版本：%v %v", applied, err)
	}
}

// applyDegradationMigration 把库继续升到 0033：内置题同步需要该迁移放宽的题目套件约束。
func applyDegradationMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	through := migrationsThrough(t, degradationMigration, true)
	applied, err := db.Migrate(context.Background(), pool, through)
	if err != nil || len(applied) == 0 || applied[len(applied)-1] != degradationMigration {
		t.Fatalf("升级到 0033 失败：%v %v", applied, err)
	}
	if applied, err := db.Migrate(context.Background(), pool, through); err != nil || len(applied) != 0 {
		t.Fatalf("升级到 0033 不可重复执行：%v %v", applied, err)
	}
}

func astraDBItem(t *testing.T, pool *pgxpool.Pool) Item {
	t.Helper()
	it, err := scanItem(pool.QueryRow(context.Background(), "SELECT "+itemColumns+" FROM probe_items WHERE builtin_key='inspection/pigeonhole/astra/1'"))
	if err != nil {
		t.Fatal(err)
	}
	return *it
}

func probeSnapshot(t *testing.T, pool *pgxpool.Pool, query string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(context.Background(), query).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestAstraMigrationKeepsIDHistoryAndMatchesFreshBank(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := preAstraMigrationDB(t)
	for _, it := range defaultItems() {
		if _, err := createLegacyFixtureItem(ctx, pool, it); err != nil {
			t.Fatal(err)
		}
	}
	old, err := createLegacyFixtureItem(ctx, pool, legacyCandyItem(t))
	if err != nil || old.ID != 22 {
		t.Fatalf("旧库夹具须包含原 ID=22：%+v %v", old, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET created_at='2026-10-01',updated_at='2026-10-01' WHERE id=22`); err != nil {
		t.Fatal(err)
	}
	var seller int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('astra-history@test','unused') RETURNING id`).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"admin", "reference"} {
		var upstream, run int64
		if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,is_reference) VALUES($1,'openai','oauth',$2) RETURNING id`, seller, trigger == "reference").Scan(&upstream); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,summary) VALUES($1,'历史糖果配对',$2,1,'gpt-6-astra','done','{"suites":{"iq":{"samples":1}}}') RETURNING id`, upstream, trigger).Scan(&run); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,passed,http_status,raw_excerpt) VALUES($1,22,'reasoning',true,200,'21')`, run); err != nil {
			t.Fatal(err)
		}
	}
	othersQuery := `SELECT jsonb_agg(to_jsonb(i) ORDER BY id)::text FROM probe_items i WHERE id<>22`
	resultsQuery := `SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM probe_results r`
	runsQuery := `SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM probe_runs r`
	others, results, runs := probeSnapshot(t, pool, othersQuery), probeSnapshot(t, pool, resultsQuery), probeSnapshot(t, pool, runsQuery)
	applyAstraMigration(t, pool)
	moved := astraDBItem(t, pool)
	if moved.ID != 22 || moved.Version != old.Version+1 {
		t.Fatalf("迁移没有原地更新 ID/版本：%+v", moved)
	}
	var createdAt, updatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at,updated_at FROM probe_items WHERE id=22`).Scan(&createdAt, &updatedAt); err != nil || !updatedAt.After(createdAt) || createdAt.UTC().Format("2006-01-02") != "2026-10-01" {
		t.Fatalf("迁移应保留创建时间并更新修改时间：%v %v %v", createdAt, updatedAt, err)
	}
	if others != probeSnapshot(t, pool, othersQuery) || results != probeSnapshot(t, pool, resultsQuery) || runs != probeSnapshot(t, pool, runsQuery) {
		t.Fatal("迁移修改了其他题目、历史逐题结果或历史报告摘要")
	}
	// 升到 0033：限定型号的题目迁入降智套件，内置题同步也需要放宽后的套件约束。
	applyDegradationMigration(t, pool)
	moved = astraDBItem(t, pool)
	if moved.Suite != "inspection_degradation" || moved.CheckKey != "D14" || moved.Version != old.Version+2 {
		t.Fatalf("限定型号的糖果题应迁入降智套件：%+v", moved)
	}
	if n, err := SyncInspectionItems(ctx, pool); err != nil || n != len(inspectionBuiltinItems())-1 {
		t.Fatalf("稳定键未阻止重复插入：%d %v", n, err)
	}
	if n, err := SyncInspectionItems(ctx, pool); err != nil || n != 0 {
		t.Fatalf("同步不幂等：%d %v", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_items WHERE category='pigeonhole'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("迁移后糖果题重复：%d %v", count, err)
	}
	stats, err := ListItemStats(ctx, pool, RecordFilter{Platform: "openai", Model: "gpt-6-astra"})
	if err != nil || len(stats) != 1 || stats[0].ItemID != 22 || stats[0].Suite != "inspection_degradation" || stats[0].Asked != 1 || stats[0].Passed != 1 || stats[0].RefAsked != 1 || stats[0].RefPassed != 1 {
		t.Fatalf("迁移丢失历史逐题统计关联：%+v %v", stats, err)
	}
	// 空库先跑全量迁移（匹配零行），再按启动入口只写新内置题。
	_, fresh := testutil.NewMarketDatabase(t)
	if n, err := SeedDefaultItems(ctx, fresh); err != nil || n != len(inspectionBuiltinItems()) {
		t.Fatalf("空库启动写入错误：%d %v", n, err)
	}
	if n, err := SeedDefaultItems(ctx, fresh); err != nil || n != 0 {
		t.Fatalf("空库重复启动插入重复题：%d %v", n, err)
	}
	if err := fresh.QueryRow(ctx, `SELECT count(*) FROM probe_items WHERE category='pigeonhole'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("空库应恰好一道糖果题：%d %v", count, err)
	}
	seeded := astraDBItem(t, fresh)
	if seeded.Version != 1 {
		t.Fatalf("新题初始版本应为 1：%+v", seeded)
	}
	// 业务定义完全一致；ID、版本计数和时间保留各自的创建/迁移历史。
	moved.ID, moved.Version, seeded.ID, seeded.Version = 0, 0, 0, 0
	if !reflect.DeepEqual(moved, seeded) {
		t.Fatalf("迁移后的老库与新库题目配置不一致：旧=%+v 新=%+v", moved, seeded)
	}
	// 迁移后糖果题属于降智流程：只有冻结了该模型的降智流程才选题。
	for _, mode := range []string{"full", "quick"} {
		for _, model := range []string{"gpt-6-astra", "gpt-6-sol"} {
			items, err := selectFlowExecutionItems(ctx, fresh, "openai", model, nil, []string{model}, []string{inspection.FlowDegradation}, "third_party", map[string]string{inspection.FlowDegradation: mode}, defaultInspectionSettings())
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, it := range items {
				if it.BuiltinKey != nil && *it.BuiltinKey == "inspection/pigeonhole/astra/1" {
					found++
				}
			}
			want := 0
			if model == "gpt-6-astra" {
				want = 1
			}
			if found != want {
				t.Fatalf("降智题模式/型号选择错误 %s/%s：%d，期望 %d", mode, model, found, want)
			}
		}
	}
}

func TestAstraMigrationPreservesAdminChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := preAstraMigrationDB(t)
	it := legacyCandyItem(t)
	it.Status, it.Platforms = "retired", []string{"openai"}
	it.Grader = Grader{Type: "exact", Answer: "管理员判分"}
	old, err := createLegacyFixtureItem(ctx, pool, it)
	if err != nil {
		t.Fatal(err)
	}
	// 空字符串和 NULL 均代表未分配 builtin_key。
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET builtin_key='' WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	applyAstraMigration(t, pool)
	// 停用题不属于「启用中的限定型号题」，0033 不改动它；这里只需要放宽后的套件约束。
	applyDegradationMigration(t, pool)
	if _, err := SyncInspectionItems(ctx, pool); err != nil {
		t.Fatal(err)
	}
	moved := astraDBItem(t, pool)
	old.Suite, old.CheckKey, old.BuiltinKey, old.Modes, old.Version = "inspection_capability", "D07", moved.BuiltinKey, []string{"full", "quick"}, old.Version+1
	if !reflect.DeepEqual(moved, *old) {
		t.Fatalf("迁移覆盖了管理员状态、平台、判分或其他字段：%+v，期望 %+v", moved, old)
	}
	items, err := selectFlowExecutionItems(ctx, pool, "openai", "gpt-6-astra", []string{"gpt-6-astra"}, nil, []string{"intelligence"}, "third_party", map[string]string{"intelligence": "quick"}, defaultInspectionSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range items {
		if selected.ID == moved.ID {
			t.Fatal("同步复活了管理员停用的题")
		}
	}
}

func TestAstraMigrationDoesNotGuessAdminItemIdentity(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"改题面", "其他模型", "其他类别", "显式质检项", "已有稳定键", "其他套件", "多行匹配", "目标键已占用"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			pool := preAstraMigrationDB(t)
			it := legacyCandyItem(t)
			switch name {
			case "改题面":
				it.Prompt += " 管理员改写"
			case "其他模型":
				it.Models = []string{"gpt-6-sol"}
			case "其他类别":
				it.Category = "管理员题"
			case "显式质检项":
				it.CheckKey = "D08"
			case "已有稳定键":
				key := "admin/candy"
				it.BuiltinKey = &key
			case "其他套件":
				it.Suite = "iq"
			}
			old, err := createLegacyFixtureItem(ctx, pool, it)
			if err != nil {
				t.Fatal(err)
			}
			if name == "多行匹配" || name == "目标键已占用" {
				second := it
				if name == "目标键已占用" {
					// 只要求稳定键已被占用；0026 库还不接受降智套件，改用通用测智套件占位。
					second = astraCandyItem()
					second.Suite, second.CheckKey = "inspection_context", "D09"
				}
				if _, err := createLegacyFixtureItem(ctx, pool, second); err != nil {
					t.Fatal(err)
				}
			}
			query := `SELECT jsonb_agg(to_jsonb(i) ORDER BY id)::text FROM probe_items i`
			before := probeSnapshot(t, pool, query)
			applyAstraMigration(t, pool)
			if before != probeSnapshot(t, pool, query) {
				t.Fatal("迁移误改了未匹配或归属不唯一的管理员题")
			}
			// 0027 不动这些管理员题；把库升到 0033 才能插入降智套件的内置题。
			applyDegradationMigration(t, pool)
			if _, err := SyncInspectionItems(ctx, pool); err != nil {
				t.Fatal(err)
			}
			added := astraDBItem(t, pool)
			if added.ID == old.ID || added.Prompt != legacyCandyItem(t).Prompt {
				t.Fatal("未匹配库应保留管理员题并补标准内置题")
			}
		})
	}
}

// 迁移测试直接构造旧库记录；现行管理接口已经拒绝创建旧套件。
func createLegacyFixtureItem(ctx context.Context, pool *pgxpool.Pool, it Item) (*Item, error) {
	if it.Platforms == nil {
		it.Platforms = []string{}
	}
	if it.Models == nil {
		it.Models = []string{}
	}
	if it.Modes == nil {
		it.Modes = []string{}
	}
	if len(it.Generator) == 0 {
		it.Generator = []byte(`{}`)
	}
	if it.Status == "" {
		it.Status = "active"
	}
	if it.Weight == 0 {
		it.Weight = 1
	}
	return scanItem(pool.QueryRow(ctx, `INSERT INTO probe_items(suite,category,prompt,grader,platforms,models,max_tokens,reasoning_effort,irt_difficulty,irt_discrimination,status,check_key,request,generator,modes,builtin_key,weight) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING `+itemColumns,
		it.Suite, it.Category, it.Prompt, mustJSON(it.Grader), it.Platforms, it.Models, it.MaxTokens, it.ReasoningEffort, it.IRTDifficulty, it.IRTDiscrimination, it.Status, it.CheckKey, mustJSON(it.Request), it.Generator, it.Modes, it.BuiltinKey, it.Weight))
}
