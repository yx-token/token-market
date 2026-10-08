package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKey() string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
}

func TestLoadAppliesFileEnvAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "database:\n  dsn: postgres://file\nsub2api:\n  base_url: http://sub2api:8080\n  admin_api_key: from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SUB2API_ADMIN_API_KEY", "from-env")
	t.Setenv("TM_CRYPTO_KEYS", "k1:"+testKey())
	t.Setenv("TM_CRYPTO_ACTIVE_KEY_ID", "k1")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.DSN != "postgres://file" {
		t.Errorf("dsn = %q", cfg.Database.DSN)
	}
	if cfg.Sub2API.AdminAPIKey != "from-env" {
		t.Errorf("env should override file, got %q", cfg.Sub2API.AdminAPIKey)
	}
	if cfg.Sub2API.Timeout != 30*time.Second || cfg.Worker.OpsBatchSize != 20 || cfg.HTTP.Addr != ":8080" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if _, ok := cfg.Crypto.Keys["k1"]; !ok {
		t.Errorf("crypto keys from env not applied")
	}
}

func TestValidateRejectsMissingActiveKey(t *testing.T) {
	cfg := &Config{Crypto: CryptoConfig{ActiveKeyID: "k2", Keys: map[string]string{"k1": testKey()}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing active key")
	}
}

func TestValidateRejectsShortKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	cfg := &Config{Crypto: CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": short}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestApplyEnvRejectsMalformedCryptoKeys(t *testing.T) {
	cfg := &Config{}
	lookup := func(name string) (string, bool) {
		if name == "TM_CRYPTO_KEYS" {
			return "no-separator", true
		}
		return "", false
	}
	if err := applyEnv(cfg, lookup); err == nil {
		t.Fatal("expected error")
	}
}

// 仓库根目录的配置示例必须能被解析（只替换占位的密钥）。
func TestExampleConfigParses(t *testing.T) {
	raw, err := os.ReadFile("../../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(raw), "REPLACE_WITH_BASE64_32_BYTES", testKey(), 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worker.AdminAddr != ":8092" || cfg.Edge.InvalidKeyLimit != 30 || cfg.Worker.BalanceReconcileInterval != 30*time.Minute ||
		cfg.Sub2API.GatewayURL != cfg.Sub2API.BaseURL || cfg.Auth.TokenTTL != 24*time.Hour {
		t.Fatalf("example config: %+v", cfg)
	}
}
