package adminclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyDiagnosticsContract(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"data":{"success":true,"ip_address":"203.0.113.1","country_code":"JP","latency_ms":42}}`, nil)
	result, err := c.TestProxy(context.Background(), 7)
	if err != nil || !result.Success || result.CountryCode != "JP" || result.LatencyMs != 42 || rec.path != "/api/v1/admin/proxies/7/test" || rec.apiKey != "admin-key" {
		t.Fatalf("invalid connectivity response: %v", err)
	}
	c, rec = newServer(t, 200, `{"code":0,"data":{"score":75,"grade":"B","items":[{"target":"openai","status":"challenge","http_status":403,"cf_ray":"ray"}]}}`, nil)
	quality, err := c.CheckProxyQuality(context.Background(), 7)
	if err != nil || quality.Score != 75 || quality.Items[0].Status != "challenge" || rec.path != "/api/v1/admin/proxies/7/quality-check" || rec.apiKey != "admin-key" {
		t.Fatalf("invalid quality response: %v", err)
	}
	for _, payload := range []string{`{"code":0,"data":null}`, `{"code":0,"data":{}}`} {
		c, _ = newServer(t, 200, payload, nil)
		if _, err := c.CheckProxyQuality(context.Background(), 7); err == nil {
			t.Fatal("empty report accepted")
		}
	}
}

func TestQualityDiagnosticsTimeoutAndGate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(20 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"grade":"A","items":[{"target":"openai","status":"pass"}]}}`))
	}))
	defer srv.Close()
	hc := srv.Client()
	hc.Timeout = time.Millisecond
	c := New(srv.URL, "key", time.Millisecond, WithHTTPClient(hc))
	if _, err := c.CheckProxyQuality(context.Background(), 1); err != nil {
		t.Fatalf("quality check used ordinary short timeout: %v", err)
	}
	if hc.Timeout != time.Millisecond {
		t.Fatal("custom HTTP client was mutated")
	}
	c = New(srv.URL, "key", time.Second, WithWriteGate(func(context.Context) error { return ErrWritesPaused }))
	if _, err := c.CheckProxyQuality(context.Background(), 1); !errors.Is(err, ErrWritesPaused) {
		t.Fatalf("quality check bypassed gate: %v", err)
	}
}
