//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/testutil/fakesub2api"
)

func TestInspectionBankShadowLifecycle(t *testing.T) {
	e := setup(t)
	upstreamID, accountID, _ := inspectionProbeAccount(t, e)
	// 四轮质检包含参数化题目生成，为 CI 的 race 运行预留整体时限。
	ctx, cancel := context.WithTimeout(e.ctx, 4*time.Minute)
	defer cancel()
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "质检基线测试代理", "protocol": "socks5", "host": "10.0.0.3", "port": 2082}, nil)
	var reference struct {
		Upstream idOnly `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &reference)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", reference.Upstream.ID), nil, nil)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", reference.Upstream.ID), map[string]string{"code": "inspection-reference"}, nil)
	e.passOnboarding(reference.Upstream.ID)
	// 新模式要求经运营确认健康、同档位的基线；测试准备对应快照。
	if _, err := e.app.DB.Exec(ctx, `UPDATE upstreams SET health='healthy',tier=(SELECT tier FROM upstreams WHERE id=$2) WHERE id=$1`, reference.Upstream.ID, upstreamID); err != nil {
		t.Fatal(err)
	}
	if n, err := quality.SyncInspectionItems(ctx, e.app.DB); err != nil || n == 0 {
		t.Fatalf("同步题目失败：%d %v", n, err)
	}
	e.app.Quality.RegisterInstance = func(instance quality.ProbeRegistration) {
		if instance.CheckKey == "" {
			return
		}
		response := fakesub2api.GatewayResponse{Text: instance.Answer, ToolCalls: []fakesub2api.GatewayToolCall{}}
		for _, call := range instance.Grader.Expect {
			response.ToolCalls = append(response.ToolCalls, fakesub2api.GatewayToolCall{Name: call.Name, Arguments: call.Args})
		}
		if err := e.fake.RegisterProbeAnswer(instance.WireBody, response); err != nil {
			t.Errorf("登记离线答案：%v", err)
		}
	}
	run := func() (int64, map[string]any) {
		t.Helper()
		id, err := e.app.Quality.QueueRun(ctx, upstreamID, "admin")
		if err != nil {
			t.Fatal(err)
		}
		var batch string
		mustScan(t, e.app.DB.QueryRow(ctx, `SELECT batch_id FROM probe_runs WHERE id=$1`, id), &batch)
		if err := e.app.Quality.ExecuteBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		var status string
		var raw []byte
		mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status,summary FROM probe_runs WHERE id=$1`, id), &status, &raw)
		if status != "done" {
			t.Fatalf("检测未完成：%s %s", status, raw)
		}
		var summary map[string]any
		if err := json.Unmarshal(raw, &summary); err != nil {
			t.Fatal(err)
		}
		return id, summary
	}
	// 替身账单仍按旧固定费用计，给本测试的探测影子用户补足虚拟余额。
	identity, err := e.app.Shadow.Get(ctx, buyer.KindProbe, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Admin.UpdateBalance(ctx, *identity.Sub2APIUserID, adminclient.BalanceAdd, 10000, "离线质检测试", "inspection-bank-fixture"); err != nil {
		t.Fatal(err)
	}
	_, legacy := run()
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"suites": []string{"iq", "inspection_capability", "inspection_context", "inspection_function", "inspection_output"}}, nil)
	fullID, healthy := run()
	var count, failed, actualTokens, versions int
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE passed IS NOT TRUE),count(*) FILTER (WHERE input_tokens>0),count(DISTINCT generator_version) FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%'`, fullID), &count, &failed, &actualTokens, &versions)
	if count < 70 || failed != 0 || actualTokens != count || versions < 9 {
		rows, err := e.app.DB.Query(ctx, `SELECT item_id,step,passed,COALESCE(error_class,''),error_body,raw_excerpt FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%' AND passed IS NOT TRUE LIMIT 25`, fullID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				values, _ := rows.Values()
				t.Logf("失败影子证据：%v", values)
			}
		}
		t.Logf("摘要：%+v", healthy)
		t.Fatalf("生成或静态题未正确作答：结果=%d 错误=%d 用量=%d 版本=%d", count, failed, actualTokens, versions)
	}
	if healthy["shadow"].(map[string]any)["applied"] != false {
		t.Fatal("新题参与正式判定")
	}
	e.fake.SetAccountIncorrectAnswers(accountID, true)
	badID, bad := run()
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*) FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%' AND passed IS NOT TRUE`, badID), &failed)
	if failed == 0 {
		t.Fatal("账号错误注入未生效")
	}
	for _, summary := range []map[string]any{healthy, bad} {
		for _, key := range []string{"verdict", "grade", "baseline"} {
			if summary[key] != legacy[key] {
				t.Fatalf("影子题改变旧结论 %s：%v → %v", key, legacy[key], summary[key])
			}
		}
		before := legacy["suites"].(map[string]any)
		after := summary["suites"].(map[string]any)
		for _, key := range []string{"availability", "iq", "reasoning"} {
			a, _ := json.Marshal(before[key])
			b, _ := json.Marshal(after[key])
			if string(a) != string(b) {
				t.Fatalf("影子题污染旧指标 %s：%s → %s", key, a, b)
			}
		}
	}
	var health, grade string
	var factor float64
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT health,grade,quality_factor FROM upstreams WHERE id=$1`, upstreamID), &health, &grade, &factor)
	if health != "healthy" || grade != legacy["grade"] || factor != 1 {
		t.Fatalf("影子题改变健康/等级/价格：%s %s %f", health, grade, factor)
	}
	// 显著压低影子预算，旧题仍先完成且不被影子失败吞掉。
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"token_budgets": map[string]any{"full": map[string]int{"input_tokens_per_side": 20000, "output_tokens_per_side": 20000}}}, nil)
	_, limited := run()
	if limited["verdict"] != legacy["verdict"] || limited["warnings"] == nil {
		t.Fatal("影子预算失败影响旧判定或缺告警")
	}
}
