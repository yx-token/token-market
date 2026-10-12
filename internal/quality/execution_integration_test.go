//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func inspectionDB(t *testing.T) (*pgxpool.Pool, runRow, runRow) {
	t.Helper()
	ctx := context.Background()
	_, pool := testutil.NewMarketDatabase(t)
	var seller, u1, u2, r1, r2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES ('inspection@test','unused') RETURNING id`).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	for _, id := range []*int64{&u1, &u2} {
		if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type) VALUES($1,'anthropic','oauth') RETURNING id`, seller).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode) VALUES($1,'inspection-batch','admin',1,'claude-sonnet-4-5','full') RETURNING id`, u1).Scan(&r1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode) VALUES($1,'inspection-batch','reference',1,'claude-sonnet-4-5','full') RETURNING id`, u2).Scan(&r2); err != nil {
		t.Fatal(err)
	}
	return pool, runRow{id: r1, upstreamID: u1, trigger: "admin", model: "claude-sonnet-4-5", mode: "full", batch: "inspection-batch"}, runRow{id: r2, upstreamID: u2, trigger: "reference", model: "claude-sonnet-4-5", mode: "full", batch: "inspection-batch"}
}

func TestInspectionSnapshotPairAndResume(t *testing.T) {
	t.Parallel()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	var items []Item
	for _, prompt := range []string{"第一题 {{HAYSTACK:100}}", "第二题"} {
		it, err := CreateItem(ctx, pool, Item{Suite: "iq", Category: "安全测试", Prompt: prompt, MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, *it)
	}
	p := &Policy{Version: 1, ItemParallelism: 1, Suites: []string{"iq"}, Thresholds: th}
	st := defaultInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, items, st)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := map[string][]string{}
	hang := true
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("x-api-key")
		mu.Lock()
		requests[key] = append(requests[key], string(raw))
		blocked := hang && strings.Contains(string(raw), "第二题")
		mu.Unlock()
		if blocked {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"claude-sonnet-4-5","content":[{"type":"text","text":"1"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":1}}`)
	}))
	defer server.Close()
	e.Prober = &Prober{GatewayURL: server.URL, Client: server.Client()}
	u := &upstreamRow{id: run.upstreamID, platform: "anthropic"}
	ru := &upstreamRow{id: ref.upstreamID, platform: "anthropic"}
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, _, err := e.executePlan(cctx, run, &ref, u, ru, "target", "reference", plan, p, st, time.Second)
		done <- err
	}()
	<-started
	<-started
	cancel()
	if err := <-done; err == nil {
		t.Fatal("取消未中断批次")
	}
	var complete int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_results WHERE item_id=$1 AND step_state->>'complete'='true'`, items[0].ID).Scan(&complete); err != nil || complete != 2 {
		t.Fatalf("已完成双侧题未保留：%d %v", complete, err)
	}
	if err := e.requeueBatch(ctx, run.batch); err != nil {
		t.Fatal(err)
	}
	// 改题后读原快照，不重新生成；基线 run 不重复存长题面。
	items[0].Prompt = "后续配置的新题"
	reloaded, err := e.executionPlan(ctx, run, p, items, st)
	if err != nil || reloaded.Instances[0].Hash != plan.Instances[0].Hash {
		t.Fatalf("快照被改题覆盖：%v", err)
	}
	var refRaw []byte
	if err := pool.QueryRow(ctx, `SELECT execution_snapshot FROM probe_runs WHERE id=$1`, ref.id).Scan(&refRaw); err != nil || string(refRaw) != "{}" {
		t.Fatal("基线重复保存快照")
	}
	mu.Lock()
	hang = false
	mu.Unlock()
	out, warnings, err := e.executePlan(ctx, run, &ref, u, ru, "target", "reference", reloaded, p, st, time.Second)
	if err != nil || len(out) != 2 || len(warnings) != 1 {
		t.Fatalf("续跑失败：%v %v", warnings, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests["target"]) != 3 || len(requests["reference"]) != 3 || requests["target"][0] != requests["reference"][0] {
		t.Fatalf("完成题重复发送或双侧题面不同：%v", requests)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_results`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("结果重复：%d %v", n, err)
	}
	if a := Assess(out, th, true); a.AvailableOK != 2 || a.Samples != 2 {
		t.Fatalf("续跑丢失观测：%+v", a)
	}
}

