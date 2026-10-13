package quality

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/sub2api/replica"
)

func testInspectionKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	k, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestInspectionSeedDomain(t *testing.T) {
	e := Engine{Keyring: testInspectionKeyring(t)}
	a := e.instanceSeed("batch|1", 2, 3, "4")
	if a != e.instanceSeed("batch|1", 2, 3, "4") || a == e.instanceSeed("batch", 12, 3, "4") || a == e.instanceSeed("batch|1", 2, 4, "4") || a == e.instanceSeed("batch|1", 2, 3, "5") {
		t.Fatal("seed 不稳定或存在参数边界歧义")
	}
}

func TestInspectionWindowUnknownDoesNotDefer(t *testing.T) {
	for _, a := range []*replica.Account{nil, {Extra: map[string]any{}}, {Extra: map[string]any{"unrelated": 100.0}}} {
		known, low := windowStatus(a, time.Now())
		if known || low {
			t.Fatal("未知窗口不能顺延")
		}
	}
	end := time.Now().Add(time.Hour)
	a := &replica.Account{SessionWindowEnd: &end, Extra: map[string]any{"session_window_utilization": 1.0}}
	if known, low := windowStatus(a, time.Now()); !known || !low {
		t.Fatal("明确耗尽应顺延")
	}
	end = time.Now().Add(-time.Minute)
	if known, low := windowStatus(a, time.Now()); known || low {
		t.Fatal("已重置窗口不能沿用旧不足快照")
	}
}

func TestInspectionSharedRequestLimit(t *testing.T) {
	e := Engine{}
	u := &upstreamRow{id: 1}
	st := defaultInspectionSettings()
	p := &Policy{ItemParallelism: 8}
	release, err := e.acquireRequest(context.Background(), u, p, st, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := e.acquireRequest(ctx, u, p, st, false); err == nil {
		t.Fatal("没有实际并发观测时不能超过安全并发1")
	}
	release()
	release, err = e.acquireRequest(context.Background(), u, p, st, false)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestInspectionBudgetReservations(t *testing.T) {
	b := sideBudget{limit: TokenBudget{Input: 10, Output: 20}}
	if !b.reserve(4, 10) || b.reserve(7, 1) || b.reserve(1, 11) || !b.reserve(6, 10) || b.used.Input != 10 || b.used.Output != 20 {
		t.Fatal("预算越界或失败预留影响计数")
	}
	_, output, err := probeTokenReservation("anthropic", ProbeRequest{Model: "claude-sonnet-4-5", Prompt: "只回答结果", MaxTokens: 1024, Effort: "high"})
	if err != nil || output < 4096 {
		t.Fatalf("预算未使用实际协议输出上限：%d %v", output, err)
	}
}

func TestInspectionDynamicRequestUsesRealOutput(t *testing.T) {
	prev := ProbeRequest{Prompt: "查班次", Model: "m"}
	step := plannedStep{Request: ProbeRequest{Prompt: "只输出日期"}, UsePrevious: true, ToolResults: map[string]any{"schedule": map[string]any{"date": "2026-10-12"}}}
	req := dynamicRequest(step, prev, resumedState{Complete: true, Text: "真实上一轮文本", ToolCalls: []ProbeToolCall{{ID: "call-1", Name: "schedule", Arguments: map[string]any{"city": "杭州"}}}})
	if len(req.Messages) != 4 || req.Messages[1].Content[0].Text != "真实上一轮文本" || req.Messages[2].Content[0].ToolUseID != "call-1" || !strings.Contains(req.Messages[2].Content[0].Content.(string), "2026-10-12") {
		t.Fatalf("未保留真实会话和本地工具结果：%+v", req.Messages)
	}
}

func TestInspectionAvailabilityExcludesPlatformButDoesNotDependOnPairs(t *testing.T) {
	rows := outcomes(5, 0, true)
	for i := range rows {
		rows[i].Suite = "inspection_capability"
		rows[i].Target.ErrorClass = "platform"
		rows[i].Target.HTTPStatus = 429
	}
	if a := Assess(rows, th, true); a.Verdict != "inconclusive" || a.AvailableTotal != 0 || a.Samples != 0 {
		t.Fatalf("平台并发被当作卖家失败：%+v", a)
	}
	rows = outcomes(5, 0, true)
	for i := range rows {
		rows[i].Suite = "inspection_capability"
		rows[i].Ref.Flags = []string{"truncated"}
		rows[i].Ref.ErrorClass = "http_5xx"
	}
	if a := Assess(rows, th, true); a.Samples != 0 || a.AvailableOK != 5 || a.AvailableTotal != 5 || a.Verdict != "normal" || a.ReferenceErrorCounts["http_5xx"] != 5 {
		t.Fatalf("目标可用率不应依赖基线配对，基线故障仍须留证据：%+v", a)
	}
}

func TestInspectionLegacyBudgetOnlyWarns(t *testing.T) {
	b := sideBudget{limit: TokenBudget{Input: 1, Output: 1}, soft: true}
	for range 2 {
		if !b.reserve(10, 20) {
			t.Fatal("旧题预算超支不应停止派发")
		}
	}
	if b.used.Input != 20 || b.used.Output != 40 {
		t.Fatal("旧题预算超支不应停止派发，且必须累计消耗")
	}
}

func TestInspectionRetryWaitBound(t *testing.T) {
	for _, tc := range []struct {
		after time.Duration
		wait  time.Duration
		retry bool
	}{{0, time.Second, true}, {30 * time.Second, 30 * time.Second, true}, {31 * time.Second, 0, false}, {time.Hour, 0, false}} {
		wait, retry := inspectionRetryDelay(tc.after, 0)
		if wait != tc.wait || retry != tc.retry {
			t.Fatalf("重试等待不匹配：%v %v", wait, retry)
		}
	}
}
