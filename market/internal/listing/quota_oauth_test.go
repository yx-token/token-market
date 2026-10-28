package listing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestCodingOAuthUsesSubscriptionQuota(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/admin/cn-providers/accounts/42/quota" {
			t.Errorf("subscription account used wrong quota endpoint: %s", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{
			"success": true, "credential_valid": true, "plan_level": "pro",
			"tiers": []map[string]any{{"window": "5h", "used_percent": 25}},
		}})
	}))
	defer engine.Close()
	service := &Service{Admin: adminclient.New(engine.URL, "key", time.Second)}
	accountID := int64(42)
	for _, test := range []struct{ platform, authType string }{
		{"kimi", "coding"}, {"kimi", "oauth"}, {"zhipu", "coding"},
		{"zhipu", "coding-intl"}, {"zhipu", "oauth"}, {"zhipu", "oauth-intl"},
	} {
		t.Run(test.platform+"/"+test.authType, func(t *testing.T) {
			quota, active := service.fetchQuota(context.Background(), &Upstream{
				Platform: test.platform, AuthType: test.authType, Sub2APIAccountID: &accountID,
			}, false)
			if !active || !quota.Supported || quota.Source != "coding_plan" || quota.PlanLevel != "pro" || len(quota.Windows) != 1 || quota.Windows[0].Utilization != 25 {
				t.Fatalf("incorrect subscription quota: %+v", quota)
			}
		})
	}
	if isCodingPlanAuthType("openai", "oauth") || isCodingPlanAuthType("kimi", "unsupported") {
		t.Fatal("non-Coding auth type classified as subscription")
	}
}
