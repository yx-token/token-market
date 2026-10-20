package quality

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestSelectedFlowsQueueCombinations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		trigger   string
		requested []string
		change    func(*Policy)
		want      []string
		code      string
	}{
		{name: "管理员默认全部", trigger: "admin", want: []string{"intelligence", "degradation", "identity"}},
		{name: "管理员只测身份", trigger: "admin", requested: []string{"identity"}, want: []string{"identity"}},
		{name: "管理员只测测智", trigger: "admin", requested: []string{"intelligence"}, want: []string{"intelligence"}},
		{name: "管理员只测降智", trigger: "admin", requested: []string{"degradation"}, want: []string{"degradation"}},
		{name: "管理员重复选择规范化", trigger: "admin", requested: []string{"identity", "identity", "intelligence"}, want: []string{"intelligence", "identity"}},
		{name: "管理员不能选关闭流程", trigger: "admin", requested: []string{"identity"}, change: func(p *Policy) { p.Flows.Identity.Enabled = false }, code: "NO_FLOWS"},
		{name: "管理员不能选关闭的降智流程", trigger: "admin", requested: []string{"degradation"}, change: func(p *Policy) { p.Flows.Degradation.Enabled = false }, code: "NO_FLOWS"},
		{name: "管理员总开关例外", trigger: "admin", change: func(p *Policy) { p.Enabled = false }, want: []string{"intelligence", "degradation", "identity"}},
		{name: "上架忽略触发限制", trigger: "onboarding", change: func(p *Policy) {
			p.Enabled = false
			p.Flows.Intelligence.Triggers, p.Flows.Degradation.Triggers, p.Flows.Identity.Triggers = nil, nil, nil
		}, want: []string{"intelligence", "degradation", "identity"}},
		{name: "上架只测已启用流程", trigger: "onboarding", change: func(p *Policy) { p.Flows.Identity.Enabled = false }, want: []string{"intelligence", "degradation"}},
		{name: "定时按触发选择", trigger: "scheduled", want: []string{"intelligence", "degradation"}},
		{name: "定时两流程都匹配", trigger: "scheduled", change: func(p *Policy) { p.Flows.Identity.Triggers = []string{"scheduled"} }, want: []string{"intelligence", "degradation", "identity"}},
		{name: "定时不选关闭流程", trigger: "scheduled", change: func(p *Policy) { p.Flows.Intelligence.Enabled = false }, want: []string{"degradation"}},
		{name: "没有匹配触发", trigger: "other", code: "NO_FLOWS"},
		{name: "指定流程仍遵守自动触发限制", trigger: "adaptive", requested: []string{"intelligence"}, code: "NO_FLOWS"},
		{name: "拒绝未知流程", trigger: "admin", requested: []string{"capability"}, code: "INVALID_FLOW"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Policy{Enabled: true, Flows: policyFlowsFixture()}
			p.Flows.Intelligence.Triggers = []string{"scheduled"}
			p.Flows.Degradation.Triggers = []string{"scheduled"}
			p.Flows.Identity.Triggers = []string{"appeal"}
			if tc.change != nil {
				tc.change(&p)
			}
			// 这些触发不查询状态，保证入队范围不依赖数据库替身。
			e := &Engine{}
			got, err := e.selectedFlows(context.Background(), &upstreamRow{id: 1}, &p, tc.trigger, tc.requested)
			if tc.code != "" {
				var he *httpx.Error
				if !errors.As(err, &he) || he.Code != tc.code {
					t.Fatalf("期望错误 %s，实际流程=%v，错误=%v", tc.code, got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("入队流程=%v，期望=%v，错误=%v", got, tc.want, err)
			}
		})
	}
}

func TestModesForFlowsKeepEachFlowMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
		flows   []string
		failed  []string
		want    string
		modes   map[string]string
	}{
		{"身份完整测智快检", "scheduled", []string{"intelligence", "identity"}, nil, "full", map[string]string{"intelligence": "quick", "identity": "full"}},
		{"测智完整身份快检", "admin", []string{"intelligence", "identity"}, nil, "full", map[string]string{"intelligence": "full", "identity": "quick"}},
		{"只测快检流程", "scheduled", []string{"intelligence"}, nil, "quick", map[string]string{"intelligence": "quick"}},
		{"上架强制完整", "onboarding", []string{"intelligence", "identity"}, nil, "full", map[string]string{"intelligence": "full", "identity": "full"}},
		{"申诉强制完整", "appeal", []string{"identity"}, nil, "full", map[string]string{"identity": "full"}},
		{"上下文异常追加完整", "adaptive", []string{"intelligence"}, []string{"D09"}, "full", map[string]string{"intelligence": "full"}},
		{"推理异常追加完整", "adaptive", []string{"intelligence"}, []string{"D06"}, "full", map[string]string{"intelligence": "full"}},
		{"智力异常追加快检", "adaptive", []string{"intelligence"}, []string{"D07"}, "quick", map[string]string{"intelligence": "quick"}},
		{"恢复失败项完整", "recovery", []string{"intelligence"}, []string{"D13"}, "full", map[string]string{"intelligence": "full"}},
		{"卖家扣价复测完整", "seller_manual", []string{"identity"}, []string{"D08"}, "full", map[string]string{"identity": "full"}},
		{"降智上架强制完整", "onboarding", []string{"degradation"}, nil, "full", map[string]string{"degradation": "full"}},
		{"降智按完整触发完整", "admin", []string{"degradation"}, nil, "full", map[string]string{"degradation": "full"}},
		{"降智未在完整触发时快检", "scheduled", []string{"degradation"}, nil, "quick", map[string]string{"degradation": "quick"}},
		{"降智失败项追加完整", "adaptive", []string{"degradation"}, []string{"D14"}, "full", map[string]string{"degradation": "full"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Policy{Flows: policyFlowsFixture()}
			p.Flows.Intelligence.FullTriggers = []string{"admin"}
			p.Flows.Degradation.FullTriggers = []string{"admin"}
			p.Flows.Identity.FullTriggers = []string{"scheduled"}
			mode, modes := modesForFlows(&p, tc.flows, tc.trigger, tc.failed)
			if mode != tc.want || !reflect.DeepEqual(modes, tc.modes) {
				t.Fatalf("运行模式=%s，流程模式=%v，期望=%s/%v", mode, modes, tc.want, tc.modes)
			}
		})
	}
}

func flowStateCheck(key, status string) inspection.Check {
	return inspection.Check{Key: key, Status: status, Samples: inspection.Samples{Observed: 20}, Evidence: inspection.Evidence{Source: "item_probe"}}
}

func TestDecideVerdictAvailabilityAndGateTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		ok    int
		total int
		gate  *inspection.Gate
		want  string
	}{
		{name: "试运行两个请求全失败", mode: "trial", total: 2, want: "unavailable"},
		{name: "试运行三次不足一半", mode: "trial", ok: 1, total: 3, want: "unavailable"},
		{name: "试运行成功率等于一半", mode: "trial", ok: 1, total: 2, want: "normal"},
		{name: "试运行单次失败不确认", mode: "trial", total: 1, want: "normal"},
		{name: "正式不沿用两请求门槛", mode: "formal", total: 2, want: "normal"},
		{name: "正式未确认自有门槛", mode: "formal", gate: &inspection.Gate{CheckKey: "A04", Health: "unavailable"}, want: "normal"},
		{name: "正式确认权限门槛", mode: "formal", gate: &inspection.Gate{CheckKey: "A04", Health: "unavailable", Confirmed: true}, want: "unavailable"},
		{name: "正式确认身份门槛", mode: "formal", gate: &inspection.Gate{CheckKey: "A05", Health: "degraded", Confirmed: true}, want: "degraded"},
		{name: "试运行不应用自有正式门槛", mode: "trial", gate: &inspection.Gate{CheckKey: "A04", Health: "unavailable", Confirmed: true}, want: "normal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runRow{flows: []string{"intelligence"}, models: []string{"模型甲"}}
			a := Assessment{InspectionMode: tc.mode, AvailableOK: tc.ok, AvailableTotal: tc.total, Checks: map[string]inspection.Check{"D07": flowStateCheck("D07", "pass")}}
			if tc.gate != nil {
				a.Gates = []inspection.Gate{*tc.gate}
			}
			if got := decideVerdict(run, a, nil); got != tc.want {
				t.Fatalf("结论=%s，期望=%s：%+v", got, tc.want, a)
			}
		})
	}
}

func TestDecideVerdictIntelligenceOnlySuspected(t *testing.T) {
	for _, mode := range []string{inspection.ModeTrial, inspection.ModeFormal} {
		for _, key := range []string{"D07", "D07.honesty", "D09", "D06"} {
			for _, status := range []string{"warning", "fail"} {
				t.Run(mode+"/"+key+"/"+status, func(t *testing.T) {
					run := runRow{flows: []string{"intelligence"}, models: []string{"模型甲"}}
					a := Assessment{InspectionMode: mode, Verdict: "degraded", LLR: 100, Checks: map[string]inspection.Check{key: flowStateCheck(key, status)}}
					if got := decideVerdict(run, a, nil); got != "suspected" {
						t.Fatalf("测智异常只能标疑似，实际=%s：%+v", got, a)
					}
				})
			}
		}
	}
}

