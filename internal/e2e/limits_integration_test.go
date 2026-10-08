//go:build integration

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/billing"
)

// 买家 Key 花费限额，以及风控：买卖双方关联时提现转人工审核。
func TestSpendingLimitAndRiskControls(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "p1", "protocol": "socks5", "host": "10.0.0.3", "port": 3081}, nil)
	admin.must("PUT", "/api/admin/settings", map[string]any{"withdraw_min": 0.01}, nil)

	seller := e.newUser("seller@market.test")
	var up idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": "dave"}, nil)
	e.passOnboarding(up.ID)
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5}, &created)
	e.schedule()

	buyer := e.newUser("buyer@market.test")
	var rc struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 10}, &rc)
	admin.must("POST", fmt.Sprintf("/api/admin/recharges/%d/confirm", rc.Order.ID), map[string]string{"provider_ref": "bank-1"}, nil)
	e.drain()

	var key struct {
		Key    string `json:"key"`
		Detail idOnly `json:"detail"`
	}
	buyer.must("POST", "/api/buyer/keys", map[string]any{
		"name":   "limited",
		"route":  map[string]any{"type": "chain", "hops": []map[string]any{{"listing_id": created.Listing.ID}}},
		"limits": map[string]any{"quota_usd": 0.5},
	}, &key)
	e.schedule()
	e.ingestAll() // 上架检测的探测流量
	ed := e.startEdge()
	for i := 0; i < 2; i++ { // 每次 0.6 × 0.6 = 0.36，两次超过 0.5
		if status, _ := ed.call(key.Key); status != 200 {
			t.Fatalf("call %d: %d", i, status)
		}
	}
	e.app.Ingester.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	if _, _, err := e.app.Ingester.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Limits.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ed.waitBlocked(key.Key)
	if status, _ := ed.call(key.Key); status != 429 {
		t.Fatalf("key over quota should be blocked at the edge: %d", status)
	}
	var k struct {
		LimitBlockReason *string `json:"limit_block_reason"`
	}
	buyer.must("GET", fmt.Sprintf("/api/buyer/keys/%d", key.Detail.ID), nil, &k)
	if k.LimitBlockReason == nil || *k.LimitBlockReason != "quota exhausted" {
		t.Fatalf("buyer should see why the key is blocked: %+v", k)
	}
	// 买家提高额度后立即解除
	buyer.must("PUT", fmt.Sprintf("/api/buyer/keys/%d", key.Detail.ID), map[string]any{"limits": map[string]any{"quota_usd": 5}}, nil)
	if err := e.app.Limits.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if status, _ := ed.call(key.Key); status == 200 {
			break
		} else if i > 40 {
			t.Fatalf("key should work again after raising the quota: %d", status)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 风控：测试中买卖双方来自同一 IP，且有指定卖家流量 → 关联事件
	n, err := e.app.Risk.Detect(ctx)
	if err != nil || n == 0 {
		t.Fatalf("risk detection: %d %v", n, err)
	}
	var events []struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
	}
	admin.must("GET", "/api/admin/risk-events?status=open", nil, &events)
	if len(events) == 0 || events[0].Kind != "buyer_seller_linkage" {
		t.Fatalf("risk events: %+v", events)
	}
	// 有未处理风险时提现进入人工审核
	var me idOnly
	seller.must("POST", "/api/me/kyc", map[string]string{"real_name": "Dave", "id_number": "110101199001015678"}, nil)
	seller.must("GET", "/api/me", nil, &me)
	admin.must("POST", fmt.Sprintf("/api/admin/kyc/%d/review", me.ID), map[string]any{"approve": true}, nil)
	if _, err := e.app.DB.Exec(ctx, "UPDATE users SET created_at = now() - interval '90 days' WHERE id = $1", me.ID); err != nil {
		t.Fatal(err) // 排除新卖家期的影响，只看风险事件
	}
	var wd struct {
		Status       string  `json:"status"`
		ReviewReason *string `json:"review_reason"`
	}
	if _, err := billing.Unfreeze(ctx, e.app.DB, 7, time.Now().Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	seller.must("POST", "/api/seller/withdrawals", map[string]any{"amount": 0.1, "channel": "alipay", "account_info": "d@e"}, &wd)
	if wd.Status != "pending_review" || wd.ReviewReason == nil || !strings.Contains(*wd.ReviewReason, "risk") {
		t.Fatalf("withdrawal with open risk events must be reviewed: %+v", wd)
	}
	admin.must("POST", fmt.Sprintf("/api/admin/risk-events/%d/resolve", events[0].ID), map[string]string{"status": "resolved"}, nil)
	seller.must("POST", "/api/seller/withdrawals", map[string]any{"amount": 0.05, "channel": "alipay", "account_info": "d@e"}, &wd)
	if wd.Status != "approved" {
		t.Fatalf("small withdrawal without risk should be auto approved: %+v", wd)
	}
}
