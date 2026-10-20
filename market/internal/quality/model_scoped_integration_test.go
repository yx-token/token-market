//go:build integration

package quality

import (
	"context"
	"reflect"
	"testing"
)

func TestInspectionModelScopedSelectionAndSnapshot(t *testing.T) {
	t.Parallel()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	scoped, err := CreateItem(ctx, pool, scopedIntelligenceItem())
	if err != nil {
		t.Fatal(err)
	}
	unscoped := scopedIntelligenceItem()
	unscoped.Models, unscoped.Prompt = nil, "只输出 1。"
	unscoped.Grader = Grader{Type: "exact", Answer: "1"}
	created, err := CreateItem(ctx, pool, unscoped)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol"} {
		items, err := selectFlowExecutionItems(ctx, pool, "openai", model, []string{model}, nil, []string{"intelligence"}, "third_party", map[string]string{"intelligence": "full"}, defaultInspectionSettings())
		want := 1
		if model == "gpt-6-astra" {
			want = 2
		}
		if err != nil || len(items) != want {
			t.Fatalf("型号限定选题错误：%s %d %v", model, len(items), err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='retired' WHERE id=$1`, scoped.ID); err != nil {
		t.Fatal(err)
	}
	items, err := selectFlowExecutionItems(ctx, pool, "openai", "gpt-6-astra", []string{"gpt-6-astra"}, nil, []string{"intelligence"}, "third_party", map[string]string{"intelligence": "full"}, defaultInspectionSettings())
	if err != nil || len(items) != 1 {
		t.Fatal("下线题不应执行", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='active' WHERE id=$1`, scoped.ID); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	p := &Policy{Version: 1, Thresholds: th}
	run.model, run.models, run.flows = "gpt-6-astra", []string{"gpt-6-astra", "gpt-6-sol"}, []string{"intelligence"}
	// 排队已经冻结模式，生成计划必须保留它并替换为可续跑快照。
	frozen := executionSnapshot{Policy: p, FlowModes: map[string]string{"intelligence": "full"}, Models: run.models, ModelWhitelist: run.models}
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=$2 WHERE id=$1`, run.id, mustJSON(frozen)); err != nil {
		t.Fatal(err)
	}
	st := calibratedInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, []Item{*scoped, *created}, st)
	if err != nil || len(plan.Instances) != 3 {
		t.Fatalf("按模型展开计划错误：%+v %v", plan, err)
	}
	if plan.Budget.Input != 2*st.TokenBudgets["full"].Input || plan.Budget.Output != 2*st.TokenBudgets["full"].Output || plan.FlowModes["intelligence"] != "full" {
		t.Fatalf("模型预算未放大或流程模式丢失：%+v", plan)
	}
	seen := map[string]bool{}
	for _, instance := range plan.Instances {
		if instance.RequestedModel != instance.Steps[0].Request.Model || instance.Hash == "" || seen[instance.Hash] {
			t.Fatalf("请求型号或冻结哈希错误：%+v", instance)
		}
		seen[instance.Hash] = true
		result := ProbeResult{HTTPStatus: 200, Text: "12", Flags: []string{"stop_normal"}}
		for _, runID := range []int64{run.id, ref.id} {
			if err := e.saveInstanceResult(ctx, runID, instance, 0, result, true, resumedState{Complete: true, Text: "12"}); err != nil {
				t.Fatal(err)
			}
			got, passed, _, found, err := e.loadInstanceResult(ctx, runID, instance, 0)
			if err != nil || !found || got.Text != "12" || !passed {
				t.Fatalf("按模型结果加载串题：%+v %v %v %v", got, passed, found, err)
			}
		}
	}
	reloaded, err := e.executionPlan(ctx, run, p, nil, defaultInspectionSettings())
	if err != nil || !reflect.DeepEqual(mustJSON(plan), mustJSON(reloaded)) {
		t.Fatalf("续跑改变了按模型执行计划：%v", err)
	}
}

func TestFlowSelectionAllowsPureThirdPartyAndIndependentRecovery(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	if _, err := SyncInspectionItems(ctx, pool); err != nil {
		t.Fatal(err)
	}
	items, err := selectFlowExecutionItems(ctx, pool, "anthropic", run.model, run.models, nil, []string{"identity"}, "third_party", map[string]string{"identity": "full"}, defaultInspectionSettings())
	if err != nil || len(items) != 0 {
		t.Fatalf("纯第三方仍选中了内部题：%d %v", len(items), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='retired' WHERE check_key='A04'`); err != nil {
		t.Fatal(err)
	}
	carrier, err := loadAvailabilityRecoveryProbe(ctx, pool, run.model)
	if err != nil {
		t.Fatal(err)
	}
	plan := executionSnapshot{Mode: "full", Flows: []string{"intelligence"}, Models: run.models}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	if err := e.addAvailabilityRecoveryProbes(&plan, run.batch, 20, carrier); err != nil || len(plan.Instances) != 20 {
		t.Fatalf("A04未选中时恢复载体不可用：%d %v", len(plan.Instances), err)
	}
}
