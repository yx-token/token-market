//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInspectionUnsentShadowDoesNotAffectAvailability(t *testing.T) {
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	var items []Item
	for i := 0; i < 6; i++ {
		suite, prompt := "inspection_capability", "影子题"
		if i == 0 {
			suite, prompt = "iq", "旧题"
		}
		it, err := CreateItem(ctx, pool, Item{Suite: suite, Prompt: prompt, Category: "影子截止回归", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, *it)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content []ProbeContent `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Messages[0].Content[0].Text == "影子题" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"1\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t), Prober: &Prober{GatewayURL: server.URL}}
	p := &Policy{Version: 1, ItemParallelism: 1, Suites: []string{"iq", "inspection_capability"}, Thresholds: th}
	st := defaultInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, items, st)
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	rows, warnings, err := e.executePlan(deadline, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", plan, p, st, time.Second)
	if err != nil || len(warnings) == 0 {
		t.Fatalf("影子截止未隔离：%v %v", warnings, err)
	}
	assessment := Assess(rows, th, false)
	shadow := summarizeShadow(rows)
	if assessment.AvailableTotal != 1 || assessment.AvailableOK != 1 || assessment.Verdict != "normal" || shadow == nil || shadow.Checks["inspection_capability"].NotRun < 3 {
		t.Fatalf("未派发影子题污染旧结果：%+v %+v", assessment, shadow)
	}
	// 在预算读取前中断，也须保留冻结计划中的未执行计数。
	shadowPlan := plan
	shadowPlan.Instances = plan.Instances[1:]
	interrupted, stop := context.WithCancel(ctx)
	stop()
	unstarted, _, earlyErr := e.executePlanItems(interrupted, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", shadowPlan, p, st, time.Second)
	unstartedSummary := summarizeShadow(unstarted)
	if earlyErr == nil || unstartedSummary == nil || unstartedSummary.Checks["inspection_capability"].Planned != 5 || unstartedSummary.Checks["inspection_capability"].NotRun != 5 {
		t.Fatalf("派发前中断丢失影子计划：%+v %v", unstartedSummary, earlyErr)
	}
}
