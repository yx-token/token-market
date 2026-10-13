//go:build integration

package quality

import (
	"context"
	"reflect"
	"testing"
)

func TestInspectionBuiltinSyncAndFlowSelection(t *testing.T) {
	t.Parallel()
	pool, _, _ := inspectionDB(t)
	ctx := context.Background()
	created, err := SeedDefaultItems(ctx, pool)
	if err != nil || created != len(inspectionBuiltinItems()) {
		t.Fatalf("启动同步失败：%d %v", created, err)
	}
	var legacyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_items WHERE suite IN ('iq','reasoning','long_context') AND status='active'`).Scan(&legacyCount); err != nil || legacyCount != 0 {
		t.Fatalf("启动不应重新写入旧题：%d %v", legacyCount, err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM probe_items WHERE builtin_key='inspection/zebra/2'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_items SET status='retired',prompt='管理员私有题面',version=9 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM probe_items WHERE builtin_key='inspection/knights_knaves/1'`); err != nil {
		t.Fatal(err)
	}
	if n, err := SyncInspectionItems(ctx, pool); err != nil || n != 1 {
		t.Fatalf("补缺不正确：%d %v", n, err)
	}
	var status, prompt string
	var version int
	if err := pool.QueryRow(ctx, `SELECT status,prompt,version FROM probe_items WHERE id=$1`, id).Scan(&status, &prompt, &version); err != nil || status != "retired" || prompt != "管理员私有题面" || version != 9 {
		t.Fatal("启动复活下线题或覆盖管理员编辑", err)
	}
	quick, err := selectFlowExecutionItems(ctx, pool, "anthropic", "claude-sonnet-4-5", []string{"claude-sonnet-4-5"}, []string{"intelligence", "identity"}, "self", map[string]string{"intelligence": "quick", "identity": "quick"}, defaultInspectionSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range quick {
		if it.Suite == "inspection_context" {
			t.Fatal("快检选择了完整长上下文题")
		}
	}
}

func TestInspectionGeneratedSnapshotIsFrozen(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	var selected Item
	for _, it := range inspectionBuiltinItems() {
		if it.BuiltinKey != nil && *it.BuiltinKey == "inspection/register_machine/1" {
			selected = it
			break
		}
	}
	it, err := CreateItem(ctx, pool, selected)
	if err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	p := &Policy{Version: 1, Suites: []string{"inspection_capability"}, Thresholds: th}
	st := defaultInspectionSettings()
	a, err := e.executionPlan(ctx, run, p, []Item{*it}, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Instances) != 16 || a.Instances[0].GeneratorVersion != "register_machine/1" {
		t.Fatalf("实例数版本错误：%+v", a)
	}
	for _, instance := range a.Instances {
		if instance.Hash == "" || instance.Seed == "" || instance.Steps[0].Answer == "" {
			t.Fatal("生成实例信息缺失")
		}
	}
	it.Prompt = "管理员后续改题"
	it.Generator = []byte(`{"template_id":"zebra","version":"2"}`)
	b, err := e.executionPlan(ctx, run, p, []Item{*it}, st)
	if err != nil || !reflect.DeepEqual(mustJSON(a), mustJSON(b)) {
		t.Fatal("恢复时重新生成题面", err)
	}
	var copies int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_runs WHERE batch_id=$1 AND execution_snapshot<>'{}'`, run.batch).Scan(&copies); err != nil || copies != 1 {
		t.Fatalf("每批快照应唯一：%d %v", copies, err)
	}
}

func TestInspectionInvalidTemplatePreservesValidItems(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	legacy := Item{ID: 1, Suite: "inspection_capability", CheckKey: "D07", Prompt: "有效题", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}}
	shadow := Item{ID: 2, Suite: "inspection_capability", CheckKey: "D07", MaxTokens: 32, Generator: []byte(`{"template_id":"zebra","version":"1","parameters":{"houses":7}}`)}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	p := &Policy{Version: 1, Suites: []string{"inspection_capability"}, Thresholds: th}
	plan, err := e.executionPlan(ctx, run, p, []Item{shadow, legacy}, defaultInspectionSettings())
	if err != nil || len(plan.Instances) != 1 || plan.Instances[0].ItemID != 1 || len(plan.Warnings) == 0 {
		t.Fatalf("无效模板挡住有效题：%+v %v", plan, err)
	}
	if err := validateItem(shadow); err == nil {
		t.Fatal("非法参数域不应允许保存")
	}
}

func TestInspectionExpansionLimitIsPerModel(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	items := []Item{}
	for i := 0; i < 8; i++ {
		items = append(items, Item{ID: int64(i + 1), Suite: "inspection_capability", CheckKey: "D07", MaxTokens: 32, Generator: []byte(`{"template_id":"register_machine","version":"1","instances":{"full":32}}`)})
	}
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t)}
	plan, err := e.executionPlan(ctx, run, &Policy{Version: 1, Suites: []string{"inspection_capability"}, Thresholds: th}, items, defaultInspectionSettings())
	if err == nil || len(plan.Instances) != 200 || len(plan.Warnings) == 0 {
		t.Fatalf("超限计划不能作为完整覆盖执行：%d %v %v", len(plan.Instances), plan.Warnings, err)
	}
}
