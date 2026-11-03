package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestRequestOriginTrustsOnlyConfiguredProxy(t *testing.T) {
	r := httptest.NewRequest("POST", "http://market.local/api/buyer/recharge", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-Host", "wallet.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := requestOrigin(r, nil); got != "http://market.local" {
		t.Fatalf("untrusted forwarded origin: %q", got)
	}
	if got := requestOrigin(r, []string{"192.0.2.0/24"}); got != "https://wallet.example" {
		t.Fatalf("trusted forwarded origin: %q", got)
	}
	r.Header.Set("X-Forwarded-Host", "wallet.example/bad")
	if got := requestOrigin(r, []string{"192.0.2.1"}); got != "" {
		t.Fatalf("invalid origin accepted: %q", got)
	}
}

func TestRequestOriginUsesMatchingBrowserOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{name: "https origin on http connection", origin: "https://market.local", want: "https://market.local"},
		{name: "different host ignored", origin: "https://other.local", want: "http://market.local"},
		{name: "invalid origin ignored", origin: "not an origin", want: "http://market.local"},
		{name: "null origin ignored", origin: "null", want: "http://market.local"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://market.local/api/buyer/recharge", nil)
			r.Header.Set("Origin", tc.origin)
			if got := requestOrigin(r, nil); got != tc.want {
				t.Fatalf("request origin = %q, want %q", got, tc.want)
			}
		})
	}
}
