//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInspectionUnsentItemsDoNotAffectAvailability(t *testing.T) {
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	var items []Item
	for i := 0; i < 6; i++ {
		suite, prompt := "inspection_capability", "慢请求"
		if i == 0 {
			suite, prompt = "inspection_capability", "成功请求"
		}
		it, err := CreateItem(ctx, pool, Item{Suite: suite, Prompt: prompt, CheckKey: "D07", Category: "截止统计回归", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}})
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
		if request.Messages[0].Content[0].Text == "慢请求" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"1\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	e := Engine{DB: pool, Keyring: testInspectionKeyring(t), Prober: &Prober{GatewayURL: server.URL}}
	p := &Policy{Version: 1, ItemParallelism: 1, Suites: []string{"inspection_capability"}, Thresholds: th}
	st := defaultInspectionSettings()
	plan, err := e.executionPlan(ctx, run, p, items, st)
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	rows, _, err := e.executePlan(deadline, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", plan, p, st, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("截止后执行应停止：%v", err)
	}
	assessment := assessFormal(rows, th, st)
	unobserved := 0
	for _, row := range rows {
		if row.Target.HTTPStatus == 0 && row.Target.ErrorClass == "" {
			unobserved++
		}
	}
	if assessment.AvailableOK != 1 || assessment.AvailableTotal > 2 || unobserved < 3 {
		t.Fatalf("未派发题污染可用分母：%+v；未派发=%d", assessment, unobserved)
	}
	// 派发前中断仍保留计划，未观测不能伪造可用率失败。
	shadowPlan := plan
	shadowPlan.Instances = plan.Instances[1:]
	interrupted, stop := context.WithCancel(ctx)
	stop()
	unstarted, _, earlyErr := e.executePlanItems(interrupted, run, nil, &upstreamRow{id: run.upstreamID, platform: "anthropic"}, nil, "test", "", shadowPlan, p, st, time.Second)
	unstartedSummary := assessFormal(unstarted, th, st)
	if earlyErr == nil || len(unstarted) != 5 || unstartedSummary.AvailableTotal != 0 {
		t.Fatalf("派发前中断丢失计划或计入可用分母：%+v %v", unstartedSummary, earlyErr)
	}
}
