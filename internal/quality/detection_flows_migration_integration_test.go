//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/internal/testutil"
)

func TestDetectionFlowsMigrationPreservesLegacyHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, pool := testutil.NewDatabase(t)
	const migration = "0030_detection_flows.sql"
	if _, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,scheduled_enabled=true,interval_minutes=90,
		models=ARRAY['gpt-6-astra','gpt-6.1-sol'],full_triggers=ARRAY['appeal'],
		third_party='[{"provider":"bazaarlink","triggers":["admin","scheduled"]},{"provider":"other","triggers":["admin","appeal"]}]'
		WHERE platform='openai' AND auth_type=''`); err != nil {
		t.Fatal(err)
	}
	var seller, upstream, run int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('flows-migration@test','unused') RETURNING id`).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type) VALUES($1,'openai','oauth') RETURNING id`, seller).Scan(&upstream); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,next_scheduled_at,last_run_at)
		VALUES($1,'2026-10-12 01:00:00Z','2026-10-11 01:00:00Z')`, upstream); err != nil {
		t.Fatal(err)
	}
	oldSummary := json.RawMessage(`{"suites":{"iq":{"samples":1}},"旧报告":"保留"}`)
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,summary)
		VALUES($1,'旧运行','admin',1,'gpt-6-astra','done',$2) RETURNING id`, upstream, oldSummary).Scan(&run); err != nil {
		t.Fatal(err)
	}
	type oldItem struct {
		suite, status, model string
		id                   int64
	}
	items := []oldItem{
		{suite: "iq", status: "active", model: "gpt-6-astra"},
		{suite: "reasoning", status: "retired", model: "gpt-6.1-sol"},
		{suite: "iq", status: "active"},
		{suite: "reasoning", status: "draft"},
		{suite: "long_context", status: "active", model: "gpt-6-astra"},
		{suite: "inspection_capability", status: "active", model: "gpt-6-astra"},
	}
	for i := range items {
		models := []string{}
		if items[i].model != "" {
			models = append(models, items[i].model)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO probe_items(suite,category,prompt,grader,models,status,version,platforms,max_tokens)
			VALUES($1,'管理员旧题','管理员题面','{"type":"exact","answer":"管理员答案"}',$2,$3,4,ARRAY['openai'],321) RETURNING id`,
			items[i].suite, models, items[i].status).Scan(&items[i].id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,passed,http_status,raw_excerpt)
		VALUES($1,$2,'iq',true,200,'历史回答')`, run, items[0].id); err != nil {
		t.Fatal(err)
	}
	resultsQuery := `SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM probe_results r`
	history := probeSnapshot(t, pool, resultsQuery)
	if applied, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, true)); err != nil || !reflect.DeepEqual(applied, []string{migration}) {
		t.Fatalf("流程迁移失败：%v %v", applied, err)
	}
	if history != probeSnapshot(t, pool, resultsQuery) {
		t.Fatal("迁移修改了旧逐题结果")
	}
	p, err := scanPolicy(pool.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform='openai' AND auth_type=''"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Flows.Intelligence.Enabled || !p.Flows.Intelligence.ScheduledEnabled || p.Flows.Intelligence.IntervalMinutes != 90 ||
		!reflect.DeepEqual(p.Flows.Intelligence.FullTriggers, []string{"appeal"}) || len(p.Flows.Intelligence.Models) != 2 ||
		p.Flows.Intelligence.Models[0].Model != "gpt-6-astra" || p.Flows.Intelligence.Models[1].Model != "gpt-6.1-sol" ||
		p.Flows.Intelligence.Models[0].ExpectedPassRate != nil {
		t.Fatalf("旧测智配置没有迁入 flows：%+v", p.Flows.Intelligence)
	}
	// 身份核验上线时先关闭，等 https 的 Edge 地址与检测站就绪后由管理员开启；来源与触发照常迁入。
	if p.Flows.Identity.Enabled || p.Flows.Identity.ScheduledEnabled || p.Flows.Identity.Source != "third_party" ||
		!reflect.DeepEqual(p.Flows.Identity.Triggers, []string{"admin", "appeal", "scheduled"}) {
		t.Fatalf("身份来源和检测站触发并集迁移错误：%+v", p.Flows.Identity)
	}
	// 未开检测的厂商：测智流程仍启用，保证上架检测会测智；定时与身份核验保持关闭。
	disabled, err := scanPolicy(pool.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform='anthropic' AND auth_type=''"))
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || !disabled.Flows.Intelligence.Enabled || disabled.Flows.Intelligence.ScheduledEnabled || disabled.Flows.Identity.Enabled {
		t.Fatalf("未开检测的厂商迁移后上架须仍测智：总开关=%t %+v %+v", disabled.Enabled, disabled.Flows.Intelligence.FlowPolicy, disabled.Flows.Identity.FlowPolicy)
	}
	for i, old := range items {
		moved, err := scanItem(pool.QueryRow(ctx, "SELECT "+itemColumns+" FROM probe_items WHERE id=$1", old.id))
		if err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0, 1:
			if moved.Suite != "inspection_capability" || moved.CheckKey != "D07" || moved.Status != old.status || moved.Version != 5 ||
				!reflect.DeepEqual(moved.Modes, []string{"full", "quick"}) {
				t.Fatalf("型号旧题应保留状态并原地迁入 D07：%+v", moved)
			}
		case 2, 3, 4:
			if moved.Suite != old.suite || moved.Status != "retired" || moved.Version != 5 {
				t.Fatalf("其余旧题没有下线：%+v", moved)
			}
		case 5:
			if moved.Status != "active" || moved.Version != 4 {
				t.Fatalf("迁移误改新题：%+v", moved)
			}
		}
		if moved.Prompt != "管理员题面" || moved.Grader.Answer != "管理员答案" || moved.MaxTokens != 321 || !reflect.DeepEqual(moved.Platforms, []string{"openai"}) {
			t.Fatalf("迁移覆盖了管理员题目配置：%+v", moved)
		}
	}
	var nextIntelligence, nextIdentity time.Time
	var lastIntelligence, lastIdentity, reset *time.Time
	var suspected, flows, models []string
	var identitySource *string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT next_intelligence_at,next_identity_at,last_intelligence_at,last_identity_at,
		suspected_flows,adaptive_attempts,deductions_reset_at FROM upstream_quality_state WHERE upstream_id=$1`, upstream).
		Scan(&nextIntelligence, &nextIdentity, &lastIntelligence, &lastIdentity, &suspected, &attempts, &reset); err != nil ||
		!nextIntelligence.Equal(time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)) || !nextIdentity.Equal(nextIntelligence) ||
		lastIntelligence != nil || lastIdentity != nil || reset != nil || len(suspected) != 0 || attempts != 0 {
		t.Fatalf("按流程调度字段回填错误：%v %v %v %v %v %d %v %v", nextIntelligence, nextIdentity, lastIntelligence, lastIdentity, suspected, attempts, reset, err)
	}
	var summary []byte
	if err := pool.QueryRow(ctx, `SELECT flows,identity_source,models,summary FROM probe_runs WHERE id=$1`, run).
		Scan(&flows, &identitySource, &models, &summary); err != nil || len(flows) != 0 || len(models) != 0 || identitySource != nil {
		t.Fatalf("旧运行不应补推断出的流程：%v %v %v %v", flows, identitySource, models, err)
	}
	var oldData, newData any
	if err := json.Unmarshal(oldSummary, &oldData); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(summary, &newData); err != nil || !reflect.DeepEqual(oldData, newData) {
		t.Fatalf("迁移修改了旧报告摘要：%s %v", summary, err)
	}
	var whitelist []string
	if err := pool.QueryRow(ctx, `SELECT model_whitelist FROM upstreams WHERE id=$1`, upstream).Scan(&whitelist); err != nil || whitelist != nil {
		t.Fatalf("存量上游应保留未声明白名单：%v %v", whitelist, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO third_party_checks(run_id,upstream_id,provider,model,format,config,deadline)
		VALUES($1,$2,'bazaarlink','gpt-6-astra','openai','{}',now()+interval '1 hour')`, run, upstream); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO third_party_checks(run_id,upstream_id,provider,model,format,config,deadline)
		VALUES($1,$2,'bazaarlink','gpt-6-astra','openai','{}',now()+interval '1 hour')`, run, upstream); err == nil {
		t.Fatal("同次运行不应重复提交同一检测站")
	}
	if applied, err := db.Migrate(ctx, pool, migrationsThrough(t, migration, true)); err != nil || len(applied) != 0 {
		t.Fatalf("重复运行迁移应无操作：%v %v", applied, err)
	}
}

