package fakesub2api

import (
	"reflect"
	"testing"
)

func TestAccountModelWhitelistFiltering(t *testing.T) {
	for _, tt := range []struct {
		name, credentials, model string
		want                     bool
	}{
		{"未声明白名单", `{}`, "任意模型", true},
		{"空映射", `{"model_mapping":{}}`, "任意模型", true},
		{"白名单内", `{"model_mapping":{"gpt-6-astra":"gpt-6-astra"}}`, "gpt-6-astra", true},
		{"白名单外", `{"model_mapping":{"gpt-6-astra":"gpt-6-astra"}}`, "gpt-6.1-sol", false},
		{"精确键不应按前缀匹配", `{"model_mapping":{"gpt-6":"gpt-6"}}`, "gpt-6-astra", false},
		{"通配符前缀", `{"model_mapping":{"gpt-*":"gpt-6"}}`, "gpt-6-astra", true},
		{"通配符后缀", `{"model_mapping":{"*-astra":"gpt-6"}}`, "gpt-6-astra", true},
		{"通配符其他家族", `{"model_mapping":{"gpt-*":"gpt-6"}}`, "claude-sonnet-4-5", false},
		{"通配符全量", `{"model_mapping":{"*":"gpt-6"}}`, "任意模型", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportsAccountModel([]byte(tt.credentials), tt.model); got != tt.want {
				t.Fatalf("模型 %s 是否允许 = %v，期望 %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestAccountModelCatalogConfiguration(t *testing.T) {
	s := NewUnstarted(nil, "test-key")
	models := []string{"模型一", "模型二"}
	s.SetAccountModels(0, models)
	models[0] = "调用方改动"
	got, err := s.accountModelCatalog(42, true)
	if err != nil || !reflect.DeepEqual(got, []string{"模型一", "模型二"}) {
		t.Fatalf("默认目录配置 = %v，错误 %v", got, err)
	}
	got[0] = "返回值改动"
	s.SetAccountModels(42, []string{})
	got, err = s.accountModelCatalog(42, false)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("显式空目录应保留：%v，错误 %v", got, err)
	}
	s.FailAccountModelsSync(0, 502)
	if _, err := s.accountModelCatalog(42, true); err == nil {
		t.Fatal("默认同步故障未生效")
	}
	s.FailAccountModelsSync(42, 0)
	if _, err := s.accountModelCatalog(42, true); err != nil {
		t.Fatalf("账号同步恢复未生效：%v", err)
	}
	s.FailAccountModelsQuery(42, 503)
	if _, err := s.accountModelCatalog(42, false); err == nil {
		t.Fatal("查询故障未生效")
	}
	if syncCount, queryCount := s.AccountModelRequests(42); syncCount != 3 || queryCount != 2 {
		t.Fatalf("目录请求次数 = %d / %d", syncCount, queryCount)
	}
}

func TestPutAccountCredentialsPreservesSecretsAndClearsWhitelist(t *testing.T) {
	existing := map[string]any{
		"access_token": "旧令牌", "refresh_token": "刷新令牌", "api_key": "原密钥", "base_url": "旧地址",
		"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"},
	}
	incoming := map[string]any{"access_token": "新令牌", "base_url": "新地址"}
	got := mergeSensitiveAccountCredentials(existing, incoming)
	want := map[string]any{"access_token": "新令牌", "refresh_token": "刷新令牌", "api_key": "原密钥", "base_url": "新地址"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PUT 合并语义不符：%#v", got)
	}
	if existing["access_token"] != "旧令牌" || incoming["refresh_token"] != nil {
		t.Fatal("合并不应修改输入对象")
	}
}
