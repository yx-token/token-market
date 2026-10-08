//go:build integration

package e2e

import (
	"fmt"
	"testing"
)

// TestSellerUsageAndQuota：卖家能看到自己上游经平台产生的用量（汇总、每日、按模型、逐次记录）与官方订阅窗口用量，
// 看不到其他卖家的数据，也看不到买家身份。
func TestSellerUsageAndQuota(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	admin.must("PUT", "/api/admin/vendors/kimi", map[string]any{"enabled": true}, nil)
	for i := 1; i <= 3; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.9", "port": 7080 + i}, nil)
	}

	seller := e.newUser("usage-seller@market.test")
	var up idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": "usage-acct"}, nil)
	e.passOnboarding(up.ID)
	var created struct {
		Listing struct {
			ID int64 `json:"id"`
		} `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5,
		"join_default_pool": false}, &created)

	buyer := e.newUser("usage-buyer@market.test")
	var rc struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 10}, &rc)
	admin.must("POST", fmt.Sprintf("/api/admin/recharges/%d/confirm", rc.Order.ID), map[string]string{"provider_ref": "bank-u"}, nil)
	e.settle()
	var key struct {
		Key string `json:"key"`
	}
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "k", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"listing_id": created.Listing.ID}}}}, &key)
	e.schedule()
	ed := e.startEdge()
	for i := 0; i < 3; i++ {
		if status, _ := ed.call(key.Key); status != 200 {
			t.Fatalf("buyer call %d: %d", i, status)
		}
	}
	ed.flushAttempts()
	e.ingestAll()

	type totals struct {
		Requests      int64   `json:"requests"`
		BuyerRequests int64   `json:"buyer_requests"`
		ProbeRequests int64   `json:"probe_requests"`
		InputTokens   int64   `json:"input_tokens"`
		OutputTokens  int64   `json:"output_tokens"`
		TotalCost     float64 `json:"total_cost"`
		Payout        float64 `json:"payout"`
	}
	var usage struct {
		Totals totals `json:"totals"`
		Today  totals `json:"today"`
		Daily  []struct {
			Day string `json:"day"`
			totals
		} `json:"daily"`
		Models []struct {
			Model string `json:"model"`
			totals
		} `json:"models"`
		Upstreams []struct {
			UpstreamID int64 `json:"upstream_id"`
			totals
		} `json:"upstreams"`
	}
	seller.must("GET", "/api/seller/usage?days=7", nil, &usage)
	tt := usage.Totals
	if tt.BuyerRequests != 3 || tt.ProbeRequests < 1 || tt.Requests != tt.BuyerRequests+tt.ProbeRequests || tt.InputTokens <= 0 ||
		tt.Payout <= 0 || tt.TotalCost <= 0 || usage.Today.Requests != tt.Requests {
		t.Fatalf("usage totals: %+v today %+v", tt, usage.Today)
	}
	if len(usage.Daily) != 7 || usage.Daily[6].Requests != tt.Requests || usage.Daily[0].Requests != 0 {
		t.Fatalf("daily series: %+v", usage.Daily)
	}
	if len(usage.Models) == 0 || usage.Models[0].Model != "claude-sonnet-4-5" || len(usage.Upstreams) != 1 || usage.Upstreams[0].UpstreamID != up.ID {
		t.Fatalf("models / upstreams: %+v %+v", usage.Models, usage.Upstreams)
	}
	var recs []map[string]any
	seller.must("GET", fmt.Sprintf("/api/seller/usage/records?upstream_id=%d&channel=buyer", up.ID), nil, &recs)
	if len(recs) != 3 || recs[0]["channel"] != "direct" {
		t.Fatalf("buyer records: %+v", recs)
	}
	for _, r := range recs {
		for _, k := range []string{"buyer_user_id", "buyer_key_id", "sub2api_request_id"} {
			if _, ok := r[k]; ok {
				t.Fatalf("seller must not see %s: %+v", k, r)
			}
		}
	}
	seller.expect(400, "GET", "/api/seller/usage/records?channel=nope", nil)

	// 官方订阅窗口：Claude 读被动快照
	type quota struct {
		Supported   bool   `json:"supported"`
		Source      string `json:"source"`
		SelfReserve *int   `json:"self_reserve_percent"`
		Windows     []struct {
			Key         string  `json:"key"`
			Utilization float64 `json:"utilization"`
			Requests    *int64  `json:"requests"`
		} `json:"windows"`
	}
	var q quota
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/quota", up.ID), nil, &q)
	if !q.Supported || q.Source != "passive" || q.SelfReserve == nil || len(q.Windows) != 2 || q.Windows[0].Key != "five_hour" ||
		q.Windows[0].Requests == nil || *q.Windows[0].Requests != tt.Requests || q.Windows[0].Utilization <= 0 {
		t.Fatalf("anthropic quota: %+v", q)
	}
	// 刷新：向官方查询
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/quota?refresh=1", up.ID), nil, &q)
	if q.Source != "active" {
		t.Fatalf("refresh should query actively: %+v", q)
	}

	// Coding Plan：读各家的配额接口
	var kimi idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "kimi", "auth_type": "coding"}, &kimi)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", kimi.ID), map[string]string{"api_key": "sk-kimi-usage-test-key-0001"}, nil)
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/quota", kimi.ID), nil, &q)
	if !q.Supported || q.Source != "coding_plan" || len(q.Windows) != 1 || q.Windows[0].Key != "cn:5h" {
		t.Fatalf("coding plan quota: %+v", q)
	}

	// 其他卖家看不到
	other := e.newUser("usage-other@market.test")
	other.expect(404, "GET", fmt.Sprintf("/api/seller/upstreams/%d/quota", up.ID), nil)
	other.expect(404, "GET", fmt.Sprintf("/api/seller/usage?upstream_id=%d", up.ID), nil)
	var none []map[string]any
	other.must("GET", fmt.Sprintf("/api/seller/usage/records?upstream_id=%d", up.ID), nil, &none)
	if len(none) != 0 {
		t.Fatalf("other seller sees records: %+v", none)
	}
}
