package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	alertSettingsKey   = "support_alerts"
	deliveryAlertKey   = "email_delivery"
)

// AlertConfig 客服工作台预警与自动结束配置（存 settings 表，不依赖用户管理）。
type AlertConfig struct {
	Emails                []string `json:"emails"`
	ReplyTimeoutMinutes   int      `json:"reply_timeout_minutes"`
	PendingAlertThreshold int      `json:"pending_alert_threshold"`
	IdleCloseMinutes      int      `json:"idle_close_minutes"`
	EmailEnabled          bool     `json:"email_enabled"`
}

// AlertDeliveryStatus 最近一次预警发信结果（给工作台展示，不进配置表）。
type AlertDeliveryStatus struct {
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
	LastSentAt  *time.Time `json:"last_sent_at,omitempty"`
}

// AlertSettingsView GET /support/alert-settings 响应。
type AlertSettingsView struct {
	AlertConfig
	Delivery AlertDeliveryStatus `json:"delivery"`
}

func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		Emails:                []string{},
		ReplyTimeoutMinutes:   int(ReplyTimeout / time.Minute),
		PendingAlertThreshold: PendingAlertCount,
		IdleCloseMinutes:      int(IdleCloseTimeout / time.Minute),
		EmailEnabled:          true,
	}
}

func (c AlertConfig) ReplyTimeout() time.Duration {
	m := c.ReplyTimeoutMinutes
	if m <= 0 {
		m = int(ReplyTimeout / time.Minute)
	}
	return time.Duration(m) * time.Minute
}

func (c AlertConfig) IdleCloseTimeout() time.Duration {
	m := c.IdleCloseMinutes
	if m <= 0 {
		m = int(IdleCloseTimeout / time.Minute)
	}
	return time.Duration(m) * time.Minute
}

func (c AlertConfig) PendingThreshold() int {
	if c.PendingAlertThreshold <= 0 {
		return PendingAlertCount
	}
	return c.PendingAlertThreshold
}

func (c AlertConfig) NotifyEmails() []string {
	if !c.EmailEnabled {
		return nil
	}
	out := make([]string, 0, len(c.Emails))
	seen := map[string]struct{}{}
	for _, e := range c.Emails {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

func (c AlertConfig) Validate() error {
	if c.ReplyTimeoutMinutes < 1 || c.ReplyTimeoutMinutes > 24*60 {
		return errors.New("reply_timeout_minutes must be between 1 and 1440")
	}
	if c.PendingAlertThreshold < 1 || c.PendingAlertThreshold > 1000 {
		return errors.New("pending_alert_threshold must be between 1 and 1000")
	}
	if c.IdleCloseMinutes < 1 || c.IdleCloseMinutes > 24*60 {
		return errors.New("idle_close_minutes must be between 1 and 1440")
	}
	for _, e := range c.Emails {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, err := mail.ParseAddress(e); err != nil {
			return fmt.Errorf("invalid email: %s", e)
		}
	}
	return nil
}

// LoadAlertConfig 读取预警配置；缺失时返回默认值。
func (s *Service) LoadAlertConfig(ctx context.Context) (AlertConfig, error) {
	cfg := DefaultAlertConfig()
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, alertSettingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return DefaultAlertConfig(), fmt.Errorf("decode support alert settings: %w", err)
	}
	cfg.Emails = normalizeEmailList(cfg.Emails)
	if cfg.ReplyTimeoutMinutes <= 0 {
		cfg.ReplyTimeoutMinutes = DefaultAlertConfig().ReplyTimeoutMinutes
	}
	if cfg.PendingAlertThreshold <= 0 {
		cfg.PendingAlertThreshold = DefaultAlertConfig().PendingAlertThreshold
	}
	if cfg.IdleCloseMinutes <= 0 {
		cfg.IdleCloseMinutes = DefaultAlertConfig().IdleCloseMinutes
	}
	return cfg, nil
}

// SaveAlertConfig 校验并保存预警配置。
func (s *Service) SaveAlertConfig(ctx context.Context, cfg AlertConfig) (AlertConfig, error) {
	cfg.Emails = normalizeEmailList(cfg.Emails)
	if err := cfg.Validate(); err != nil {
		return cfg, httpx.BadRequest("INVALID_ALERT_SETTINGS", err.Error())
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	_, err = s.DB.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		alertSettingsKey, raw)
	return cfg, err
}

// LoadAlertSettingsView 配置 + 最近发信状态。
func (s *Service) LoadAlertSettingsView(ctx context.Context) (AlertSettingsView, error) {
	cfg, err := s.LoadAlertConfig(ctx)
	if err != nil {
		return AlertSettingsView{}, err
	}
	return AlertSettingsView{AlertConfig: cfg, Delivery: s.loadDeliveryStatus(ctx)}, nil
}

func (s *Service) loadDeliveryStatus(ctx context.Context) AlertDeliveryStatus {
	var meta []byte
	var updatedAt time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT meta, updated_at FROM support_alert_state WHERE key = $1`, deliveryAlertKey).Scan(&meta, &updatedAt)
	if err != nil {
		return AlertDeliveryStatus{}
	}
	var m struct {
		LastError  string     `json:"last_error"`
		LastSentAt *time.Time `json:"last_sent_at"`
		ErrorAt    *time.Time `json:"error_at"`
	}
	_ = json.Unmarshal(meta, &m)
	out := AlertDeliveryStatus{LastError: strings.TrimSpace(m.LastError), LastSentAt: m.LastSentAt, LastErrorAt: m.ErrorAt}
	if out.LastError != "" && out.LastErrorAt == nil {
		t := updatedAt
		out.LastErrorAt = &t
	}
	return out
}

// RecordAlertDeliveryOK 清除发信错误并记录成功时间。
func (s *Service) RecordAlertDeliveryOK(ctx context.Context) {
	now := time.Now().UTC()
	raw, _ := json.Marshal(map[string]any{"last_sent_at": now, "last_error": "", "error_at": nil})
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO support_alert_state (key, fired_at, meta, updated_at) VALUES ($1, NULL, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET meta = EXCLUDED.meta, updated_at = now()`, deliveryAlertKey, raw)
}

// RecordAlertDeliveryErr 记录发信失败原因，供工作台提示。
func (s *Service) RecordAlertDeliveryErr(ctx context.Context, sendErr error) {
	if sendErr == nil {
		return
	}
	now := time.Now().UTC()
	raw, _ := json.Marshal(map[string]any{
		"last_error": sendErr.Error(),
		"error_at":   now,
	})
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO support_alert_state (key, fired_at, meta, updated_at) VALUES ($1, NULL, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET
			meta = COALESCE(support_alert_state.meta, '{}'::jsonb) || EXCLUDED.meta,
			updated_at = now()`, deliveryAlertKey, raw)
}

func normalizeEmailList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		for _, part := range splitEmails(raw) {
			e := strings.TrimSpace(strings.ToLower(part))
			if e == "" {
				continue
			}
			if _, ok := seen[e]; ok {
				continue
			}
			seen[e] = struct{}{}
			out = append(out, e)
		}
	}
	return out
}

func splitEmails(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	replacer := strings.NewReplacer(",", " ", ";", " ", "\n", " ", "\r", " ", "\t", " ")
	return strings.Fields(replacer.Replace(s))
}
