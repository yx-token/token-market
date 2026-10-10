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
		var u listing.Upstream
		seller.must("POST", "/api/seller/upstreams", map[string]any{"platform": platform, "auth_type": authType, "proxy_id": proxy.ID}, &u)
		return u
	}
	u := create("anthropic", "oauth", 1)
	path := fmt.Sprintf("/api/seller/upstreams/%d/import", u.ID)
	account := map[string]any{"name": "exported account", "platform": "anthropic", "type": "oauth",
		"credentials": map[string]any{"access_token": "import-secret-access", "refresh_token": "import-secret-refresh", "base_url": "http://untrusted.test", "account_scheduling_threshold": 1},
		"extra":       map[string]any{"account_uuid": "export-account-1", "model_mapping": map[string]any{"claude": "other"}},
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
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/import", openai.ID), map[string]any{"data": map[string]any{"platform": "openai", "type": "oauth", "credentials": map[string]any{"access_token": "oa-access", "refresh_token": "oa-refresh", "chatgpt_account_id": "oa-id", "plan_type": "plus"}}}, &openai)
	if openai.PrivacyOK == nil || !*openai.PrivacyOK || openai.Status != listing.StatusVerifying || openai.Tier == nil || *openai.Tier != "plus" {
		t.Fatal("OpenAI import must apply privacy and plan metadata")
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
