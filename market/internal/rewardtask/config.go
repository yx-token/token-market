package rewardtask

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	settingsKey    = "reward_partner"
	secretAAD      = "settings:reward_partner"
	DefaultBaseURL = "https://loxo.cc"
)

var appKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// storedConfig 是 settings 表 reward_partner 的 JSON；app_secret 只存密文。
type storedConfig struct {
	Enabled    bool   `json:"enabled"`
	BaseURL    string `json:"base_url"`
	AppKey     string `json:"app_key"`
	SecretEnc  []byte `json:"app_secret_enc,omitempty"`
	SDKEnabled bool   `json:"sdk_enabled"`
}

// Config 是解密后的运行配置，只在服务端使用。
type Config struct {
	Enabled    bool
	BaseURL    string
	AppKey     string
	Secret     string
	SDKEnabled bool
	UpdatedAt  *time.Time
}

// Ready 是否可以调用 loxo（启用且凭证齐全）。
func (c Config) Ready() bool { return c.Enabled && c.BaseURL != "" && c.AppKey != "" && c.Secret != "" }

// SDKScriptURL 浏览任务 SDK 的地址；未启用 SDK 时为空。
func (c Config) SDKScriptURL() string {
	if !c.Ready() || !c.SDKEnabled {
		return ""
	}
	return c.BaseURL + "/reward-task-sdk.js"
}

// ConfigView 是管理端看到的配置，不含 secret。
type ConfigView struct {
	Enabled    bool       `json:"enabled"`
	BaseURL    string     `json:"base_url"`
	AppKey     string     `json:"app_key"`
	SecretSet  bool       `json:"secret_set"`
	SDKEnabled bool       `json:"sdk_enabled"`
	UpdatedAt  *time.Time `json:"updated_at"`
	Summary    Summary    `json:"summary"`
}

// ConfigInput 是管理端保存的整份配置；AppSecret 为空表示不修改。
type ConfigInput struct {
	Enabled    bool   `json:"enabled"`
	BaseURL    string `json:"base_url"`
	AppKey     string `json:"app_key"`
	AppSecret  string `json:"app_secret"`
	SDKEnabled bool   `json:"sdk_enabled"`
}

func errConfig(msg string) error { return httpx.BadRequest("INVALID_REWARD_CONFIG", msg) }

// normalizeBaseURL 只接受站点根地址（SDK 以脚本的 origin 作为心跳地址，带路径的部署本来就用不了）。
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultBaseURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errConfig("base_url must be a site origin such as https://loxo.cc")
	}
	host := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "https":
	case "http":
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return "", errConfig("base_url must use https (http is only allowed for localhost)")
		}
	default:
		return "", errConfig("base_url must use https")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func validSecret(s string) bool {
	if len(s) < 8 || len(s) > 256 {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func (s *Service) loadStored(ctx context.Context) (storedConfig, *time.Time, error) {
	st := storedConfig{BaseURL: DefaultBaseURL, SDKEnabled: true}
	var raw []byte
	var updated time.Time
	err := s.DB.QueryRow(ctx, `SELECT value, updated_at FROM settings WHERE key = $1`, settingsKey).Scan(&raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil, nil
	}
	if err != nil {
		return st, nil, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, nil, err
	}
	if st.BaseURL == "" {
		st.BaseURL = DefaultBaseURL
	}
	return st, &updated, nil
}

// LoadConfig 读取并解密配置；未保存时返回默认值（未启用）。
func (s *Service) LoadConfig(ctx context.Context) (Config, error) {
	st, updated, err := s.loadStored(ctx)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Enabled: st.Enabled, BaseURL: st.BaseURL, AppKey: st.AppKey, SDKEnabled: st.SDKEnabled, UpdatedAt: updated}
	if len(st.SecretEnc) > 0 {
		if s.Keyring == nil {
			return cfg, errors.New("reward task keyring is not configured")
		}
		if cfg.Secret, err = s.Keyring.OpenString(st.SecretEnc, secretAAD); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// SaveConfig 校验并保存整份配置。
func (s *Service) SaveConfig(ctx context.Context, in ConfigInput) error {
	st, _, err := s.loadStored(ctx)
	if err != nil {
		return err
	}
	base, err := normalizeBaseURL(in.BaseURL)
	if err != nil {
		return err
	}
	appKey := strings.TrimSpace(in.AppKey)
	if appKey != "" && !appKeyPattern.MatchString(appKey) {
		return errConfig("app_key must be 1-64 characters of [A-Za-z0-9_-]")
	}
	secret := strings.TrimSpace(in.AppSecret)
	if secret != "" {
		if !validSecret(secret) {
			return errConfig("app_secret must be 8-256 characters without spaces")
		}
		if s.Keyring == nil {
			return errors.New("reward task keyring is not configured")
		}
		if st.SecretEnc, err = s.Keyring.SealString(secret, secretAAD); err != nil {
			return err
		}
	}
	if in.Enabled && (appKey == "" || len(st.SecretEnc) == 0) {
		return errConfig("app_key and app_secret are required before enabling")
	}
	st.Enabled, st.BaseURL, st.AppKey, st.SDKEnabled = in.Enabled, base, appKey, in.SDKEnabled
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, settingsKey, raw)
	if err == nil {
		s.storeScriptOrigin(Config{Enabled: st.Enabled, BaseURL: st.BaseURL, AppKey: st.AppKey, Secret: secretMarker(st.SecretEnc), SDKEnabled: st.SDKEnabled})
	}
	return err
}

// secretMarker 只用于判断 Ready()，不解密。
func secretMarker(enc []byte) string {
	if len(enc) == 0 {
		return ""
	}
	return "set"
}

// View 管理端配置视图（含票据 / 事件状态汇总）。
func (s *Service) View(ctx context.Context) (*ConfigView, error) {
	st, updated, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	sum, err := s.summary(ctx)
	if err != nil {
		return nil, err
	}
	return &ConfigView{
		Enabled: st.Enabled, BaseURL: st.BaseURL, AppKey: st.AppKey, SecretSet: len(st.SecretEnc) > 0,
		SDKEnabled: st.SDKEnabled, UpdatedAt: updated, Summary: sum,
	}, nil
}
