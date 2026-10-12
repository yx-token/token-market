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
	if _, err := SeedDefaultItems(ctx, pool); err != nil {
		t.Fatal(err)
	}
	suites := []string{"iq", "reasoning", "long_context"}
	var candy Item
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol"} {
		items, err := selectExecutionItems(ctx, pool, "openai", model, suites, "full")
		if err != nil {
			t.Fatal(err)
		}
		formal := formalItems(items)
		if model == "gpt-6-astra" {
			if len(items) != 22 || len(formal) != 1 || formal[0].Models[0] != model || formal[0].CheckKey != "" {
				t.Fatalf("限定型号旧题选择错误或被改写：%+v", formal)
			}
			candy = formal[0]
		} else if len(items) != 21 || len(formal) != 0 {
			t.Fatalf("其他型号选中了专用旧题：%+v", formal)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='retired' WHERE id=$1`, candy.ID); err != nil {
		t.Fatal(err)
	}
	items, err := selectExecutionItems(ctx, pool, "openai", "gpt-6-astra", suites, "full")
	if err != nil || len(formalItems(items)) != 0 {
		t.Fatal("下线题不应执行", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='active' WHERE id=$1`, candy.ID); err != nil {
		t.Fatal(err)
	}
	// 另一个静态题也使用 idx=0；item_id 和冻结哈希须独立，不能串配。
	second := candy
	second.ID, second.Suite, second.Prompt, second.CheckKey = 0, "iq", "只输出 1。", "D08"
	second.Grader = Grader{Type: "exact", Answer: "1"}
	created, err := CreateItem(ctx, pool, second)
	if err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	p := &Policy{Version: 1, Suites: suites, Thresholds: th}
	st := calibratedInspectionSettings()
	run.model = "gpt-6-astra"
	plan, err := e.executionPlan(ctx, run, p, []Item{candy, *created}, st)
	if err != nil || len(plan.Instances) != 2 {
		t.Fatalf("静态计划生成错误：%+v %v", plan, err)
	}
	a, b := plan.Instances[0], plan.Instances[1]
	if a.InstanceIdx != 0 || b.InstanceIdx != 0 || a.GeneratorVersion != "static-1" || a.Hash == "" || a.Hash == b.Hash || a.ItemID == b.ItemID || a.CheckKey != "D07" || b.CheckKey != "D08" || a.Item.Suite != "reasoning" {
		t.Fatalf("静态配对身份或归类错误：%+v %+v", a, b)
	}
	result := ProbeResult{HTTPStatus: 200, Text: "21", Flags: []string{"stop_normal"}}
	for _, runID := range []int64{run.id, ref.id} {
		if err := e.saveInstanceResult(ctx, runID, a, 0, result, true, resumedState{Complete: true, Text: "21"}); err != nil {
			t.Fatal(err)
		}
		if err := e.saveInstanceResult(ctx, runID, b, 0, result, false, resumedState{Complete: true, Text: "21"}); err != nil {
			t.Fatal(err)
		}
		for _, it := range []plannedInstance{a, b} {
			got, passed, _, found, err := e.loadInstanceResult(ctx, runID, it, 0)
			if err != nil || !found || got.Text != "21" || passed != (it.ItemID == a.ItemID) {
				t.Fatalf("静态题配对加载串题：%+v %v %v %v", got, passed, found, err)
			}
		}
	}
	// 重启续跑继续读冻结 key/hash；不要求管理员补写 check_key。
	reloaded, err := e.executionPlan(ctx, run, p, nil, defaultInspectionSettings())
	if err != nil || !reflect.DeepEqual(mustJSON(plan), mustJSON(reloaded)) {
		t.Fatalf("续跑改变静态配对计划：%v", err)
	}
	var key, suite string
	if err := pool.QueryRow(ctx, `SELECT check_key,suite FROM probe_items WHERE id=$1`, candy.ID).Scan(&key, &suite); err != nil || key != "" || suite != "reasoning" {
		t.Fatal("运行时派生不应改写题库", err)
	}
}