func TestDecideVerdictSelfGateRequiresFormalCalibration(t *testing.T) {
	for _, mode := range []string{inspection.ModeTrial, inspection.ModeFormal} {
		for _, calibrated := range []bool{false, true} {
			st := inspection.Defaults()
			st.CheckOverrides["A04"] = inspection.CheckOverride{Calibrated: calibrated, CalibrationNote: "同入口权限门槛校准记录"}
			checks := map[string]inspection.Check{"A04": flowStateCheck("A04", "fail")}
			gates := []inspection.Gate{{CheckKey: "A04", Health: "unavailable", Confirmed: true}}
			result := inspection.Aggregate(checks, gates, mode, st)
			a := Assessment{InspectionMode: mode, AvailableOK: 2, AvailableTotal: 2, Checks: result.Checks, Gates: result.Gates}
			want := "normal"
			if mode == inspection.ModeFormal && calibrated {
				want = "unavailable"
			}
			if got := decideVerdict(runRow{flows: []string{"identity"}, identitySource: "self"}, a, nil); got != want {
				t.Fatalf("自有门槛须正式且已校准，模式=%s，校准=%t，结论=%s，期望=%s", mode, calibrated, got, want)
			}
		}
	}
}

func TestDecideVerdictThirdPartyActionsWithoutCalibration(t *testing.T) {
	for _, mode := range []string{inspection.ModeTrial, inspection.ModeFormal} {
		for _, action := range []string{"degraded", "suspected", "ignore"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				st := inspection.Defaults()
				st.Mode = mode
				run := runRow{flows: []string{"identity"}, identitySource: "third_party"}
				site := ThirdPartyCheck{OnMismatch: action}
				tp := []tpResult{{Provider: "bazaarlink", Status: "completed", Outcome: "mismatch", Verdict: site.VerdictFor("mismatch")}}
				a := Assessment{InspectionMode: mode, InspectionSettings: &st, Checks: map[string]inspection.Check{}}
				addThirdPartyInspection(&a, tp, time.Time{})
				want := action
				if action == "ignore" {
					want = "normal"
				}
				if got := decideVerdict(run, a, tp); got != want {
					t.Fatalf("未校准的第三方应按动作处置，实际=%s，期望=%s", got, want)
				}
				if action == "degraded" && mode == inspection.ModeFormal && (len(a.Gates) != 1 || !a.Gates[0].Confirmed) {
					t.Fatalf("第三方正式门槛不应依赖校准：%+v", a.Gates)
				}
			})
		}
	}
}

func TestDecideVerdictMissingTargetFlowIsInconclusive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		run    runRow
		checks map[string]inspection.Check
	}{
		{"测智没有有效观测", runRow{flows: []string{"intelligence"}, models: []string{"模型甲"}}, map[string]inspection.Check{"D07": flowStateCheck("D07", "inconclusive")}},
		{"身份等待第三方", runRow{flows: []string{"identity"}, identitySource: "third_party"}, map[string]inspection.Check{"third_party": flowStateCheck("third_party", "not_run")}},
		{"测智通过身份无结论", runRow{flows: []string{"intelligence", "identity"}, identitySource: "third_party", models: []string{"模型甲"}}, map[string]inspection.Check{"D07": flowStateCheck("D07", "pass"), "third_party": flowStateCheck("third_party", "inconclusive")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Assessment{InspectionMode: "trial", Checks: tc.checks}
			if got := decideVerdict(tc.run, a, nil); got != "inconclusive" {
				t.Fatalf("目标流程没有结论不能记正常，实际=%s", got)
			}
		})
	}
	if got := decideVerdict(runRow{flows: []string{"intelligence"}, models: []string{}}, Assessment{InspectionMode: "trial"}, nil); got != "normal" {
		t.Fatalf("空交集应记不适用，不能记检测失败：%s", got)
	}
}

func TestFlowHistoryCannotReplaceUnrunFlowConclusion(t *testing.T) {
	at := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	history := flowStateCheck("D07", "fail")
	history.Evidence.Source, history.CheckedAt = "history", at
	run := runRow{flows: []string{"identity"}, identitySource: "third_party", models: []string{"模型甲"}}
	a := Assessment{InspectionMode: "trial", Checks: map[string]inspection.Check{"D07": history, "third_party": flowStateCheck("third_party", "pass")}}
	if got := decideVerdict(run, a, nil); got != "normal" {
		t.Fatalf("未跑的历史异常不能变成本次目标结论：%s", got)
	}
	intelligence := summarizeFlow("intelligence", run, a)
	if intelligence.Status != "fail" || intelligence.InheritedAt == nil || !intelligence.InheritedAt.Equal(at) || !reflect.DeepEqual(run.flows, []string{"identity"}) {
		t.Fatalf("另一流程正常不能覆盖历史测智异常或扩大目标流程：%+v，%v", intelligence, run.flows)
	}
	current := flowStateCheck("D07", "pass")
	a.Checks["D07"] = current
	if summary := summarizeFlow("intelligence", run, a); summary.Status != "not_run" || summary.InheritedAt != nil {
		t.Fatalf("未跑流程只能展示继承证据：%+v", summary)
	}
}

