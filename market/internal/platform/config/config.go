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
	Auth     AuthConfig     `yaml:"auth"`
	Sub2API  Sub2APIConfig  `yaml:"sub2api"`
	Worker   WorkerConfig   `yaml:"worker"`
	Edge     EdgeConfig     `yaml:"edge"`
	Update   UpdateConfig   `yaml:"update"`
}

// UpdateConfig 的访问令牌仅用于服务端读取 Release，不能返回给浏览器。
type UpdateConfig struct {
	Repository  string `yaml:"repository"`
	GitHubToken string `yaml:"github_token"`
	RuntimeDir  string `yaml:"runtime_dir"`
}

type AuthConfig struct {
	JWTSecret string        `yaml:"jwt_secret"`
	TokenTTL  time.Duration `yaml:"token_ttl"`
	// 首次启动时若没有任何管理员，用这组账号创建一个
	BootstrapAdminEmail    string `yaml:"bootstrap_admin_email"`
	BootstrapAdminPassword string `yaml:"bootstrap_admin_password"`
}

// EdgeConfig 是 market-edge 的配置。
type EdgeConfig struct {
	// PublicURL 是买家调用模型时使用的地址（Edge 对外的域名），在买家控制台展示
	PublicURL        string        `yaml:"public_url"`
	Addr             string        `yaml:"addr"`
	AdminAddr        string        `yaml:"admin_addr"`         // 健康检查与指标
	ConnectTimeout   time.Duration `yaml:"connect_timeout"`    // 每跳连接 sub2api 的超时
	FirstByteTimeout time.Duration `yaml:"first_byte_timeout"` // 整个请求的首字节截止时间
	MaxBodyBytes     int64         `yaml:"max_body_bytes"`
	BreakerFailures  int           `yaml:"breaker_failures"` // 连续失败多少次熔断
	BreakerCooldown  time.Duration `yaml:"breaker_cooldown"`
	SpoolDir         string        `yaml:"spool_dir"` // 尝试日志写库失败时的本地暂存目录
	// TrustedProxies 是前置负载均衡的 IP 或 CIDR，只有来自它们的请求才采信 X-Forwarded-For（IP 白名单依赖它）
	TrustedProxies  []string `yaml:"trusted_proxies"`
	InvalidKeyLimit int      `yaml:"invalid_key_limit"` // 单个 IP 每分钟允许的无效 Key 次数
}

