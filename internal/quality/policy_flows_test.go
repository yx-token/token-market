package quality

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func policyFlowsFixture() PolicyFlows {
	return PolicyFlows{
		Intelligence: IntelligenceFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, ScheduledEnabled: true,
			IntervalMinutes: 360, Triggers: []string{"onboarding", "scheduled", "admin"}, FullTriggers: []string{"onboarding", "admin"}},
			Models: []IntelligenceModel{{Model: "gpt-6-astra"}}},
		Degradation: IntelligenceFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true,
			IntervalMinutes: 1440, Triggers: []string{"onboarding", "scheduled", "admin"}, FullTriggers: []string{"onboarding", "admin"}},
			Models: []IntelligenceModel{{Model: "gpt-6.1-sol"}}},
		Identity: IdentityFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, IntervalMinutes: 1440,
			Triggers: []string{"onboarding", "admin"}, FullTriggers: []string{"onboarding", "admin"}}, Source: "third_party"},
	}
}

func TestPolicyFlowsJSONContract(t *testing.T) {
	flows := policyFlowsFixture()
	rate := 0.9
	flows.Intelligence.Models = append(flows.Intelligence.Models, IntelligenceModel{Model: "gpt-6.1-sol", ExpectedPassRate: &rate})
	raw, err := json.Marshal(PolicyInput{Flows: &flows})
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	var got PolicyFlows
	if err := json.Unmarshal(data["flows"], &got); err != nil || !reflect.DeepEqual(got, flows) {
		t.Fatalf("流程配置未按接口约定往返：%+v %v", got, err)
	}
	var nested map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data["flows"], &nested); err != nil {
		t.Fatal(err)
	}
	if string(nested["intelligence"]["enabled"]) != "true" || string(nested["identity"]["source"]) != `"third_party"` {
		t.Fatalf("共用配置字段没有展开到流程内：%s", data["flows"])
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(nested["intelligence"]["models"], &models); err != nil || string(models[0]["expected_pass_rate"]) != "null" {
		t.Fatalf("留空期望通过率应返回 null：%s %v", nested["intelligence"]["models"], err)
	}
}

// 加第三条流程前保存的旧调用方只带测智与身份，降智必须继承测智而不是被校验拒绝。
func TestPolicyFlowsLegacyClientInheritsDegradation(t *testing.T) {
	legacy := []byte(`{"intelligence":{"enabled":true,"scheduled_enabled":true,"interval_minutes":360,
		"triggers":["onboarding","scheduled"],"full_triggers":["onboarding"],
		"models":[{"model":"gpt-6-astra","expected_pass_rate":0.8}]},
		"identity":{"enabled":true,"source":"third_party","scheduled_enabled":false,"interval_minutes":1440,
		"triggers":["onboarding"],"full_triggers":["onboarding"]}}`)
	var flows PolicyFlows
	if err := json.Unmarshal(legacy, &flows); err != nil {
		t.Fatal(err)
	}
	flows.backfillDegradation()
	if err := flows.normalize([]ThirdPartyCheck{{Provider: "bazaarlink"}}); err != nil {
		t.Fatalf("旧调用方保存策略不应被拒绝：%v", err)
	}
	if !flows.Degradation.Enabled || flows.Degradation.ScheduledEnabled || flows.Degradation.IntervalMinutes != 360 ||
		!reflect.DeepEqual(flows.Degradation.Triggers, []string{"onboarding", "scheduled"}) ||
		!reflect.DeepEqual(flows.Degradation.FullTriggers, []string{"onboarding"}) || len(flows.Degradation.Models) != 1 ||
		flows.Degradation.Models[0].Model != "gpt-6-astra" || flows.Degradation.Models[0].ExpectedPassRate == nil {
		t.Fatalf("降智应继承测智模型与周期：%+v", flows.Degradation)
	}
}

func TestPolicyFlowsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PolicyFlows)
		sites  []ThirdPartyCheck
		valid  bool
	}{
		{name: "标准第三方", sites: []ThirdPartyCheck{{Provider: "bazaarlink"}}, valid: true},
		{name: "自有身份无需检测站", change: func(f *PolicyFlows) { f.Identity.Source = "self" }, valid: true},
		{name: "关闭流程可以不配模型", change: func(f *PolicyFlows) {
			f.Intelligence.Enabled, f.Intelligence.ScheduledEnabled, f.Identity.Enabled = false, false, false
			f.Intelligence.Models = nil
		}, valid: true},
		{name: "第三方缺少检测站"},
		{name: "无效来源", sites: []ThirdPartyCheck{{Provider: "bazaarlink"}}, change: func(f *PolicyFlows) { f.Identity.Source = "legacy" }},
		{name: "周期过短", change: func(f *PolicyFlows) { f.Intelligence.IntervalMinutes = 9 }},
		{name: "关闭仍开定时", change: func(f *PolicyFlows) { f.Intelligence.Enabled = false }},
		{name: "未知触发类型", change: func(f *PolicyFlows) { f.Intelligence.Triggers = []string{"reference"} }},
		{name: "完整触发类型重复", change: func(f *PolicyFlows) { f.Identity.FullTriggers = []string{"admin", "admin"} }},
		{name: "启用无模型", change: func(f *PolicyFlows) { f.Intelligence.Models = nil }},
		{name: "空模型", change: func(f *PolicyFlows) { f.Intelligence.Models[0].Model = " " }},
		{name: "重复模型", change: func(f *PolicyFlows) {
			f.Intelligence.Models = append(f.Intelligence.Models, IntelligenceModel{Model: " gpt-6-astra "})
		}},
		{name: "期望通过率为零", change: func(f *PolicyFlows) { rate := 0.0; f.Intelligence.Models[0].ExpectedPassRate = &rate }},
		{name: "期望通过率超过一", change: func(f *PolicyFlows) { rate := 1.1; f.Intelligence.Models[0].ExpectedPassRate = &rate }},
		{name: "期望通过率非数字", change: func(f *PolicyFlows) { rate := math.NaN(); f.Intelligence.Models[0].ExpectedPassRate = &rate }},
		{name: "期望通过率为一", sites: []ThirdPartyCheck{{Provider: "bazaarlink"}}, valid: true,
			change: func(f *PolicyFlows) { rate := 1.0; f.Intelligence.Models[0].ExpectedPassRate = &rate }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flows := policyFlowsFixture()
			if tc.change != nil {
				tc.change(&flows)
			}
			sites := tc.sites
			if sites == nil && tc.name != "第三方缺少检测站" {
				sites = []ThirdPartyCheck{{Provider: "bazaarlink"}}
			}
			if err := flows.normalize(sites); (err == nil) != tc.valid {
				t.Fatalf("校验结果=%v，期望通过=%t", err, tc.valid)
			}
		})
	}
}

func TestPolicyFlowsLegacyMapping(t *testing.T) {
	p := Policy{Enabled: true, ScheduledEnabled: true, IntervalMinutes: 90, Models: []string{"first", "second"},
		FullTriggers: []string{"appeal"}, ThirdParty: []ThirdPartyCheck{
			{Provider: "bazaarlink", Triggers: []string{"admin", "scheduled"}},
			{Provider: "other", Triggers: []string{"admin", "appeal"}},
		}}
	flows := legacyPolicyFlows(&p)
	if !flows.Intelligence.Enabled || !flows.Intelligence.ScheduledEnabled || flows.Intelligence.IntervalMinutes != 90 ||
		!reflect.DeepEqual(flows.Intelligence.FullTriggers, p.FullTriggers) || len(flows.Intelligence.Models) != 2 ||
		flows.Intelligence.Models[1].Model != "second" || flows.Intelligence.Models[0].ExpectedPassRate != nil {
		t.Fatalf("测智没有保留旧配置：%+v", flows.Intelligence)
	}
	if !flows.Identity.Enabled || !flows.Identity.ScheduledEnabled || flows.Identity.Source != "third_party" ||
		!reflect.DeepEqual(flows.Identity.Triggers, []string{"scheduled", "admin", "appeal"}) {
		t.Fatalf("身份没有合并旧检测站触发类型：%+v", flows.Identity)
	}
	if !flows.Intelligence.Applies("scheduled") || flows.Intelligence.Full("scheduled") || !flows.Intelligence.Full("appeal") {
		t.Fatalf("流程触发或模式判定错误：%+v", flows.Intelligence)
	}
	flows.Intelligence.Enabled = false
	if flows.Intelligence.Applies("admin") {
		t.Fatal("关闭的流程不应响应触发")
	}
}

func TestPolicyFlowsDeprecatedThirdPartyTriggersOptional(t *testing.T) {
	site := ThirdPartyCheck{Provider: "bazaarlink"}
	if err := site.normalize(); err != nil || site.OnMismatch != "degraded" || site.OnSubmodelMismatch != "suspected" {
		t.Fatalf("检测站配置不应再要求旧 triggers：%+v %v", site, err)
	}
}