func TestIntelligenceModelWhitelistIntersection(t *testing.T) {
	p := Policy{Flows: policyFlowsFixture()}
	p.Flows.Intelligence.Models = []IntelligenceModel{{Model: "模型甲"}, {Model: "模型乙"}, {Model: "模型丙"}}
	for _, tc := range []struct {
		name      string
		whitelist []string
		want      []string
	}{
		{"未声明取全部", nil, []string{"模型甲", "模型乙", "模型丙"}},
		{"显式空白名单不匹配", []string{}, []string{}},
		{"按策略顺序取交集", []string{"模型丙", "模型甲"}, []string{"模型甲", "模型丙"}},
		{"无交集", []string{"模型丁"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := intelligenceModels(&p, tc.whitelist); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("测智模型=%v，期望=%v", got, tc.want)
			}
		})
	}
}

func TestFlowSummaryPublicModelsAndAppliedFactor(t *testing.T) {
	at := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	run := runRow{trigger: "admin", mode: "full", model: "模型甲", models: []string{"模型甲", "模型乙"}, flows: []string{"intelligence", "identity"}, identitySource: "third_party"}
	c := flowStateCheck("D07", "warning")
	c.Applied, c.Factor = true, numberPointer(.85)
	c.Metrics = map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{
		{Model: "模型甲", Status: "pass", TargetPassRate: numberPointer(.987654), ExpectedPassRate: numberPointer(.912345), Samples: 47, Metrics: map[string]inspection.Metric{"内部指标": {Value: numberPointer(.923456)}}},
		{Model: "模型乙", Status: "warning", TargetPassRate: numberPointer(.654321), ExpectedPassRate: numberPointer(.812345), Samples: 53},
	}}}
	inactive := flowStateCheck("A02", "warning")
	inactive.Factor = numberPointer(.7)
	a := Assessment{InspectionMode: "trial", Verdict: "suspected", Checks: map[string]inspection.Check{"D07": c, "A02": inactive, "third_party": flowStateCheck("third_party", "pass")}}
	e := &Engine{Now: func() time.Time { return at }}
	raw := mustJSON(e.buildSummary(run, &upstreamRow{}, a, "account"))
	var admin struct {
		Intelligence flowSummary `json:"intelligence"`
	}
	if err := json.Unmarshal(raw, &admin); err != nil || len(admin.Intelligence.Models) != 2 || admin.Intelligence.Models[0].TargetPassRate == nil || admin.Intelligence.Models[0].Samples != 47 {
		t.Fatalf("管理员摘要未保留完整模型指标：%s，%v", raw, err)
	}
	safe := publicReportSummary(raw)
	var public struct {
		Flows       []string `json:"flows"`
		Models      []string `json:"models"`
		Degradation struct {
			Status string                       `json:"status"`
			Models []map[string]json.RawMessage `json:"models"`
		} `json:"degradation"`
		Intelligence struct {
			Status string                       `json:"status"`
			Models []map[string]json.RawMessage `json:"models"`
		} `json:"intelligence"`
		Checks map[string]inspection.Check `json:"checks"`
	}
	if err := json.Unmarshal(safe, &public); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(public.Flows, run.flows) || !reflect.DeepEqual(public.Models, run.models) || public.Intelligence.Status != "warning" || len(public.Intelligence.Models) != 2 {
		t.Fatalf("公开摘要丢失流程契约：%s", safe)
	}
	// 身份来源属于内部归因，卖家摘要不下发。
	if strings.Contains(string(safe), "identity_source") {
		t.Fatalf("卖家摘要泄露身份来源：%s", safe)
	}
	// 未跑降智流程时按模型结论为空。
	if public.Degradation.Status != "not_run" || len(public.Degradation.Models) != 0 {
		t.Fatalf("未跑的降智流程不应有模型结论：%s", safe)
	}
	for i, model := range public.Intelligence.Models {
		if len(model) != 2 || model["model"] == nil || model["status"] == nil {
			t.Fatalf("卖家模型项只能含型号与状态，第%d项=%v", i, model)
		}
	}
	if public.Checks["D07"].Factor == nil || *public.Checks["D07"].Factor != .85 || !public.Checks["D07"].Applied || public.Checks["A02"].Factor != nil {
		t.Fatalf("卖家生效系数与候选系数边界错误：%+v", public.Checks)
	}
	for _, secret := range []string{"target_pass_rate", "expected_pass_rate", "内部指标", ".987654", ".912345", ".923456", ".654321", ".812345"} {
		if strings.Contains(string(safe), secret) {
			t.Fatalf("卖家摘要泄露 %s：%s", secret, safe)
		}
	}
}
