package inspection

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCodingOAuthCapabilityCompatibility(t *testing.T) {
	for _, platform := range []string{"kimi", "zhipu"} {
		capability := ModelCapability{Platform: platform, ContextWindow: 100000,
			ContextWindowByAuth: map[string]int{"coding": 200000, "coding-intl": 300000}}
		if capability.ContextForAuth("oauth") != 200000 || capability.ContextForAuth("oauth-intl") != 300000 {
			t.Fatalf("OAuth did not inherit Coding Plan capability: %+v", capability)
		}
		capability.ContextWindowByAuth["oauth-intl"] = 400000
		if capability.ContextForAuth("oauth-intl") != 400000 {
			t.Fatal("explicit OAuth capability did not take precedence")
		}
	}
	if _, err := Update(context.Background(), &memoryDB{}, []byte(`{"model_capabilities":[{"platform":"zhipu","model":"*","context_window_by_auth":{"oauth-intl":200000}}]}`)); err != nil {
		t.Fatalf("international OAuth capability rejected: %v", err)
	}
}

func TestCapabilityDefaultsOfficialFacts(t *testing.T) {
	defaults := CapabilityDefaults()
	if len(defaults) < 40 {
		t.Fatal("未覆盖主力型号")
	}
	for _, c := range defaults {
		if c.Source == "" || c.ContextWindow <= 0 || c.Thinking == nil || c.Tools != nil || c.JSONSchema != nil || c.Image != nil || c.PDF != nil || c.MultiTurn != nil {
			t.Fatalf("缺少官方事实或臆测功能：%+v", c)
		}
	}
	c := LookupCapability("openai", "gpt-6-astra", Defaults())
	if c.ContextWindow != 1050000 || c.ContextForAuth("oauth") != 272000 || c.ContextForAuth("apikey") != 1050000 || c.MaxOutput != 128000 {
		t.Fatalf("Astra 官方窗口错误：%+v", c)
	}
	defaults[0].Thinking = capabilityBool(false)
	defaults[0].Efforts[0] = "未知"
	if c := CapabilityDefaults()[0]; !*c.Thinking || c.Efforts[0] == "未知" {
		t.Fatal("调用方改写内置能力表")
	}
	unknown := LookupCapability("openai", "未收录型号", Defaults())
	if unknown.Tools != nil || unknown.Thinking != nil || unknown.ContextWindow != 0 {
		t.Fatal("未登记型号被推断为已支持")
	}
}

