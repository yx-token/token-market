package listing

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestValidateModelWhitelist(t *testing.T) {
	for _, test := range []struct {
		name    string
		models  []string
		catalog []string
		want    []string
		code    string
	}{
		{"未传名单", nil, []string{"m1"}, nil, "MODEL_WHITELIST_REQUIRED"},
		{"空名单", []string{}, nil, nil, "MODEL_WHITELIST_REQUIRED"},
		{"空模型", []string{" "}, nil, nil, "INVALID_MODEL"},
		{"白名单不能使用通配符", []string{"m*"}, nil, nil, "INVALID_MODEL"},
		{"白名单不能使用换行", []string{"m\n1"}, nil, nil, "INVALID_MODEL"},
		{"白名单不能使用不可见控制字符", []string{"m\x001"}, nil, nil, "INVALID_MODEL"},
		{"目录外模型", []string{"m2"}, []string{"m1"}, nil, "MODEL_NOT_IN_CATALOG"},
		{"合法子集", []string{"m2"}, []string{"m1", "m2"}, []string{"m2"}, ""},
		{"去重并保留卖家顺序", []string{" m2 ", "m2", "m1"}, []string{"m1", "m2"}, []string{"m2", "m1"}, ""},
		{"目录为空可手动输入具体模型", []string{"custom-model-2026"}, []string{}, []string{"custom-model-2026"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateModelWhitelist(test.models, test.catalog)
			if test.code != "" {
				var he *httpx.Error
				if !errors.As(err, &he) || he.Code != test.code {
					t.Fatalf("校验错误 = %v，期望 %s", err, test.code)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("校验结果 = %v，错误 = %v，期望 %v", got, err, test.want)
			}
		})
	}
}

func TestUpstreamModelsPreservesUndeclaredWhitelist(t *testing.T) {
	raw, err := json.Marshal(upstreamModels(&Upstream{ModelCatalog: []string{}}))
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]json.RawMessage
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if string(view["catalog"]) != "[]" || string(view["whitelist"]) != "null" || string(view["fetched_at"]) != "null" || string(view["detection_status"]) != `"untested"` || string(view["model_checks"]) != "[]" {
		t.Fatalf("未声明白名单与空目录必须保留各自含义：%s", raw)
	}
}

func TestModelsEditableRequiresCompletedAccountCreation(t *testing.T) {
	accountID := int64(8)
	for _, status := range []string{StatusSelectingModels, StatusVerifying, StatusVerifyFailed, StatusReady, StatusListed, StatusSuspended} {
		if !modelsEditable(&Upstream{Status: status, Sub2APIAccountID: &accountID}) {
			t.Errorf("已接入账号在状态 %s 应允许修改白名单", status)
		}
		if modelsEditable(&Upstream{Status: status}) {
			t.Errorf("状态 %s 没有底层账号时不能修改白名单", status)
		}
	}
	for _, status := range []string{StatusDraft, StatusAuthorizing, StatusFailed, StatusRemoving, StatusRemoved, ""} {
		if modelsEditable(&Upstream{Status: status, Sub2APIAccountID: &accountID}) {
			t.Errorf("接入未完成或正在删除的状态 %s 不能修改白名单", status)
		}
	}
}
