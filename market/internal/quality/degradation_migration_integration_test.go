//go:build integration

package quality

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
)

// 0033 为存量策略补降智流程、新增冻结列，并把限定厂商/型号的旧测智题迁到独立的降智套件。
func TestDegradationFlowMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, pool := testutil.NewDatabase(t)
	const migration = "0033_degradation_flow.sql"
	if _, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, false)); err != nil {
		t.Fatal(err)
	}
	// 存量策略：测智配了两个模型且带期望通过率，管理员尚未配置降智流程。
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,scheduled_enabled=true,interval_minutes=90,
		models=ARRAY['gpt-6-astra','gpt-6.1-sol'],
		flows = jsonb_set(jsonb_set(flows, '{intelligence,interval_minutes}', '90'::jsonb),
			'{intelligence,models}', '[{"model":"gpt-6-astra","expected_pass_rate":0.85},{"model":"gpt-6.1-sol","expected_pass_rate":null}]'::jsonb)
		WHERE platform='openai' AND auth_type=''`); err != nil {
		t.Fatal(err)
	}
	var seller, upstream int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('degradation-migration@test','unused') RETURNING id`).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type) VALUES($1,'openai','oauth') RETURNING id`, seller).Scan(&upstream); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,next_intelligence_at,last_intelligence_at)
		VALUES($1,'2026-10-12 01:00:00Z','2026-10-12 00:00:00Z')`, upstream); err != nil {
		t.Fatal(err)
	}
	type oldItem struct {
		name    string
		suite   string
		status  string
		models  []string
		platfrm []string
		id      int64
	}
	items := []oldItem{
		// 限定厂商的降智题（线上 ID=22 那一类）：迁移到 inspection_degradation / D14。
		{name: "限定厂商", suite: "inspection_capability", status: "active", platfrm: []string{"openai"}},
		{name: "限定型号", suite: "inspection_capability", status: "active", models: []string{"gpt-6-astra"}},
		// 通用的测智题保持不动。
		{name: "通用测智", suite: "inspection_capability", status: "active"},
		{name: "通用上下文", suite: "inspection_context", status: "active"},
		// 非启用状态不属于「启用中题目」，保持不动。
		{name: "停用限定型号", suite: "inspection_capability", status: "retired", models: []string{"gpt-6-astra"}},
	}
	for i := range items {
		models, platforms := items[i].models, items[i].platfrm
		if models == nil {
			models = []string{}
		}
		if platforms == nil {
			platforms = []string{}
		}
		if err := pool.QueryRow(ctx, `INSERT INTO probe_items(suite,category,prompt,grader,models,platforms,status,version,max_tokens)
			VALUES($1,'迁移用例','题面','{"type":"exact","answer":"12"}',$2,$3,$4,3,128) RETURNING id`,
			items[i].suite, models, platforms, items[i].status).Scan(&items[i].id); err != nil {
			t.Fatal(err)
		}
	}
	resultsQuery := `SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM probe_results r`
	var runID int64
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,summary,finished_at)
		VALUES($1,'迁移前运行','admin',1,'gpt-6-astra','done','{"verdict":"normal"}',now()) RETURNING id`, upstream).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,passed) VALUES($1,$2,'inspection_capability',true)`, runID, items[0].id); err != nil {
		t.Fatal(err)
	}
	history := probeSnapshot(t, pool, resultsQuery)
	if applied, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, true)); err != nil || !reflect.DeepEqual(applied, []string{migration}) {
		t.Fatalf("降智流程迁移失败：%v %v", applied, err)
	}
	if history != probeSnapshot(t, pool, resultsQuery) {
		t.Fatal("迁移修改了旧逐题结果")
	}

	// 降智流程继承测智的模型与周期，但定时检测保持关闭。
	p, err := scanPolicy(pool.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform='openai' AND auth_type=''"))
	if err != nil {
		t.Fatal(err)
	}
	got := p.Flows.Degradation
	if !got.Enabled || got.ScheduledEnabled || got.IntervalMinutes != 90 || !reflect.DeepEqual(got.Models, p.Flows.Intelligence.Models) ||
		!reflect.DeepEqual(got.Triggers, p.Flows.Intelligence.Triggers) || !reflect.DeepEqual(got.FullTriggers, p.Flows.Intelligence.FullTriggers) {
		t.Fatalf("降智流程未按测智继承：%+v", got)
	}
	if len(got.Models) != 2 || got.Models[0].Model != "gpt-6-astra" {
		t.Fatalf("降智模型列表复制错误：%+v", got.Models)
	}

	for i, old := range items {
		moved, err := scanItem(pool.QueryRow(ctx, "SELECT "+itemColumns+" FROM probe_items WHERE id=$1", old.id))
		if err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0, 1:
			if moved.Suite != "inspection_degradation" || moved.CheckKey != "D14" || moved.Status != "active" || moved.Version != 4 {
				t.Fatalf("限定厂商或型号的旧题应原地迁入降智套件：%+v", moved)
			}
		case 2, 3, 4:
			if moved.Suite != old.suite || moved.CheckKey != "" || moved.Status != old.status || moved.Version != 3 {
				t.Fatalf("通用测智题或未启用题目不应改动：%+v", moved)
			}
		}
		if moved.Prompt != "题面" || moved.Grader.Answer != "12" || moved.MaxTokens != 128 {
			t.Fatalf("迁移覆盖了管理员题目配置：%+v", moved)
		}
	}

	// 新增字段可用：降智模型集合与按流程的 next / last 时间。
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET degradation_models=ARRAY['gpt-6-astra'] WHERE batch_id='迁移前运行'`); err != nil {
		t.Fatal(err)
	}
	var models []string
	if err := pool.QueryRow(ctx, `SELECT degradation_models FROM probe_runs WHERE batch_id='迁移前运行'`).Scan(&models); err != nil || !reflect.DeepEqual(models, []string{"gpt-6-astra"}) {
		t.Fatalf("降智模型冻结列不可用：%v %v", models, err)
	}
	var nextDegradation, lastDegradation *time.Time
	var nextIntelligence time.Time
	if err := pool.QueryRow(ctx, `SELECT next_degradation_at,last_degradation_at,next_intelligence_at FROM upstream_quality_state WHERE upstream_id=$1`, upstream).
		Scan(&nextDegradation, &lastDegradation, &nextIntelligence); err != nil {
		t.Fatal(err)
	}
	if nextDegradation != nil || lastDegradation != nil || !nextIntelligence.Equal(time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("降智调度字段初值错误：%v %v %v", nextDegradation, lastDegradation, nextIntelligence)
	}

	// 幂等：管理员改过的降智配置不能被第二次执行覆盖。
	if _, err := pool.Exec(ctx, `UPDATE detection_policies
		SET flows = jsonb_set(jsonb_set(flows, '{degradation,models}', '[{"model":"人工指定","expected_pass_rate":0.8}]'::jsonb),
			'{degradation,scheduled_enabled}', 'true'::jsonb)
		WHERE platform='openai' AND auth_type=''`); err != nil {
		t.Fatal(err)
	}
	if applied, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, true)); err != nil || len(applied) != 0 {
		t.Fatalf("重复运行迁移应无操作：%v %v", applied, err)
	}
	updated, err := scanPolicy(pool.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform='openai' AND auth_type=''"))
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Flows.Degradation.Models) != 1 || updated.Flows.Degradation.Models[0].Model != "人工指定" || !updated.Flows.Degradation.ScheduledEnabled {
		t.Fatalf("迁移覆盖了管理员配置的降智流程：%+v", updated.Flows.Degradation)
	}
}
