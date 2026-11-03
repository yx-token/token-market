package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/testutil/fakepay"
)

func TestNormalizeNotifyOrigin(t *testing.T) {
	got, err := normalizeNotifyOrigin(" https://market.example:8443/ ")
	if err != nil || got != "https://market.example:8443" {
		t.Fatalf("normalize = %q, %v", got, err)
	}
	for _, raw := range []string{"", "/relative", "ftp://example.com", "http://example.com/api", "http://example.com?x=1", "http://user@example.com"} {
		if got, err := normalizeNotifyOrigin(raw); err == nil {
			t.Errorf("normalizeNotifyOrigin(%q) = %q, want error", raw, got)
		}
	}
}

func TestWriteProvidersUsesHostPaymentContract(t *testing.T) {
	dir := t.TempDir()
	keys, app, merchant, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"alipay_app_private.pem":      payment.EncodePrivateKeyPEM(app),
		"alipay_platform_public.pem":  payment.EncodePublicKeyPEM(&keys.AlipayPlatform.PublicKey),
		"wechat_merchant_private.pem": payment.EncodePrivateKeyPEM(merchant),
		"wechat_platform_public.pem":  payment.EncodePublicKeyPEM(&keys.WechatPlatform.PublicKey),
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeProviders(dir, "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	for file, provider := range map[string]string{"provider-alipay.json": "alipay", "provider-wxpay.json": "wxpay"} {
		path := filepath.Join(dir, file)
		var got struct {
			ProviderKey string            `json:"provider_key"`
			Name        string            `json:"name"`
			Enabled     bool              `json:"enabled"`
			Types       []string          `json:"supported_types"`
			Config      map[string]string `json:"config"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.ProviderKey != provider || got.Name != "dev-fake-"+provider || !got.Enabled || len(got.Types) != 1 || got.Types[0] != provider {
			t.Errorf("%s instance = %+v", file, got)
		}
		wantNotifyProvider := provider
		if provider == "wxpay" {
			wantNotifyProvider = "wechat"
		}
		if got.Config["notifyUrl"] != "http://127.0.0.1:8080/api/payments/"+wantNotifyProvider+"/notify" {
			t.Errorf("%s notifyUrl = %q", file, got.Config["notifyUrl"])
		}
		if provider == "wxpay" && len(got.Config["apiV3Key"]) != 32 {
			t.Errorf("apiV3Key length = %d, want 32", len(got.Config["apiV3Key"]))
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions = %o, want 600", file, info.Mode().Perm())
		}
	}
}
