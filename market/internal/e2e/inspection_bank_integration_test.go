//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/quality"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/testutil/fakesub2api"
)

func TestInspectionBankFlowLifecycle(t *testing.T) {
	e := setup(t)
	upstreamID, accountID, _ := inspectionProbeAccount(t, e)
	// 验证参数化、静态、多步及硬预算路径，保留宽裕的 CI 时限。
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
	// 完整默认题库的全部内置键和生成器判分由正式上架完整轮验证。
	// 流程检测只需代表性能力题、三种 8K 上下文、两步工具与完整输出，
	// 仍同时走参数化、静态、多步及低档推理锚点路径。
	selectInspectionItems(t, e, "inspection/aime_math/1", "inspection/context/0/1", "inspection/context/1/1", "inspection/context/2/1", "inspection/tools/1", "inspection/output/2", "inspection/pigeonhole/astra/1")
	// 复用既有试运行检测轮验证迁移后的糖果题，不增加完整检测。
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"flows": testDetectionFlows([]string{"gpt-6-astra"}, "self", true)}, nil)
	if _, err := e.app.DB.Exec(ctx, `UPDATE probe_items SET status='retired' WHERE builtin_key IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE probe_items SET generator=jsonb_set(generator,'{instances,full}','20'::jsonb) WHERE builtin_key='inspection/aime_math/1'`); err != nil {
		t.Fatal(err)
	}

	var candyID int64
	var candyPrompt string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT id,prompt FROM probe_items WHERE builtin_key='inspection/pigeonhole/astra/1'`), &candyID, &candyPrompt)
	e.app.Quality.RegisterInstance = func(instance quality.ProbeRegistration) {
		if instance.CheckKey == "" {
			return
		}
		response := fakesub2api.GatewayResponse{Text: instance.Answer, ToolCalls: []fakesub2api.GatewayToolCall{}}
		if instance.Request.Prompt == candyPrompt {
			response.Text = "21"
		}
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
	var initialGrade *string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT grade FROM upstreams WHERE id=$1`, upstreamID), &initialGrade)
	fullID, healthy := run()
	var count, failed, actualTokens, versions int
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE passed IS NOT TRUE),count(*) FILTER (WHERE input_tokens>0),count(DISTINCT generator_version) FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%'`, fullID), &count, &failed, &actualTokens, &versions)
	if count != 37 || failed != 0 || actualTokens != count || versions != 3 {
		rows, err := e.app.DB.Query(ctx, `SELECT item_id,step,passed,COALESCE(error_class,''),error_body,raw_excerpt FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%' AND passed IS NOT TRUE LIMIT 25`, fullID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				values, _ := rows.Values()
				t.Logf("失败质检证据：%v", values)
			}
		}
		t.Logf("摘要：%+v", healthy)
		t.Fatalf("生成或静态题未正确作答：结果=%d 错误=%d 用量=%d 版本=%d", count, failed, actualTokens, versions)
	}
	var missing int
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*) FROM probe_items i WHERE i.suite LIKE 'inspection_%' AND i.status='active' AND NOT EXISTS(SELECT 1 FROM probe_results pr WHERE pr.run_id=$1 AND pr.item_id=i.id AND pr.passed IS TRUE)`, fullID), &missing)
	if missing != 0 {
		t.Fatalf("代表性质检题缺少判分证据：%d", missing)
	}
	// 三道流程都要有通过结论才算正常：降智检测的型号专用题本轮通过。
	degradation := healthy["degradation"].(map[string]any)
	if healthy["verdict"] != "normal" || healthy["intelligence"].(map[string]any)["status"] != "pass" || degradation["status"] != "pass" {
		t.Fatalf("所有题目通过时流程应正常：%+v", healthy)
	}

	e.fake.SetAccountIncorrectAnswers(accountID, true)
	badID, bad := run()
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*) FROM probe_results WHERE run_id=$1 AND suite LIKE 'inspection_%' AND passed IS NOT TRUE`, badID), &failed)
	if failed == 0 {
		t.Fatal("账号错误注入未生效")
	}
	if bad["verdict"] != "suspected" {
		t.Fatalf("测智偏低须判疑似：%+v", bad)
	}
	for _, id := range []int64{fullID, badID} {
		var oldRows int
		var evidence bool
		mustScan(t, e.app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM probe_results WHERE run_id=r.id AND suite IN('iq','reasoning','long_context')),EXISTS(SELECT 1 FROM probe_results WHERE run_id=r.id AND item_id=$2 AND suite='inspection_degradation' AND check_key='D14' AND generator_version='static-1') FROM probe_runs r WHERE r.id=$1`, id, candyID), &oldRows, &evidence)
		if oldRows != 0 || !evidence {
			t.Fatal("新流程应下线旧智力题并保留型号专用D14静态证据")
		}
	}

	var health string
	var grade *string
	var factor float64
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT health,grade,quality_factor FROM upstreams WHERE id=$1`, upstreamID), &health, &grade, &factor)
	if health != "suspected" || !reflect.DeepEqual(grade, initialGrade) || factor != 1 {
		t.Fatalf("默认未开启扣价，疑似不得修改等级或价格：%s %v %f", health, grade, factor)
	}
	// 所有新题共用硬预算，预算不足必须终止并保留已完成历史。
	e.fake.SetAccountIncorrectAnswers(accountID, false)
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"token_budgets": map[string]any{"full": map[string]int{"input_tokens_per_side": 20000, "output_tokens_per_side": 20000}}}, nil)
	id, err := e.app.Quality.QueueRun(ctx, upstreamID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var batch string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT batch_id FROM probe_runs WHERE id=$1`, id), &batch)
	if err := e.app.Quality.ExecuteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status,summary->>'error' FROM probe_runs WHERE id=$1`, id), &status, &reason)
	if status != "failed" || !strings.Contains(reason, "预算") {
		t.Fatalf("流程硬预算不足应失败：%s %s", status, reason)
	}
}
