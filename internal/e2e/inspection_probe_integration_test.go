//go:build integration

package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/testutil/fakesub2api"
)

// inspectionProbeAccount 通过现有管理与卖家接口创建实际并发为 1 的被测账号。
func inspectionProbeAccount(t *testing.T, e *env) (upstreamID, accountID int64, key string) {
	t.Helper()
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "default_concurrency": 1}, nil)
	admin.must("POST", "/api/admin/proxies", map[string]any{
		"name": "质检测试代理", "protocol": "socks5", "host": "10.0.0.2", "port": 2081,
	}, nil)
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{
		"enabled": true, "scheduled_enabled": false, "item_parallelism": 8,
	}, nil)
	seller := e.newUser("inspection-seller@market.test")
	var upstream idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", upstream.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upstream.ID), map[string]string{"code": "inspection-account"}, nil)
	e.passOnboarding(upstream.ID)
	var groupID int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id, probe_group_id FROM upstreams WHERE id = $1", upstream.ID), &accountID, &groupID)
	var concurrency int
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT concurrency FROM accounts WHERE id = $1", accountID), &concurrency)
	if concurrency != 1 {
		t.Fatalf("替身实际账号并发应为 1，得到 %d", concurrency)
	}
	var encrypted []byte
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT key_enc FROM system_keys WHERE sub2api_group_id = $1", groupID), &encrypted)
	plain, err := e.app.Keyring.OpenString(encrypted, fmt.Sprintf("system-key:%d", groupID))
	if err != nil {
		t.Fatal(err)
	}
	return upstream.ID, accountID, plain
}

