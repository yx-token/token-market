//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInspectionLongRetryIsSavedWithoutWaiting(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	}))
	defer srv.Close()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t), Prober: &Prober{GatewayURL: srv.URL}}
	ctx := context.Background()
	it, err := CreateItem(ctx, pool, Item{Suite: "inspection_capability", CheckKey: "D07", Category: "限流回归", Prompt: "只答1", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	p := inspectionTestPolicy(run)
	st := defaultInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, []Item{*it}, st)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	rows, _, err := e.executePlan(ctx, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", plan, p, st, time.Second)
	if err != nil || calls.Load() != 1 || time.Since(started) > 5*time.Second || rows[0].Target.ErrorClass != "rate_limited" {
		t.Fatalf("长等待仍重试：%v %+v", err, rows)
	}
	cached, _, state, found, err := e.loadInstanceResult(ctx, run.id, plan.Instances[0], 0)
	if err != nil || !found || !state.Complete || cached.RetryAfter != time.Hour {
		t.Fatalf("等待信息未持久化：%+v %+v %v", cached, state, err)
	}
}

func TestInspectionLegacyActualBudgetOnlyWarns(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10000}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"1\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10000}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t), Prober: &Prober{GatewayURL: srv.URL}}
	ctx := context.Background()
	it, err := CreateItem(ctx, pool, Item{Suite: "inspection_capability", CheckKey: "D07", Category: "预算回归", Prompt: "只答1", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	p := inspectionTestPolicy(run)
	st := defaultInspectionSettings()
	st.TokenBudgets["full"] = TokenBudget{Input: 1, Output: 1}
	run.mode = "legacy"
	plan, err := e.executionPlan(ctx, run, p, []Item{*it}, st)
	if err != nil {
		t.Fatal(err)
	}
	rows, warnings, err := e.executePlan(ctx, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", plan, p, st, time.Second)
	if err != nil || len(rows) != 1 || !rows[0].TargetPass || !strings.Contains(strings.Join(warnings, ""), "预算超出") {
		t.Fatalf("旧题超支被中止：%v %+v %v", err, rows, warnings)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT step_state FROM probe_results WHERE run_id=$1`, run.id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state resumedState
	if err := json.Unmarshal(raw, &state); err != nil || state.InputCharged != 10000 || state.OutputCharged != 10000 {
		t.Fatalf("实际超支未记账：%s %v", raw, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM probe_results WHERE run_id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	plan.Mode = "full"
	if _, _, err := e.executePlan(ctx, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", plan, p, st, time.Second); err == nil {
		t.Fatal("完整模式仍须硬预算中止")
	}
}
