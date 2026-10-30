//go:build integration

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestSellerProxyOnboarding(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	// 删除测试夹具的旧分配策略，验证未配置和旧设置中缺失字段时均默认要求卖家代理。
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE settings SET value = '{"allow_no_proxy":true}' WHERE key = 'platform'`); err != nil {
		t.Fatal(err)
	}
	var policy struct {
		RequireSellerProxy bool `json:"require_seller_proxy"`
	}
	e.anon().must("GET", "/api/market/config", nil, &policy)
	if !policy.RequireSellerProxy {
		t.Fatal("missing proxy policy must default to requiring a seller proxy")
	}
	for _, platform := range []string{"anthropic", "kimi"} {
		admin.must("PUT", "/api/admin/vendors/"+platform, map[string]any{"enabled": true}, nil)
	}
	seller := e.newUser("proxy-owner@market.test")
	other := e.newUser("proxy-other@market.test")
	e.anon().expect(401, "GET", "/api/seller/proxies", nil)
	for _, value := range []any{nil, 0, -1} {
		seller.expect(400, "POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": value})
	}
	var platformProxy listing.Proxy
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "platform", "protocol": "http", "host": "10.1.1.1", "port": 1080}, &platformProxy)
	var reference struct {
		Upstream listing.Upstream `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &reference)
	if reference.Upstream.ProxyID == nil || *reference.Upstream.ProxyID != platformProxy.ID {
		t.Fatal("platform reference accounts must bypass the seller requirement and use platform inventory")
	}
	seller.expect(404, "POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": platformProxy.ID})

	input := listing.CreateProxyInput{Name: " My IP ", Protocol: "socks5", Host: " Proxy.Example.Test ", Port: 1080, Username: "user", Password: "proxy-password", Region: "us"}
	var proxy listing.Proxy
	seller.must("POST", "/api/seller/proxies", input, &proxy)
	if proxy.SellerID == nil || proxy.Sub2APIProxyID == nil || proxy.Name != "My IP" || proxy.Host != "proxy.example.test" {
		t.Fatalf("invalid saved seller proxy: %+v", proxy)
	}
	var encrypted []byte
	if err := e.app.DB.QueryRow(e.ctx, "SELECT password_enc FROM proxies WHERE id = $1", proxy.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plain, err := e.app.Keyring.OpenString(encrypted, fmt.Sprintf("proxy:%d", proxy.ID))
	if err != nil || plain != input.Password || bytes.Contains(encrypted, []byte(input.Password)) {
		t.Fatalf("proxy password must be encrypted and recoverable: %v", err)
	}
	var own, others []listing.Proxy
	if raw := seller.expect(200, "GET", "/api/seller/proxies", nil); strings.Contains(raw, input.Password) || strings.Contains(raw, "password_enc") {
		t.Fatal("proxy response leaks password")
	}
	seller.must("GET", "/api/seller/proxies", nil, &own)
	other.must("GET", "/api/seller/proxies", nil, &others)
	if len(own) != 1 || len(others) != 0 {
		t.Fatalf("proxy lists must be isolated: own=%+v, other=%+v", own, others)
	}
	for _, operation := range []string{"sync", "disable", "enable"} {
		other.expect(404, "POST", fmt.Sprintf("/api/seller/proxies/%d/%s", proxy.ID, operation), nil)
	}
	other.expect(404, "POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": proxy.ID})
	other.expect(409, "POST", "/api/seller/proxies", input) // 同一地址不能换个卖家重复登记
	bad := input
	bad.Host = "socks5://user:password@proxy.example.test:1080"
	seller.expect(400, "POST", "/api/seller/proxies", bad)
	spoofed := map[string]any{"name": "spoof", "protocol": "http", "host": "10.1.1.4", "port": 1080, "seller_id": *proxy.SellerID}
	other.expect(400, "POST", "/api/seller/proxies", spoofed)

	create := map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": proxy.ID}
	// 只有连通性检测成功的代理才能绑定账号。
	if raw := seller.expect(409, "POST", "/api/seller/upstreams", create); !strings.Contains(raw, "PROXY_NOT_CONNECTED") {
		t.Fatalf("untested proxy must not be selectable: %s", raw)
	}
	var unreachable listing.Proxy
	seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "down", Protocol: "http", Host: "unreachable.example.test", Port: 1080}, &unreachable)
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", unreachable.ID), nil, nil)
	if raw := seller.expect(409, "POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": unreachable.ID}); !strings.Contains(raw, "PROXY_NOT_CONNECTED") {
		t.Fatalf("proxy with a failed connection test must not be selectable: %s", raw)
	}
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil, &proxy)
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/disable", proxy.ID), nil, nil)
	seller.expect(409, "POST", "/api/seller/upstreams", create)
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/enable", proxy.ID), nil, nil)
	var upstream listing.Upstream
	seller.must("POST", "/api/seller/upstreams", create, &upstream)
	if upstream.ProxyID == nil || *upstream.ProxyID != proxy.ID {
		t.Fatalf("selected proxy was not bound: %+v", upstream)
	}
	// 卖家自有代理可以同时绑定多个账号。
	var second listing.Upstream
	seller.must("POST", "/api/seller/upstreams", create, &second)
	if second.ProxyID == nil || *second.ProxyID != proxy.ID {
		t.Fatalf("seller proxy must be shareable: %+v", second)
	}
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/proxies/%d/disable", proxy.ID), nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", upstream.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upstream.ID), map[string]string{"code": "seller-proxy-account"}, &upstream)
	if err := e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", upstream.ID).Scan(&upstream.Sub2APIAccountID); err != nil {
		t.Fatal(err)
	}
	var actualProxyID int64
	if err := e.replica.QueryRow(e.ctx, "SELECT proxy_id FROM accounts WHERE id = $1", upstream.Sub2APIAccountID).Scan(&actualProxyID); err != nil || actualProxyID != *proxy.Sub2APIProxyID {
		t.Fatalf("sub2api account must use selected proxy: %d, %v", actualProxyID, err)
	}
	seller.must("DELETE", fmt.Sprintf("/api/seller/upstreams/%d", second.ID), nil, nil)
	seller.must("DELETE", fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID), nil, nil)
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/proxies/%d/disable", proxy.ID), nil) // 移除中仍然占用
	if err := e.app.Admin.DeleteAccount(e.ctx, *upstream.Sub2APIAccountID); err != nil {
		t.Fatal(err)
	}
	e.schedule()
	if released, err := e.app.Proxies.Get(e.ctx, proxy.ID); err != nil || released.Status != "available" || released.AssignedUpstreamCount != 0 {
		t.Fatalf("proxy must be released after its last account is removed: %+v, %v", released, err)
	}
	// 移除完成后代理可再次用于 Coding Plan，并下发同一个代理。
	create["platform"], create["auth_type"] = "kimi", "coding"
	seller.must("POST", "/api/seller/upstreams", create, &upstream)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", upstream.ID), map[string]string{"api_key": "sk-kimi-seller-proxy-0123456789"}, &upstream)
	if err := e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", upstream.ID).Scan(&upstream.Sub2APIAccountID); err != nil {
		t.Fatal(err)
	}
	if err := e.replica.QueryRow(e.ctx, "SELECT proxy_id FROM accounts WHERE id = $1", upstream.Sub2APIAccountID).Scan(&actualProxyID); err != nil || actualProxyID != *proxy.Sub2APIProxyID {
		t.Fatalf("coding account must use selected proxy: %d, %v", actualProxyID, err)
	}
}

