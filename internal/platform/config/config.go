// Package config 加载 token-market 各进程共用的配置。
//
// 配置来自 YAML 文件，敏感字段可以用环境变量覆盖（见 envOverrides），
// 以便生产环境不把密钥写进文件。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Env      string         `yaml:"env"`
	HTTP     HTTPConfig     `yaml:"http"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	Crypto   CryptoConfig   `yaml:"crypto"`
	Sub2API  Sub2APIConfig  `yaml:"sub2api"`
	Worker   WorkerConfig   `yaml:"worker"`
}

type HTTPConfig struct {
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	DSN      string `yaml:"dsn"`
	MaxConns int32  `yaml:"max_conns"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// CryptoConfig 是应用层加密的密钥环。Keys 的值为 base64 编码的 32 字节密钥；
// ActiveKeyID 用于新数据加密，旧密钥保留以便解密历史数据。
type CryptoConfig struct {
	ActiveKeyID string            `yaml:"active_key_id"`
	Keys        map[string]string `yaml:"keys"`
}

type Sub2APIConfig struct {
	BaseURL     string        `yaml:"base_url"`
	AdminAPIKey string        `yaml:"admin_api_key"`
	ReplicaDSN  string        `yaml:"replica_dsn"`
	Timeout     time.Duration `yaml:"timeout"`
}

type WorkerConfig struct {
	OpsBatchSize        int           `yaml:"ops_batch_size"`
	OpsPollInterval     time.Duration `yaml:"ops_poll_interval"`
	CompatGuardInterval time.Duration `yaml:"compat_guard_interval"`
}

// Load 读取 YAML 配置，应用环境变量覆盖与默认值，并校验。path 为空时只使用环境变量与默认值。
func Load(path string) (*Config, error) {
	cfg := &Config{}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	if err := applyEnv(cfg, os.LookupEnv); err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config, lookup func(string) (string, bool)) error {
	str := map[string]*string{
		"TM_ENV":                   &cfg.Env,
		"TM_HTTP_ADDR":             &cfg.HTTP.Addr,
		"TM_DATABASE_DSN":          &cfg.Database.DSN,
		"TM_REDIS_ADDR":            &cfg.Redis.Addr,
		"TM_REDIS_PASSWORD":        &cfg.Redis.Password,
		"TM_SUB2API_BASE_URL":      &cfg.Sub2API.BaseURL,
		"TM_SUB2API_ADMIN_API_KEY": &cfg.Sub2API.AdminAPIKey,
		"TM_SUB2API_REPLICA_DSN":   &cfg.Sub2API.ReplicaDSN,
		"TM_CRYPTO_ACTIVE_KEY_ID":  &cfg.Crypto.ActiveKeyID,
	}
	for name, dst := range str {
		if v, ok := lookup(name); ok {
			*dst = v
		}
	}
	if v, ok := lookup("TM_REDIS_DB"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("TM_REDIS_DB: %w", err)
		}
		cfg.Redis.DB = n
	}
	// TM_CRYPTO_KEYS 形如 "k1:base64,k2:base64"，整体替换文件中的密钥环。
	if v, ok := lookup("TM_CRYPTO_KEYS"); ok {
		keys := map[string]string{}
		for _, pair := range strings.Split(v, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			id, key, found := strings.Cut(pair, ":")
			if !found || id == "" || key == "" {
				return errors.New("TM_CRYPTO_KEYS: expected id:base64 pairs")
			}
			keys[id] = key
		}
		cfg.Crypto.Keys = keys
	}
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Env == "" {
		cfg.Env = "dev"
	}
	if cfg.HTTP.Addr == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.Database.MaxConns <= 0 {
		cfg.Database.MaxConns = 20
	}
	if cfg.Sub2API.Timeout <= 0 {
		cfg.Sub2API.Timeout = 30 * time.Second
	}
	if cfg.Worker.OpsBatchSize <= 0 {
		cfg.Worker.OpsBatchSize = 20
	}
	if cfg.Worker.OpsPollInterval <= 0 {
		cfg.Worker.OpsPollInterval = time.Second
	}
	if cfg.Worker.CompatGuardInterval <= 0 {
		cfg.Worker.CompatGuardInterval = 5 * time.Minute
	}
}

// Validate 只校验各进程都必需的配置；进程特有的必填项由各自的 main 检查。
func (c *Config) Validate() error {
	if c.Crypto.ActiveKeyID != "" {
		raw, ok := c.Crypto.Keys[c.Crypto.ActiveKeyID]
		if !ok {
			return fmt.Errorf("crypto.active_key_id %q not found in crypto.keys", c.Crypto.ActiveKeyID)
		}
		if _, err := DecodeKey(raw); err != nil {
			return fmt.Errorf("crypto key %q: %w", c.Crypto.ActiveKeyID, err)
		}
	}
	if c.Sub2API.BaseURL != "" && !strings.HasPrefix(c.Sub2API.BaseURL, "http://") && !strings.HasPrefix(c.Sub2API.BaseURL, "https://") {
		return errors.New("sub2api.base_url must start with http:// or https://")
	}
	return nil
}

// DecodeKey 解码 base64 密钥并要求长度为 32 字节（AES-256）。
func DecodeKey(raw string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	return key, nil
}