func TestCapabilityLookupPrecedenceAndPartialMerge(t *testing.T) {
	db := &memoryDB{raw: []byte(`{"model_capabilities":[
		{"platform":"openai","model":"gpt-6-astra","tools":false,"context_window_by_auth":{"oauth":300000}},
		{"platform":"openai","model":"*","tools":true,"pdf":false},
		{"platform":"openai","model":"gpt-6-*","json_schema":true},
		{"platform":"openai","model":"gpt-6-a*","image":true}
	]}`)}
	st, err := Load(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	c := LookupCapability("openai", "gpt-6-astra", st)
	if c.Tools == nil || *c.Tools || c.JSONSchema == nil || !*c.JSONSchema || c.Image == nil || !*c.Image || c.PDF == nil || *c.PDF || c.MaxOutput != 128000 || c.ContextForAuth("oauth") != 300000 {
		t.Fatalf("精确、最长前缀、平台默认或 auth 合并错误：%+v", c)
	}
	if c := LookupCapability("openai", "其他型号", st); c.Tools == nil || !*c.Tools || c.JSONSchema != nil {
		t.Fatal("平台默认匹配错误")
	}
	if c := LookupCapability("gemini", "gpt-6-astra", st); c.Tools != nil {
		t.Fatal("跨平台覆盖污染")
	}
	if c := LookupCapability("openai", "gpt-6-astra", Defaults()); c.ContextForAuth("oauth") != 272000 {
		t.Fatal("查找改写了内置 auth 默认")
	}
}

func TestCapabilitySettingsRoundTripNullAndLegacy(t *testing.T) {
	db := &memoryDB{raw: []byte(`{"mode":"trial","future":{"keep":true}}`)}
	st, err := Load(context.Background(), db)
	if err != nil || st.ModelCapabilities == nil || st.TokenBudgets["full"].Input != 2000000 {
		t.Fatalf("旧设置未合并新增默认：%+v %v", st, err)
	}
	st, err = Update(context.Background(), db, []byte(`{"model_capabilities":[{"platform":"openai","model":"gpt-6-astra","thinking":null,"tools":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c := LookupCapability("openai", "gpt-6-astra", st); c.Thinking != nil || c.Tools == nil || *c.Tools || c.MaxOutput != 128000 {
		t.Fatalf("未登记、不支持与未传混淆：%+v", c)
	}
	st, err = Update(context.Background(), db, []byte(`{"model_capabilities":[]}`))
	if err != nil || LookupCapability("openai", "gpt-6-astra", st).Thinking == nil {
		t.Fatal("删除覆盖不能恢复默认")
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(db.raw, &saved)
	if string(saved["future"]) != `{"keep":true}` {
		t.Fatal("旧兼容字段丢失")
	}
}

func TestCapabilityOverrideValidation(t *testing.T) {
	for _, body := range []string{
		`{"platform":"unknown","model":"gpt-6-astra"}`, `{"platform":"","model":"x"}`,
		`{"platform":"openai","model":""}`, `{"platform":"openai","model":"unknown"}`,
		`{"platform":"openai","model":"gpt*6"}`, `{"platform":"openai","model":"gpt-6-**"}`,
		`{"platform":"openai","model":"x","context_window":-1}`, `{"platform":"openai","model":"x","max_output":10000001}`,
		`{"platform":"openai","model":"x","tools":"unknown"}`, `{"platform":"openai","model":"x","tool":true}`,
		`{"platform":"openai","model":"x","context_window_by_auth":{"unknown":1}}`,
		`{"platform":"openai","model":"x","context_window_by_auth":{"oauth":-1}}`,
		`{"platform":"openai","model":"x","efforts":["unknown"]}`,
		`null`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := Update(context.Background(), &memoryDB{}, []byte(`{"model_capabilities":[`+body+`]}`)); err == nil {
				t.Fatal("无效能力覆盖被接受")
			}
		})
	}
	if _, err := Update(context.Background(), &memoryDB{}, []byte(`{"model_capabilities":[{"platform":"openai","model":"*"},{"platform":"openai","model":"*"}]}`)); err == nil {
		t.Fatal("重复型号被接受")
	}
}

func TestCapabilityReadOnlyDefaultsAndThresholdValidation(t *testing.T) {
	db := &memoryDB{}
	if _, err := Update(context.Background(), db, []byte(`{"model_capability_defaults":[{"platform":"openai","model":"*","thinking":false}]}`)); err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(db.raw, &saved)
	if _, present := saved["model_capability_defaults"]; present {
		t.Fatal("只读默认表被保存")
	}
	for _, patch := range []string{
		`{"check_overrides":{"D07":{"thresholds":{"expected_pass_rate":0}}}}`,
		`{"check_overrides":{"D09":{"thresholds":{"expected_pass_rate":1.1}}}}`,
		`{"check_overrides":{"D07.honesty":{"thresholds":{"expected_pass_rate":-1}}}}`,
		`{"check_overrides":{"D06":{"thresholds":{"absolute_high_low_ratio":0}}}}`,
	} {
		if _, err := Update(context.Background(), &memoryDB{}, []byte(patch)); err == nil {
			t.Fatalf("无效新阈值被接受：%s", patch)
		}
	}
	if _, err := Update(context.Background(), &memoryDB{}, []byte(`{"check_overrides":{"D06":{"thresholds":{"absolute_high_low_ratio":1.3}}}}`)); err != nil {
		t.Fatalf("自身档位倍率不能按比例拒绝：%v", err)
	}
}