func TestInspectionMigrationLegacyCompatibility(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	var itID int64
	if err := pool.QueryRow(ctx, `INSERT INTO probe_items(suite,category,prompt,grader) VALUES('iq','旧程序','只答1','{"type":"exact","answer":"1"}') RETURNING id`).Scan(&itID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,passed) VALUES($1,$2,'iq',true),($1,$2,'iq',true)`, run.id, itID); err != nil {
		t.Fatal("旧程序重复结果被新索引拒绝", err)
	}
	for _, column := range []string{"modes", "flags", "full_triggers"} {
		var typeName string
		if err := pool.QueryRow(ctx, `SELECT udt_name FROM information_schema.columns WHERE column_name=$1`, column).Scan(&typeName); err != nil || typeName != "_text" {
			t.Fatalf("%s 不是text[]：%s %v", column, typeName, err)
		}
	}
	var defaults []byte
	var modes []string
	var weight float64
	if err := pool.QueryRow(ctx, `SELECT request,modes,weight FROM probe_items WHERE id=$1`, itID).Scan(&defaults, &modes, &weight); err != nil || string(defaults) != "{}" || len(modes) != 0 || weight != 1 {
		t.Fatal("旧插入的安全默认值不匹配", err)
	}
	var oldModels []string
	if err := pool.QueryRow(ctx, `SELECT models FROM detection_policies WHERE platform='anthropic'`).Scan(&oldModels); err != nil || len(oldModels) != 1 || oldModels[0] != "claude-sonnet-4-5" {
		t.Fatal("迁移改动默认模型", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('inspection','{"limits":{"account_parallelism":1}}')`); err != nil {
		t.Fatal(err)
	}
	st, err := loadInspectionSettings(ctx, pool)
	if err != nil || st.Limits.Global != 32 || st.Timeouts.Full != 75 || st.Limits.Account != 1 {
		t.Fatal("部分设置覆盖破坏默认值", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settings SET value='{"token_budgets":{"full":{"input_tokens_per_side":1000000}}}' WHERE key='inspection'`); err != nil {
		t.Fatal(err)
	}
	partial, err := loadInspectionSettings(ctx, pool)
	if err != nil || partial.TokenBudgets["full"].Input != 1000000 || partial.TokenBudgets["full"].Output != 640000 || partial.TokenBudgets["quick"].Output != 160000 {
		t.Fatal("单个预算字段覆盖丢失默认值", err)
	}
	_, err = UpsertPolicy(ctx, pool, "anthropic", "", PolicyInput{ItemParallelism: func() *int { x := 2; return &x }()}, 1)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInspectionNewOrphanKeepsDeadline(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	deadline := time.Now().Add(time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET status='running',started_at=now()-interval '6 minutes',deadline_at=$2,execution_snapshot=$3 WHERE id=$1`, run.id, deadline, mustJSON(map[string]any{"schema_version": 1})); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool}
	if err := e.failOrphaned(ctx, probeLimits{batch: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var status string
	var saved time.Time
	var raw json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT status,deadline_at,execution_snapshot FROM probe_runs WHERE id=$1`, run.id).Scan(&status, &saved, &raw); err != nil || status != "queued" || saved.Sub(deadline) > time.Millisecond || !strings.Contains(string(raw), "schema_version") {
		t.Fatalf("重启丢失截止与快照：%s %v", status, err)
	}
}

func TestInspectionMultistepRealToolsAndResume(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	g := Grader{Type: "tool_calls", GraderOptions: GraderOptions{Expect: []ExpectedToolCall{{Name: "schedule", Args: map[string]any{"city": "杭州"}}}}}
	final := Grader{Type: "exact", Answer: "2026-10-12"}
	it, err := CreateItem(ctx, pool, Item{Suite: "iq", Category: "本地工具续跑", Prompt: "查班次", Grader: g, MaxTokens: 64, Request: ProbeRequest{Tools: []ProbeTool{{Name: "schedule", InputSchema: map[string]any{"type": "object"}}}, Steps: []ProbeStep{{Request: &ProbeRequest{Prompt: "只输出日期"}, Expected: &final, UsePrevious: true, ToolResults: map[string]any{"schedule": map[string]any{"date": "2026-10-12"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Version: 1, ItemParallelism: 1, Suites: []string{"iq"}, Thresholds: th}
	st := defaultInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, []Item{*it}, st)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := 0
	hang := true
	waiting := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls++
		blocked := hang && strings.Contains(string(raw), "tool_result")
		mu.Unlock()
		if blocked {
			waiting <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(string(raw), "tool_result") {
			_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"真实调用编号","name":"schedule","input":{"city":"杭州"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":10}}`)
			return
		}
		if !strings.Contains(string(raw), "真实调用编号") || !strings.Contains(string(raw), "2026-10-12") || !strings.Contains(string(raw), "tools") {
			t.Error("后续请求未沿用真实调用与工具定义")
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"2026-10-12"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":5}}`)
	}))
	defer server.Close()
	e.Prober = &Prober{GatewayURL: server.URL, Client: server.Client()}
	u := &upstreamRow{id: run.upstreamID, platform: "anthropic"}
	budget, err := e.budgetForRun(ctx, run.id, plan.Budget)
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, _, err := e.executeInstance(cctx, run, u, "key", plan.Instances[0], p, st, budget, time.Second, false)
		done <- err
	}()
	<-waiting
	cancel()
	if err := <-done; err == nil {
		t.Fatal("多步取消未生效")
	}
	mu.Lock()
	hang = false
	mu.Unlock()
	budget, err = e.budgetForRun(ctx, run.id, plan.Budget)
	if err != nil {
		t.Fatal(err)
	}
	last, pass, err := e.executeInstance(ctx, run, u, "key", plan.Instances[0], p, st, budget, time.Second, false)
	if err != nil || !pass || last.Text != "2026-10-12" {
		t.Fatalf("多步恢复失败：%v %v %+v", pass, err, last)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("已完成工具步骤被重发：%d", calls)
	}
}