func TestInspectionProbeConcurrentBatches(t *testing.T) {
	e := setup(t)
	upstreamID, accountID, _ := inspectionProbeAccount(t, e)
	e.fake.SetAccountDelay(accountID, 15*time.Millisecond)
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	var batches []string
	for range 2 {
		runID, err := e.app.Quality.QueueRun(ctx, upstreamID, "admin")
		if err != nil {
			t.Fatal(err)
		}
		var batch string
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT batch_id FROM probe_runs WHERE id = $1", runID), &batch)
		batches = append(batches, batch)
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(batches))
	for _, batch := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- e.app.Quality.ExecuteBatch(ctx, batch)
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, batch := range batches {
		var status, verdict string
		var results, failures int
		mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status, summary->>'verdict',
			(SELECT count(*) FROM probe_results WHERE run_id = r.id),
			(SELECT count(*) FROM probe_results WHERE run_id = r.id AND (error_class IS NOT NULL OR passed IS NOT TRUE))
			FROM probe_runs r WHERE batch_id = $1 AND trigger = 'admin'`, batch), &status, &verdict, &results, &failures)
		if status != "done" || verdict == "unavailable" || results < 12 || failures != 0 {
			t.Fatalf("单账号并发受限不应误判：status=%s verdict=%s results=%d failures=%d", status, verdict, results, failures)
		}
	}
}

func TestInspectionProbeGatewayConcurrencyIsPlatform(t *testing.T) {
	e := setup(t)
	upstreamID, _, key := inspectionProbeAccount(t, e)
	occupied, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	e.fake.SetResponder(func(_ int64, _ string) string {
		close(occupied)
		<-release
		return "PONG1"
	})
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	request := quality.ProbeRequest{Model: "claude-sonnet-4-5", Prompt: "只输出 PONG1。", MaxTokens: 32}
	first := make(chan quality.ProbeResult, 1)
	go func() { first <- e.app.Quality.Prober.Probe(ctx, "anthropic", key, request) }()
	select {
	case <-occupied:
	case <-ctx.Done():
		t.Fatal("首个请求未占用并发槽")
	}
	var outcomes []quality.Outcome
	for range 2 {
		limited := e.app.Quality.Prober.Probe(ctx, "anthropic", key, request)
		if limited.HTTPStatus != 429 || limited.ErrorClass != "platform" || limited.ErrorCode != "gateway_concurrency_limit" {
			t.Fatalf("网关本地并发错误应归因平台：%+v", limited)
		}
		outcomes = append(outcomes, quality.Outcome{Suite: "iq", Target: limited})
	}
	unblock()
	select {
	case result := <-first:
		if !result.OK() {
			t.Fatalf("已占槽请求应成功：%+v", result)
		}
		outcomes = append(outcomes, quality.Outcome{Suite: "iq", Target: result, TargetPass: true})
	case <-ctx.Done():
		t.Fatal("释放后请求未完成")
	}
	policy, err := quality.EffectivePolicy(ctx, e.app.DB, upstreamID, "anthropic", "oauth")
	if err != nil {
		t.Fatal(err)
	}
	assessment := quality.Assess(outcomes, policy.Thresholds, false)
	if assessment.Verdict == "unavailable" || assessment.AvailableTotal != 1 || assessment.AvailableOK != 1 {
		t.Fatalf("平台错误不应进入可用率分母：%+v", assessment)
	}
}

func TestInspectionProbeUsageStopsAndTools(t *testing.T) {
	e := setup(t)
	_, accountID, key := inspectionProbeAccount(t, e)
	e.fake.SetAccountUsage(accountID, &fakesub2api.GatewayUsage{
		InputTokens: 11, OutputTokens: 23, ReasoningTokens: 7, CacheReadInputTokens: 3, CacheCreationInputTokens: 5,
	})
	e.fake.SetAccountStopReason(accountID, "tool_use")
	e.fake.SetAccountToolCalls(accountID, []fakesub2api.GatewayToolCall{{
		ID: "call_local_1", Name: "lookup_schedule", Arguments: map[string]any{"city": "示例城市"},
	}})
	e.fake.SetGatewayResponder(func(_ int64, _ fakesub2api.GatewayRequest) fakesub2api.GatewayResponse {
		return fakesub2api.GatewayResponse{}
	})
	request := quality.ProbeRequest{
		Model: "claude-sonnet-4-5", MaxTokens: 64,
		Messages: []quality.ProbeMessage{{Role: "user", Content: []quality.ProbeContent{{Type: "text", Text: "查询示例城市的本地日程。"}}}},
		Tools: []quality.ProbeTool{{Name: "lookup_schedule", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"},
		}}},
	}
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	result := e.app.Quality.Prober.Probe(ctx, "anthropic", key, request)
	if !result.OK() || result.Text != "" || result.InputTokens == nil || *result.InputTokens != 11 ||
		result.CacheReadTokens == nil || *result.CacheReadTokens != 3 || result.CacheCreationTokens == nil || *result.CacheCreationTokens != 5 ||
		result.OutputTokens != 23 || result.ReasoningTokens != 7 || result.StopReason != "tool_use" || len(result.ToolCalls) != 1 {
		t.Fatalf("用量、结束原因或无文本工具调用解析错误：%+v", result)
	}
	if total := *result.InputTokens + *result.CacheReadTokens + *result.CacheCreationTokens; total != 19 {
		t.Fatalf("Claude 完整输入应累计普通输入与两类缓存：%d", total)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_local_1" || call.Name != "lookup_schedule" || call.Arguments["city"] != "示例城市" {
		t.Fatalf("工具调用语义丢失：%+v", call)
	}
	e.fake.SetAccountToolCalls(accountID, nil)
	e.fake.SetAccountStopReason(accountID, "max_tokens")
	e.fake.SetGatewayResponder(func(_ int64, _ fakesub2api.GatewayRequest) fakesub2api.GatewayResponse {
		return fakesub2api.GatewayResponse{Text: "ANSWER: 3"}
	})
	truncated := e.app.Quality.Prober.Probe(ctx, "anthropic", key, quality.ProbeRequest{
		Model: "claude-sonnet-4-5", Prompt: "只输出 ANSWER: 3。", MaxTokens: 64,
	})
	if !truncated.OK() || truncated.StopReason != "max_tokens" || !truncated.HasFlag("truncated") || truncated.EligibleForIntelligence() {
		t.Fatalf("截断应保留成功请求证据并排除智力统计：%+v", truncated)
	}
}
