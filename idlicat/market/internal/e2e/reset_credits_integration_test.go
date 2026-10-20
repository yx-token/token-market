//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 重置卡：卖家手动用卡（Codex、Claude）与防重复消耗；授权平台自动用卡后调度器按挂单状态下发配置，
// 并把 sub2api 的自动用卡审计记录同步为卖家可见的用卡记录。
func TestResetCredits(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	for _, p := range []string{"anthropic", "openai"} {
		admin.must("PUT", "/api/admin/vendors/"+p, map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	}
	for i := 1; i <= 3; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "http", "host": "10.0.2.1", "port": 4100 + i, "region": "us"}, nil)
	}
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-gpt", "platform": "openai", "sell_multiplier": 0.7}, nil)

	seller := e.newUser("reset@market.test")
	var up idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "openai", "auth_type": "oauth"}, &up)
	var start struct {
		AuthURL string `json:"auth_url"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, &start)
	state := start.AuthURL[len("https://auth.example.test/authorize?state="):]
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": "https://cb.test/cb?code=rita&state=" + state}, nil)
	e.passOnboarding(up.ID)
	var l struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.3, "categories": []string{"coding"}}, &l)
	e.schedule()
	var acc int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up.ID), &acc)

	base := fmt.Sprintf("/api/seller/upstreams/%d", up.ID)
	var detail struct {
		ResetCredits bool `json:"reset_credits_supported"`
		AutoReset    bool `json:"auto_reset_supported"`
	}
	seller.must("GET", base, nil, &detail)
	if !detail.ResetCredits || !detail.AutoReset {
		t.Fatalf("codex oauth should support reset credits: %+v", detail)
	}

	type event struct {
		ID      int64    `json:"id"`
		Trigger string   `json:"trigger"`
		Status  string   `json:"status"`
		Windows []string `json:"windows"`
	}
	type credits struct {
		Queried        bool `json:"queried"`
		AvailableCount int  `json:"available_count"`
		Auto           *struct {
			Enabled     bool `json:"enabled"`
			Active      bool `json:"active"`
			Effective5h int  `json:"effective_5h_percent"`
			Effective7d int  `json:"effective_7d_percent"`
			PauseLine   int  `json:"pause_line_percent"`
		} `json:"auto"`
		Events []event `json:"events"`
	}
	redeem := func(c *client, path, requestID string, expected int) (int, string) {
		return c.call("POST", path+"/reset-credits/redeem", map[string]any{"request_id": requestID, "expected_available": expected}, nil)
	}

	// ---- Codex 手动用卡 ----
	var rc credits
	seller.must("GET", base+"/reset-credits?refresh=1", nil, &rc)
	if !rc.Queried || rc.AvailableCount != 0 || rc.Auto == nil || rc.Auto.Enabled {
		t.Fatalf("fresh codex account: %+v", rc)
	}
	if status, raw := redeem(seller, base, "req-none-0001", 0); status != 409 || !strings.Contains(raw, "NO_RESET_CREDIT") {
		t.Fatalf("redeem without credits: %d %s", status, raw)
	}

	e.fake.SetResetCredits(acc, 2)
	// 卖家确认时看到的张数与官方不一致：拒绝且不消耗
	if status, raw := redeem(seller, base, "req-stale-0001", 1); status != 409 || !strings.Contains(raw, "RESET_CREDITS_CHANGED") {
		t.Fatalf("stale expected count: %d %s", status, raw)
	}
	if n := e.fake.ResetsUsed(acc); n != 0 {
		t.Fatalf("rejected redeem must not consume a credit: %d", n)
	}
	var ev event
	seller.must("POST", base+"/reset-credits/redeem", map[string]any{"request_id": "req-ok-000001", "expected_available": 2}, &ev)
	if ev.Status != "success" || ev.Trigger != "manual" {
		t.Fatalf("redeem: %+v", ev)
	}
	// 同一请求号重试返回首次结果，不再消耗
	var again event
	seller.must("POST", base+"/reset-credits/redeem", map[string]any{"request_id": "req-ok-000001", "expected_available": 2}, &again)
	if again.ID != ev.ID || e.fake.ResetsUsed(acc) != 1 {
		t.Fatalf("retry must replay: %+v used=%d", again, e.fake.ResetsUsed(acc))
	}
	// 用卡失败前的拒绝不留记录；没刷新时读 sub2api 缓存的快照
	seller.must("GET", base+"/reset-credits", nil, &rc)
	if rc.AvailableCount != 1 || len(rc.Events) != 1 {
		t.Fatalf("after redeem: %+v", rc)
	}
	// 其他卖家不能操作
	other := e.newUser("other@market.test")
	if status, _ := redeem(other, base, "req-other-001", 1); status != 404 {
		t.Fatalf("other seller should get 404, got %d", status)
	}

	// ---- 授权自动用卡 ----
	seller.must("PUT", base+"/settings", map[string]any{"self_reserve_percent": 20}, nil)
	seller.expect(400, "PUT", base+"/auto-reset", map[string]any{"enabled": true, "threshold_5h_percent": 90})
	seller.must("PUT", base+"/auto-reset", map[string]any{"enabled": true, "threshold_7d_percent": 60}, nil)
	e.schedule()
	extraOf := func() (map[string]any, map[string]any) {
		var extra, creds []byte
		mustScan(t, e.replica.QueryRow(ctx, "SELECT COALESCE(extra, '{}'), COALESCE(credentials, '{}') FROM accounts WHERE id = $1", acc), &extra, &creds)
		var x, c map[string]any
		_ = json.Unmarshal(extra, &x)
		_ = json.Unmarshal(creds, &c)
		return x, c
	}
	x, c := extraOf()
	if x["auto_reset_credit_enabled"] != true || x["auto_reset_credit_5h_threshold"] != 0.8 || x["auto_reset_credit_7d_threshold"] != 0.6 ||
		c["account_scheduling_threshold"] != float64(100) {
		t.Fatalf("auto reset should be on with window threshold disabled: extra=%v creds=%v", x, c)
	}
	if x["codex_reset_credit_snapshot"] == nil {
		t.Fatalf("writing auto reset config must keep other extra keys: %v", x)
	}
	seller.must("GET", base+"/reset-credits", nil, &rc)
	if rc.Auto == nil || !rc.Auto.Enabled || !rc.Auto.Active || rc.Auto.Effective5h != 80 || rc.Auto.Effective7d != 60 || rc.Auto.PauseLine != 80 {
		t.Fatalf("auto view: %+v", rc.Auto)
	}

	// 挂单暂停：不卖就不用卡，恢复暂停线
	seller.must("POST", fmt.Sprintf("/api/seller/listings/%d/pause", l.Listing.ID), nil, nil)
	e.schedule()
	if x, c := extraOf(); x["auto_reset_credit_enabled"] != false || c["account_scheduling_threshold"] != float64(80) {
		t.Fatalf("paused listing should turn auto reset off: extra=%v creds=%v", x, c)
	}
	seller.must("POST", fmt.Sprintf("/api/seller/listings/%d/resume", l.Listing.ID), nil, nil)
	e.schedule()
	if x, _ := extraOf(); x["auto_reset_credit_enabled"] != true {
		t.Fatalf("resumed listing should turn auto reset back on: %v", x)
	}

	// sub2api 的自动用卡审计记录同步为用卡记录（按审计 ID 幂等，不属于本系统的账号忽略）
	for _, a := range []map[string]any{
		{"account_id": acc, "result_code": "success", "trigger_window": "5h+7d", "windows_reset": 2},
		{"account_id": acc + 1000, "result_code": "success", "trigger_window": "7d"},
	} {
		raw, _ := json.Marshal(a)
		if _, err := e.replica.Exec(ctx, "INSERT INTO audit_logs (action, extra) VALUES ('system.openai.reset_credit.auto', $1)", raw); err != nil {
			t.Fatal(err)
		}
	}
	e.schedule()
	e.schedule()
	seller.must("GET", base+"/reset-credits", nil, &rc)
	var autos []event
	for _, x := range rc.Events {
		if x.Trigger == "auto" {
			autos = append(autos, x)
		}
	}
	if len(autos) != 1 || autos[0].Status != "success" || strings.Join(autos[0].Windows, ",") != "5h,7d" {
		t.Fatalf("auto reset events: %+v", rc.Events)
	}

	// 取消授权
	seller.must("PUT", base+"/auto-reset", map[string]any{"enabled": false}, nil)
	e.schedule()
	if x, c := extraOf(); x["auto_reset_credit_enabled"] != false || c["account_scheduling_threshold"] != float64(80) {
		t.Fatalf("revoked auto reset: extra=%v creds=%v", x, c)
	}

	// ---- Claude：只能手动用卡，用卡后恢复账号状态，并延时重新同步用量 ----
	var cu idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &cu)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", cu.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", cu.ID), map[string]string{"code": "https://cb.test/callback?code=rosa&state=x"}, nil)
	e.passOnboarding(cu.ID)
	var cacc int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", cu.ID), &cacc)
	cbase := fmt.Sprintf("/api/seller/upstreams/%d", cu.ID)
	seller.expect(400, "PUT", cbase+"/auto-reset", map[string]any{"enabled": true})

	e.fake.SetResetCredits(cacc, 1)
	rc = credits{}
	seller.must("GET", cbase+"/reset-credits", nil, &rc)
	if rc.Queried || rc.Auto != nil {
		t.Fatalf("claude credits are only queried on demand: %+v", rc)
	}
	seller.must("GET", cbase+"/reset-credits?refresh=1", nil, &rc)
	if !rc.Queried || rc.AvailableCount != 1 {
		t.Fatalf("claude credits: %+v", rc)
	}
	seller.must("POST", cbase+"/reset-credits/redeem", map[string]any{"request_id": "claude-req-0001", "expected_available": 1}, &ev)
	if ev.Status != "success" || strings.Join(ev.Windows, ",") != "five_hour" || e.fake.Recovers(cacc) != 1 {
		t.Fatalf("claude redeem: %+v recovers=%d", ev, e.fake.Recovers(cacc))
	}
	var notBefore time.Time
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT next_attempt_at FROM sub2api_ops WHERE op_type = 'account.resync_recover' AND status = 'pending'"), &notBefore)
	if time.Until(notBefore) < 3*time.Minute {
		t.Fatalf("resync should wait for sub2api's usage cache to expire: %v", notBefore)
	}
	if _, err := e.app.DB.Exec(ctx, "UPDATE sub2api_ops SET next_attempt_at = now() WHERE op_type = 'account.resync_recover'"); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if n := e.fake.Recovers(cacc); n != 2 {
		t.Fatalf("delayed resync should recover the account again: %d", n)
	}
}