func TestDetectionFlowsPolicyRoundTripAndProviderValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, pool := testutil.NewMarketDatabase(t)
	flows := policyFlowsFixture()
	rate := 0.9
	flows.Intelligence.Models[0].ExpectedPassRate = &rate
	sites := []ThirdPartyCheck{{Provider: "bazaarlink"}}
	input := PolicyInput{Flows: &flows, ThirdParty: &sites}
	if _, err := UpsertPolicy(ctx, pool, "openai", "", input, 1); err == nil {
		t.Fatal("启用第三方身份流程必须至少有一站已启用")
	}
	if _, err := pool.Exec(ctx, `UPDATE detection_providers SET enabled=true WHERE name='bazaarlink'`); err != nil {
		t.Fatal(err)
	}
	p, err := UpsertPolicy(ctx, pool, "openai", "", input, 1)
	if err != nil || !reflect.DeepEqual(p.Flows, flows) || len(p.ThirdParty) != 1 || len(p.ThirdParty[0].Triggers) != 0 {
		t.Fatalf("流程写入与扫描不一致：%+v %v", p, err)
	}
	updated := flows
	updated.Identity.Source = "self"
	updated.Intelligence.Models = []IntelligenceModel{{Model: "gpt-6.1-sol"}}
	empty := []ThirdPartyCheck{}
	next, err := UpsertPolicy(ctx, pool, "openai", "", PolicyInput{Flows: &updated, ThirdParty: &empty}, 1)
	if err != nil || !reflect.DeepEqual(next.Flows, updated) || next.Version != p.Version+1 {
		t.Fatalf("流程修改未写入或未推进版本：%+v %v", next, err)
	}
	policies, err := ListPolicies(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.Platform == "openai" && policy.AuthType == "" {
			if !reflect.DeepEqual(policy.Flows, updated) {
				t.Fatalf("策略列表遗漏 flows：%+v", policy)
			}
			return
		}
	}
	t.Fatal("策略列表没有更新后的厂商策略")
}
