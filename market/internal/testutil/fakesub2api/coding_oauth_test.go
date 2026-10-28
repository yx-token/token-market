package fakesub2api

import (
	"context"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestCodingOAuthEndpoints(t *testing.T) {
	server := New(nil, "test-key")
	defer server.Close()
	client := adminclient.New(server.URL(), "test-key", time.Second)
	ctx := context.Background()
	for _, test := range []struct{ platform, provider, start string }{
		{"kimi", "", "device-code"}, {"zhipu", "bigmodel", "init"}, {"zhipu", "zai", "init"},
	} {
		t.Run(test.platform+"/"+test.provider, func(t *testing.T) {
			path := "/api/v1/admin/" + test.platform + "/oauth/"
			var start map[string]any
			if err := client.Post(ctx, path+test.start, map[string]any{"provider": test.provider, "proxy_id": 55}, "", &start); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"session_id": start["session_id"], "proxy_id": 66}
			var result map[string]any
			if err := client.Post(ctx, path+"poll", body, "", &result); err == nil {
				t.Fatal("fake did not enforce proxy consistency")
			}
			body["proxy_id"] = 55
			for _, status := range []string{"pending", "success"} {
				if err := client.Post(ctx, path+"poll", body, "", &result); err != nil || result["status"] != status {
					t.Fatalf("fake poll want %s: %+v %v", status, result, err)
				}
			}
			if test.platform == "kimi" {
				if result["token_info"].(map[string]any)["device_id"] == "" {
					t.Fatal("fake omitted Kimi token metadata")
				}
			} else {
				body["callback_url"] = "zcode://oauth/callback?authCode=test"
				if err := client.Post(ctx, path+"exchange-code", body, "", &result); err != nil || result["provider"] != test.provider {
					t.Fatalf("fake callback exchange: %+v %v", result, err)
				}
			}
		})
	}
}
