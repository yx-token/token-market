//go:build integration

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestProxyDiagnosticsPersistenceAndOwnership(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	seller := e.newUser("proxy-diagnostics@market.test")
	other := e.newUser("proxy-diagnostics-other@market.test")
	var proxy listing.Proxy
	seller.must("POST", "/api/seller/proxies", listing.CreateProxyInput{Name: "diagnostics", Protocol: "http", Host: "proxy.example.test", Port: 8080, Password: "secret-proxy-password"}, &proxy)
	for _, op := range []string{"test", "quality-check"} {
		path := fmt.Sprintf("/api/seller/proxies/%d/%s", proxy.ID, op)
		other.expect(404, "POST", path, nil)
		e.anon().expect(401, "POST", path, nil)
		seller.must("POST", path, nil, &proxy)
	}
	if proxy.Region == nil || *proxy.Region != "US" || proxy.ConnectionReport == nil || !proxy.ConnectionReport.Success || proxy.QualityReport == nil || proxy.QualityReport.Score != 100 || len(proxy.QualityReport.Items) != 5 {
		t.Fatalf("missing diagnostics: %+v", proxy)
	}
	var proxies []listing.Proxy
	seller.must("GET", "/api/seller/proxies", nil, &proxies)
	if len(proxies) != 1 || proxies[0].QualityCheckedAt == nil || proxies[0].ConnectionCheckedAt == nil || proxies[0].ConnectionReport.IPAddress != "203.0.113.25" {
		t.Fatal("reports must survive reloading")
	}
	admin.must("POST", fmt.Sprintf("/api/admin/proxies/%d/test", proxy.ID), nil, &proxy)
	admin.must("POST", fmt.Sprintf("/api/admin/proxies/%d/quality-check", proxy.ID), nil, &proxy)
	// 已同步的代理发生出站失败时仍保存失败报告，不能显示为成功。
	if _, err := e.fake.DB.Exec(e.ctx, "UPDATE proxies SET host = 'unreachable.example.test' WHERE id = $1", proxy.Sub2APIProxyID); err != nil {
		t.Fatal(err)
	}
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil, &proxy)
	if proxy.ConnectionReport.Success {
		t.Fatal("failed connection was shown as successful")
	}
	seller.must("POST", fmt.Sprintf("/api/seller/proxies/%d/quality-check", proxy.ID), nil, &proxy)
	if proxy.QualityReport.Score != 0 || proxy.QualityReport.FailedCount != 1 {
		t.Fatal("failed quality report was not stored")
	}
	original := e.app.Proxies.Admin
	leaking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"success":false,"message":"socks5://user:secret-proxy-password@proxy.example.test:8080 failed"}}`))
	}))
	defer leaking.Close()
	e.app.Proxies.Admin = adminclient.New(leaking.URL, "test", time.Second)
	if raw := seller.expect(200, "POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil); strings.Contains(raw, "secret-proxy-password") || !strings.Contains(raw, "[redacted]") {
		t.Fatal("report exposed proxy password")
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer failing.Close()
	e.app.Proxies.Admin = adminclient.New(failing.URL, "test", time.Second)
	seller.expect(503, "POST", fmt.Sprintf("/api/seller/proxies/%d/quality-check", proxy.ID), nil)
	e.app.Proxies.Admin = original
	seller.must("GET", "/api/seller/proxies", nil, &proxies)
	if proxies[0].QualityReport.Score != 0 || proxies[0].QualityReport.FailedCount != 1 {
		t.Fatal("request error overwrote saved quality report")
	}
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE proxies SET sub2api_proxy_id = NULL WHERE id = $1", proxy.ID); err != nil {
		t.Fatal(err)
	}
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/proxies/%d/test", proxy.ID), nil)
}
