//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/DueGin/token-market/internal/listing"
)

func TestAccountImportOnboarding(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	for _, p := range []string{"anthropic", "gemini", "openai", "kimi"} {
		admin.must("PUT", "/api/admin/vendors/"+p, map[string]bool{"enabled": true}, nil)
	}
	seller := e.newUser("import@market.test")
	other := e.newUser("import-other@market.test")
	create := func(platform, authType string, n int) listing.Upstream {
		var proxy listing.Proxy
		seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "import proxy", Protocol: "http", Host: "10.10.4.1", Port: 6000 + n}, &proxy)
		seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil, &proxy)
		var u listing.Upstream
		seller.must("POST", "/api/seller/upstreams", map[string]any{"platform": platform, "auth_type": authType, "proxy_id": proxy.ID}, &u)
		return u
	}
	u := create("anthropic", "oauth", 1)
	path := fmt.Sprintf("/api/seller/upstreams/%d/import", u.ID)
	account := map[string]any{"name": "exported account", "platform": "anthropic", "type": "oauth",
		"credentials": map[string]any{"access_token": "import-secret-access", "refresh_token": "import-secret-refresh", "base_url": "http://untrusted.test", "account_scheduling_threshold": 1},
		"extra":       map[string]any{"account_uuid": "export-account-1", "email_address": "exported@example.test", "model_mapping": map[string]any{"claude": "other"}},
		"group_ids":   []int{123}, "proxy_id": 999, "priority": 0, "concurrency": 999, "rate_multiplier": 99}
	body := map[string]any{"data": map[string]any{"type": "sub2api-data", "version": 1, "proxies": []any{}, "accounts": []any{nil, account}}, "account_index": 1}
	other.expect(404, "POST", path, body)
	seller.expect(400, "POST", path, map[string]any{"data": map[string]any{"platform": "openai", "credentials": map[string]any{"access_token": "x"}}})
	if raw := seller.expect(200, "POST", path, body); strings.Contains(raw, "import-secret") || strings.Contains(raw, "credentials") {
		t.Fatal("import response must not disclose tokens")
	}
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", u.ID), nil, &u)
	if u.Status != listing.StatusVerifying {
		t.Fatalf("import bypassed verification: %s", u.Status)
	}
	if u.AccountEmail == nil || *u.AccountEmail != "exported@example.test" {
		t.Fatalf("imported account email not recognized: %v", u.AccountEmail)
	}
	var accountID int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", u.ID), &accountID)
	creds, err := e.fake.AccountCredentials(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if creds["access_token"] != "import-secret-access" || creds["base_url"] != nil || creds["account_scheduling_threshold"] != nil {
		t.Fatalf("unexpected imported credential fields: %v", creds)
	}
	var proxyID, concurrency int64
	var priority int
	var extra []byte
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT proxy_id, concurrency, priority, extra FROM accounts WHERE id = $1", accountID), &proxyID, &concurrency, &priority, &extra)
	proxy, err := e.app.Proxies.Get(e.ctx, *u.ProxyID)
	if err != nil {
		t.Fatal(err)
	}
	if proxyID != *proxy.Sub2APIProxyID || concurrency == 999 || priority != 99 || strings.Contains(string(extra), "model_mapping") {
		t.Fatal("exported routing settings must not be applied")
	}
	seller.expect(409, "POST", path, body)
	e.passOnboarding(u.ID)

	duplicate := create("anthropic", "oauth", 2)
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/import", duplicate.ID), map[string]any{"data": account})
	setupToken := create("anthropic", "setup-token", 3)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", setupToken.ID), map[string]any{"data": map[string]any{"access_token": "setup-import-token"}}, &setupToken)
	if setupToken.Status != listing.StatusVerifying {
		t.Fatal("setup-token should be verified")
	}
	g := create("gemini", "google_one", 4)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", g.ID), map[string]any{"data": []any{map[string]any{"platform": "gemini", "type": "oauth", "credentials": map[string]any{"access_token": "g-access", "refresh_token": "g-refresh", "expires_at": 1900000000, "oauth_type": "google_one"}}}}, &g)
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", g.ID), &accountID)
	creds, err = e.fake.AccountCredentials(accountID)
	if err != nil || creds["expires_at"] != "1900000000" || creds["oauth_type"] != "google_one" {
		t.Fatalf("Gemini normalization failed: %v", err)
	}

	openai := create("openai", "oauth", 5)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", openai.ID), map[string]any{"data": map[string]any{"platform": "openai", "type": "oauth", "credentials": map[string]any{"access_token": "oa-access", "refresh_token": "oa-refresh", "chatgpt_account_id": "oa-id", "plan_type": "plus", "email": "oa@example.test"}}}, &openai)
	if openai.PrivacyOK == nil || !*openai.PrivacyOK || openai.Status != listing.StatusVerifying || openai.Tier == nil || *openai.Tier != "plus" ||
		openai.AccountEmail == nil || *openai.AccountEmail != "oa@example.test" {
		t.Fatal("OpenAI import must apply privacy, plan and email metadata")
	}

	k := create("kimi", "coding", 6)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", k.ID), map[string]any{"data": json.RawMessage(`{"type":"apikey","platform":"kimi","credentials":{"api_key":"sk-import-coding-0123456789","base_url":"http://untrusted.test"}}`)}, &k)
	if k.Status != listing.StatusVerifying {
		t.Fatal("Coding Plan import should be verified")
	}
	bad := create("kimi", "coding", 7)
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/import", bad.ID), map[string]any{"data": map[string]any{"api_key": "invalid-key-0123456789"}})
}

