package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yx-token/token-market/market/internal/listing"
)

func TestSellerProxyViewRedactsPlatformProxy(t *testing.T) {
	username, engineID := "private-user", int64(42)
	proxy := &listing.Proxy{ID: 1, Name: "internal pool", Protocol: "socks5", Host: "private-proxy.example", Port: 1080,
		Username: &username, Sub2APIProxyID: &engineID, IsShared: true, Status: "assigned"}
	raw, err := json.Marshal(sellerProxyView(proxy))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"host", "port", "username", "sub2api_proxy_id", "private-proxy", "internal pool", "socks5", "private-user"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("platform proxy leaks %s in %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), `"is_shared":true`) {
		t.Fatalf("missing shared state: %s", raw)
	}
}