type HTTPConfig struct {
	Addr string `yaml:"addr"`
	// WebDir 是 C 端构建产物目录（web/dist）；设置后 market-api 同时提供闲置 TOKEN 页面
	WebDir string `yaml:"web_dir"`
	// AdminWebDir 是管理端构建产物目录（admin-dist）；挂在 /admin 与 /admin-assets
	AdminWebDir string `yaml:"admin_web_dir"`
	// TrustedProxies 是前置负载均衡的 IP 或 CIDR，只有来自它们的请求才采信 X-Forwarded-For
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type DatabaseConfig struct {
	DSN      string `yaml:"dsn"`
	MaxConns int32  `yaml:"max_conns"`
	// TimeZone 是数据库会话时区，决定按天统计（账单、用量、今日收益、每日检测预算）的日界，默认 Asia/Shanghai
	TimeZone string `yaml:"time_zone"`
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
	// HashPepper 用于不可逆的盲索引（如证件号哈希），一经使用不能更换，生产环境必须显式设置
	HashPepper string `yaml:"hash_pepper"`
}

type Sub2APIConfig struct {
	BaseURL           string        `yaml:"base_url"`
	GatewayURL        string        `yaml:"gateway_url"` // 网关地址，默认与 base_url 相同
	ShadowEmailDomain string        `yaml:"shadow_email_domain"`
	AdminAPIKey       string        `yaml:"admin_api_key"`
	ReplicaDSN        string        `yaml:"replica_dsn"`
	Timeout           time.Duration `yaml:"timeout"`
}

type WorkerConfig struct {
	OpsBatchSize        int           `yaml:"ops_batch_size"`
	OpsPollInterval     time.Duration `yaml:"ops_poll_interval"`
	CompatGuardInterval time.Duration `yaml:"compat_guard_interval"`
	SchedulerInterval   time.Duration `yaml:"scheduler_interval"`
	IngestInterval      time.Duration `yaml:"ingest_interval"`
	QualityInterval     time.Duration `yaml:"quality_interval"`
	ProbeConcurrency    int           `yaml:"probe_concurrency"`
	// ReconcileInterval 是 Edge 尝试日志与 usage_logs 逐条对账（R1–R4）的间隔
	ReconcileInterval time.Duration `yaml:"reconcile_interval"`
	// BalanceReconcileInterval 是买家余额恒等式核对的间隔
	BalanceReconcileInterval time.Duration `yaml:"balance_reconcile_interval"`
	// AdminAddr 是 worker 的健康检查与指标端口（/healthz、/debug/vars）
	AdminAddr string `yaml:"admin_addr"`
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
		"TM_UPDATE_REPOSITORY":        &cfg.Update.Repository,
		"TM_UPDATE_GITHUB_TOKEN":      &cfg.Update.GitHubToken,
		"TM_UPDATE_RUNTIME_DIR":       &cfg.Update.RuntimeDir,
		"TM_ENV":                      &cfg.Env,
		"TM_HTTP_ADDR":                &cfg.HTTP.Addr,
		"TM_HTTP_WEB_DIR":             &cfg.HTTP.WebDir,
		"TM_HTTP_ADMIN_WEB_DIR":       &cfg.HTTP.AdminWebDir,
		"TM_DATABASE_DSN":             &cfg.Database.DSN,
		"TM_DATABASE_TIME_ZONE":       &cfg.Database.TimeZone,
		"TM_REDIS_ADDR":               &cfg.Redis.Addr,
		"TM_REDIS_PASSWORD":           &cfg.Redis.Password,
		"TM_SUB2API_BASE_URL":         &cfg.Sub2API.BaseURL,
		"TM_SUB2API_ADMIN_API_KEY":    &cfg.Sub2API.AdminAPIKey,
		"TM_SUB2API_REPLICA_DSN":      &cfg.Sub2API.ReplicaDSN,
		"TM_CRYPTO_ACTIVE_KEY_ID":     &cfg.Crypto.ActiveKeyID,
		"TM_CRYPTO_HASH_PEPPER":       &cfg.Crypto.HashPepper,
		"TM_AUTH_JWT_SECRET":          &cfg.Auth.JWTSecret,
		"TM_BOOTSTRAP_ADMIN_EMAIL":    &cfg.Auth.BootstrapAdminEmail,
		"TM_BOOTSTRAP_ADMIN_PASSWORD": &cfg.Auth.BootstrapAdminPassword,
		"TM_SUB2API_GATEWAY_URL":      &cfg.Sub2API.GatewayURL,
		"TM_EDGE_ADDR":                &cfg.Edge.Addr,
		"TM_EDGE_PUBLIC_URL":          &cfg.Edge.PublicURL,
		"TM_EDGE_ADMIN_ADDR":          &cfg.Edge.AdminAddr,
		"TM_WORKER_ADMIN_ADDR":        &cfg.Worker.AdminAddr,
		"TM_EDGE_SPOOL_DIR":           &cfg.Edge.SpoolDir,
	}
	for name, dst := range str {
		if v, ok := lookup(name); ok {
			*dst = v
		}
	}
	if v, ok := lookup("TM_EDGE_TRUSTED_PROXIES"); ok {
		cfg.Edge.TrustedProxies = splitList(v)
	}
	if v, ok := lookup("TM_HTTP_TRUSTED_PROXIES"); ok {
		cfg.HTTP.TrustedProxies = splitList(v)
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

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func applyDefaults(cfg *Config) {
	if cfg.Update.Repository == "" {
		cfg.Update.Repository = "yx-token/token-market"
	}
	if cfg.Update.RuntimeDir == "" {
		cfg.Update.RuntimeDir = "data/runtime"
	}
	if cfg.Env == "" {
		cfg.Env = "dev"
	}
	if cfg.HTTP.Addr == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.Database.MaxConns <= 0 {
		cfg.Database.MaxConns = 20
	}
	if cfg.Database.TimeZone == "" {
		cfg.Database.TimeZone = "Asia/Shanghai"
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
	if cfg.Worker.SchedulerInterval <= 0 {
		cfg.Worker.SchedulerInterval = 30 * time.Second
	}
	if cfg.Worker.IngestInterval <= 0 {
		cfg.Worker.IngestInterval = 15 * time.Second
	}
	if cfg.Worker.QualityInterval <= 0 {
		cfg.Worker.QualityInterval = 30 * time.Second
	}
	if cfg.Worker.ProbeConcurrency <= 0 {
		cfg.Worker.ProbeConcurrency = 4
	}
	if cfg.Worker.ReconcileInterval <= 0 {
		cfg.Worker.ReconcileInterval = 10 * time.Minute
	}
	if cfg.Worker.BalanceReconcileInterval <= 0 {
		cfg.Worker.BalanceReconcileInterval = 30 * time.Minute
	}
	if cfg.Worker.AdminAddr == "" {
		cfg.Worker.AdminAddr = ":8092"
	}
	if cfg.Auth.TokenTTL <= 0 {
		cfg.Auth.TokenTTL = 24 * time.Hour
	}
	if cfg.Sub2API.GatewayURL == "" {
		cfg.Sub2API.GatewayURL = cfg.Sub2API.BaseURL
	}
	if cfg.Sub2API.ShadowEmailDomain == "" {
		cfg.Sub2API.ShadowEmailDomain = "shadow.token-market.internal"
	}
	if cfg.Edge.Addr == "" {
		cfg.Edge.Addr = ":8090"
	}
	if cfg.Edge.AdminAddr == "" {
		cfg.Edge.AdminAddr = ":8091"
	}
	if cfg.Edge.ConnectTimeout <= 0 {
		cfg.Edge.ConnectTimeout = 3 * time.Second
	}
	if cfg.Edge.FirstByteTimeout <= 0 {
		cfg.Edge.FirstByteTimeout = 30 * time.Second
	}
	if cfg.Edge.MaxBodyBytes <= 0 {
		cfg.Edge.MaxBodyBytes = 32 << 20
	}
	if cfg.Edge.BreakerFailures <= 0 {
		cfg.Edge.BreakerFailures = 5
	}
	if cfg.Edge.BreakerCooldown <= 0 {
		cfg.Edge.BreakerCooldown = 30 * time.Second
	}
	if cfg.Edge.SpoolDir == "" {
		cfg.Edge.SpoolDir = "data/edge-spool"
	}
	if cfg.Edge.InvalidKeyLimit == 0 {
		cfg.Edge.InvalidKeyLimit = 30
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