func TestConcurrentAccountImports(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]bool{"enabled": true}, nil)
	seller := e.newUser("import-race@market.test")
	var p listing.Proxy
	seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "import", Protocol: "http", Host: "10.10.5.1", Port: 8080}, &p)
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", p.ID), nil, &p)
	var u listing.Upstream
	seller.must("POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": p.ID}, &u)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for n := range 2 {
		wg.Go(func() {
			status, _ := seller.call("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", u.ID), map[string]any{"data": map[string]any{"access_token": fmt.Sprintf("race-token-%d", n)}}, nil)
			statuses <- status
		})
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("imports should serialize: %v", counts)
	}
	var accounts int
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT count(*) FROM accounts WHERE name = $1", listing.AccountName(u.ID)), &accounts)
	if accounts != 1 {
		t.Fatalf("created %d accounts", accounts)
	}
}

func TestAccountImportWithExportedProxy(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	for _, p := range []string{"anthropic", "kimi"} {
		admin.must("PUT", "/api/admin/vendors/"+p, map[string]bool{"enabled": true}, nil)
	}
	admin.must("PUT", "/api/admin/settings", map[string]any{"require_seller_proxy": true}, nil)
	seller := e.newUser("import-proxy@market.test")
	other := e.newUser("import-proxy-other@market.test")
	create := func(platform, authType string) listing.Upstream {
		var u listing.Upstream
		seller.must("POST", "/api/seller/upstreams", map[string]any{"platform": platform, "auth_type": authType, "proxy_from_import": true}, &u)
		if u.ProxyID != nil {
			t.Fatal("upstream waiting for an imported proxy must start unbound")
		}
		return u
	}
	seller.expect(400, "POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_from_import": true, "proxy_id": 1})
	u := create("anthropic", "oauth")
	// 未绑定代理的上游不能绕过卖家代理要求直接授权或提交 key。
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", u.ID), nil)
	k := create("kimi", "coding")
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", k.ID), map[string]string{"api_key": "sk-kimi-import-proxy-0123456789"})

	const secret = "exported-proxy-secret"
	key := func(host string) string { return "socks5|" + host + "|1080|user|" + secret }
	exported := func(host string) map[string]any {
		return map[string]any{"proxy_key": key(host), "name": "exported " + host, "protocol": "socks5", "host": host, "port": 1080, "username": "user", "password": secret, "status": "active"}
	}
	file := func(token, host string, proxies ...map[string]any) map[string]any {
		account := map[string]any{"name": token, "platform": "anthropic", "type": "oauth",
			"credentials": map[string]any{"access_token": token, "refresh_token": token + "-refresh"}, "extra": map[string]any{"account_uuid": token}}
		if host != "" {
			account["proxy_key"] = key(host)
		}
		return map[string]any{"data": map[string]any{"type": "sub2api-data", "version": 1, "proxies": proxies, "accounts": []any{account}}}
	}
	importPath := func(u listing.Upstream) string { return fmt.Sprintf("/api/seller/upstreams/%d/import", u.ID) }
	expectCode := func(status int, code string, u listing.Upstream, body any) string {
		raw := seller.expect(status, "POST", importPath(u), body)
		if !strings.Contains(raw, code) {
			t.Fatalf("want %s, got %s", code, raw)
		}
		return raw
	}

	expectCode(400, "SELLER_PROXY_REQUIRED", u, file("no-proxy-token", ""))
	var claimed bool
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT identity_hash IS NOT NULL FROM upstreams WHERE id = $1", u.ID), &claimed)
	if claimed {
		t.Fatal("rejected import must not claim the account identity")
	}
	expectCode(400, "IMPORT_PROXY_NOT_FOUND", u, file("dangling-token", "203.0.113.40"))
	if raw := expectCode(400, "IMPORT_PROXY_UNREACHABLE", u, file("unreachable-token", "unreachable.example.test", exported("unreachable.example.test"))); strings.Contains(raw, secret) {
		t.Fatal("import error leaks the proxy password")
	}
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", u.ID), nil, &u)
	var proxies []listing.Proxy
	seller.must("GET", "/api/seller/proxies", nil, &proxies)
	if u.ProxyID != nil || len(proxies) != 1 || proxies[0].ConnectionReport == nil || proxies[0].ConnectionReport.Success || proxies[0].AssignedUpstreamID != nil {
		t.Fatalf("unreachable proxy must stay unbound in the inventory with its failed report: %+v", proxies)
	}
	other.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "taken", Protocol: "socks5", Host: "taken.example.test", Port: 1080}, nil)
	expectCode(409, "PROXY_ALREADY_EXISTS", u, file("taken-token", "taken.example.test", exported("taken.example.test")))

	seller.must("POST", importPath(u), file("proxy-token", "203.0.113.50", exported("203.0.113.50")), &u)
	if u.Status != listing.StatusVerifying || u.ProxyID == nil {
		t.Fatalf("import with a reachable proxy must bind it and start verification: %+v", u)
	}
	proxy, err := e.app.Proxies.Get(e.ctx, *u.ProxyID)
	if err != nil {
		t.Fatal(err)
	}
	if proxy.SellerID == nil || *proxy.SellerID != u.SellerID || proxy.Status != "assigned" || proxy.AssignedUpstreamID == nil || *proxy.AssignedUpstreamID != u.ID ||
		proxy.Name != "exported 203.0.113.50" || proxy.ConnectionReport == nil || !proxy.ConnectionReport.Success {
		t.Fatalf("imported proxy must be registered to the seller and bound after a successful test: %+v", proxy)
	}
	var encrypted []byte
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT password_enc FROM proxies WHERE id = $1", proxy.ID), &encrypted)
	if plain, err := e.app.Keyring.OpenString(encrypted, fmt.Sprintf("proxy:%d", proxy.ID)); err != nil || plain != secret {
		t.Fatalf("imported proxy password must be stored encrypted: %v", err)
	}
	var accountID, accountProxyID int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", u.ID), &accountID)
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT proxy_id FROM accounts WHERE id = $1", accountID), &accountProxyID)
	if accountProxyID != *proxy.Sub2APIProxyID {
		t.Fatalf("sub2api account must use the imported proxy: %d", accountProxyID)
	}

	// 同一卖家已登记的地址直接复用并重新检测；已绑定其他账号的代理不能再用。
	var own listing.Proxy
	seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "own", Protocol: "socks5", Host: "203.0.113.60", Port: 1080, Username: "user", Password: secret}, &own)
	reuse := create("anthropic", "oauth")
	seller.must("POST", importPath(reuse), file("reuse-token", "203.0.113.60", exported("203.0.113.60")), &reuse)
	if reuse.ProxyID == nil || *reuse.ProxyID != own.ID {
		t.Fatalf("registered seller proxy must be reused: %+v", reuse)
	}
	busy := create("anthropic", "oauth")
	expectCode(409, "PROXY_IN_USE", busy, file("busy-token", "203.0.113.50", exported("203.0.113.50")))
	var count int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM proxies WHERE seller_id = $1", u.SellerID), &count)
	if count != 3 {
		t.Fatalf("imports must not duplicate proxies: %d", count)
	}
}