func TestSellerProxyConcurrencyAndPlatformFallback(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	admin.must("PUT", "/api/admin/settings", map[string]any{"require_seller_proxy": true}, nil)
	seller := e.newUser("proxy-race@market.test")
	var proxy listing.Proxy
	seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "race", Protocol: "http", Host: "10.1.2.1", Port: 1080}, &proxy)
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil, &proxy)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			status, _ := seller.call("POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": proxy.ID}, nil)
			statuses <- status
		})
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[201] != 2 {
		t.Fatalf("concurrent accounts should share a seller proxy: %v", counts)
	}
	var accounts []listing.Upstream
	seller.must("GET", "/api/seller/upstreams", nil, &accounts)
	if len(accounts) != 2 {
		t.Fatalf("both accounts must be created: %+v", accounts)
	}
	if shared, err := e.app.Proxies.Get(e.ctx, proxy.ID); err != nil || shared.AssignedUpstreamCount != 2 {
		t.Fatalf("shared proxy must list both accounts: %+v, %v", shared, err)
	}
	for _, account := range accounts {
		seller.must("DELETE", fmt.Sprintf("/api/seller/upstreams/%d", account.ID), nil, nil)
	}
	admin.must("PUT", "/api/admin/settings", map[string]any{"require_seller_proxy": false}, nil)
	// 平台没有库存时不能借用卖家自己的空闲代理。
	seller.expect(503, "POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"})
	var platformProxy listing.Proxy
	admin.must("POST", "/api/admin/proxies", listing.CreateProxyInput{Name: "platform", Protocol: "http", Host: "10.1.2.2", Port: 1080}, &platformProxy)
	var upstream listing.Upstream
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	if upstream.ProxyID == nil || *upstream.ProxyID != platformProxy.ID {
		t.Fatalf("platform fallback must use platform inventory: %+v", upstream)
	}
	// 平台分配模式仍允许卖家选自己的代理。
	seller.must("POST", "/api/seller/upstreams", map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": proxy.ID}, &upstream)
	if upstream.ProxyID == nil || *upstream.ProxyID != proxy.ID {
		t.Fatal("optional seller proxy should still be honored")
	}
}

func TestSellerProxySyncRecovery(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	admin.must("PUT", "/api/admin/settings", map[string]any{"require_seller_proxy": true}, nil)
	seller := e.newUser("proxy-recovery@market.test")
	original := e.app.Proxies.Admin
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":503,"message":"unavailable"}`))
	}))
	defer failing.Close()
	e.app.Proxies.Admin = adminclient.New(failing.URL, "test-key", time.Second)
	seller.expect(503, "POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "recover", Protocol: "socks5", Host: "10.1.3.1", Port: 1080, Password: "recovery-password"})
	e.app.Proxies.Admin = original
	var proxies []listing.Proxy
	seller.must("GET", "/api/seller/proxies", nil, &proxies)
	if len(proxies) != 1 || proxies[0].Sub2APIProxyID != nil {
		t.Fatalf("failed sync must preserve a retryable proxy: %+v", proxies)
	}
	proxy := proxies[0]
	body := map[string]any{"platform": "anthropic", "auth_type": "oauth", "proxy_id": proxy.ID}
	if raw := seller.expect(409, "POST", "/api/seller/upstreams", body); !strings.Contains(raw, "PROXY_NOT_SYNCED") {
		t.Fatalf("unsynced proxy must be rejected: %s", raw)
	}
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/sync", proxy.ID), nil, &proxy)
	if proxy.Sub2APIProxyID == nil {
		t.Fatal("retry must synchronize the saved proxy")
	}
	var password string
	if err := e.replica.QueryRow(e.ctx, "SELECT password FROM proxies WHERE id = $1", *proxy.Sub2APIProxyID).Scan(&password); err != nil || password != "recovery-password" {
		t.Fatalf("retry must decrypt and submit the original password: %v", err)
	}
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil, &proxy)
	seller.must("POST", "/api/seller/upstreams", body, nil)
	// 密码不进入 HTTP 返回的数据。
	raw, err := json.Marshal(proxy)
	if err != nil || bytes.Contains(raw, []byte(password)) {
		t.Fatal("serialized proxy must not contain its password")
	}
}
