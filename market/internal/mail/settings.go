package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/hostmail"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
)

const settingsKey = "email"
const apiKeyAAD = "email:resend-api-key"

// Mode 发信模式。
const (
	ModeOff    = "off"
	ModeDev    = "dev"    // 验证码打日志，不真发信
	ModeResend = "resend" // Resend API（独立进程兜底）
	ModeSMTP   = "smtp"   // 宿主系统设置 SMTP
)

// Boot 进程环境变量提供的缺省；仅当库里尚未保存过邮箱配置时生效。
type Boot struct {
	APIKey string
	From   string
}

// View 管理端回显（不含明文密钥）。
type View struct {
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
	From      string `json:"from"`
	HasAPIKey bool   `json:"has_api_key"`
	Source    string `json:"source"` // "host" | "settings" | "boot" | "none"
}

// Patch 部分更新；APIKey 为 nil 表示保留，空串表示清除。
type Patch struct {
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
	From    *string `json:"from"`
	APIKey  *string `json:"api_key"`
}

type stored struct {
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
	From      string `json:"from"`
	APIKeyEnc string `json:"api_key_enc,omitempty"`
}

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// BootFromEnv 把 TM_RESEND_* 规范成 Boot。
func BootFromEnv(apiKey, from string) Boot {
	return Boot{APIKey: strings.TrimSpace(apiKey), From: strings.TrimSpace(from)}
}

func (b Boot) inferred() (enabled bool, mode string) {
	key := strings.TrimSpace(b.APIKey)
	switch {
	case key == "" || strings.EqualFold(key, "off"):
		return false, ModeOff
	case strings.EqualFold(key, "dev"):
		return true, ModeDev
	default:
		return true, ModeResend
	}
}

// LoadView 读取当前邮箱验证配置。嵌入宿主时优先系统 SMTP；否则 settings / boot。
func LoadView(ctx context.Context, db DB, boot Boot) (View, error) {
	if hostmail.Active() {
		hs, err := hostmail.LoadStatus(ctx)
		if err != nil {
			return View{}, err
		}
		if hs.VerifyEnabled && hs.SMTPReady {
			return View{
				Enabled: true, Mode: ModeSMTP, From: hs.From, HasAPIKey: true, Source: "host",
			}, nil
		}
		if !hs.VerifyEnabled {
			return View{Enabled: false, Mode: ModeOff, From: hs.From, Source: "host"}, nil
		}
		// 已开验证但 SMTP 未配：允许回落到 boot/settings（本地 dev 日志）
	}
	st, ok, err := loadStored(ctx, db)
	if err != nil {
		return View{}, err
	}
	if ok {
		return View{
			Enabled:   st.Enabled,
			Mode:      normalizeMode(st.Mode),
			From:      st.From,
			HasAPIKey: st.APIKeyEnc != "",
			Source:    "settings",
		}, nil
	}
	enabled, mode := boot.inferred()
	hasKey := mode == ModeResend && strings.TrimSpace(boot.APIKey) != ""
	src := "none"
	if enabled || mode != ModeOff {
		src = "boot"
	}
	return View{
		Enabled:   enabled,
		Mode:      mode,
		From:      defaultFrom(boot.From),
		HasAPIKey: hasKey,
		Source:    src,
	}, nil
}

// Update 保存邮箱验证配置到 settings 表，立即对后续发信生效。
func Update(ctx context.Context, db DB, kr *crypto.Keyring, boot Boot, patch Patch) (View, error) {
	if hostmail.Active() {
		hs, err := hostmail.LoadStatus(ctx)
		if err != nil {
			return View{}, err
		}
		if hs.SMTPReady {
			return View{}, errors.New("embedded mode uses system SMTP; change under 设置 → 邮件 / 安全与认证")
		}
		// SMTP 未配时仍允许写入 settings / 使用 boot，方便本地
	}
	if kr == nil {
		return View{}, errors.New("keyring required")
	}
	st, ok, err := loadStored(ctx, db)
	if err != nil {
		return View{}, err
	}
	if !ok {
		enabled, mode := boot.inferred()
		st = stored{Enabled: enabled, Mode: mode, From: defaultFrom(boot.From)}
		if mode == ModeResend && strings.TrimSpace(boot.APIKey) != "" {
			enc, err := kr.SealString(strings.TrimSpace(boot.APIKey), apiKeyAAD)
			if err != nil {
				return View{}, err
			}
			st.APIKeyEnc = base64.StdEncoding.EncodeToString(enc)
		}
	}
	if patch.Enabled != nil {
		st.Enabled = *patch.Enabled
	}
	if patch.Mode != nil {
		st.Mode = normalizeMode(*patch.Mode)
	}
	if patch.From != nil {
		st.From = strings.TrimSpace(*patch.From)
	}
	if patch.APIKey != nil {
		key := strings.TrimSpace(*patch.APIKey)
		if key == "" {
			st.APIKeyEnc = ""
		} else {
			enc, err := kr.SealString(key, apiKeyAAD)
			if err != nil {
				return View{}, err
			}
			st.APIKeyEnc = base64.StdEncoding.EncodeToString(enc)
		}
	}
	st.Mode = normalizeMode(st.Mode)
	st.From = defaultFrom(st.From)
	if !st.Enabled {
		st.Mode = ModeOff
	}
	if err := validateStored(st); err != nil {
		return View{}, err
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return View{}, err
	}
	_, err = db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, settingsKey, raw)
	if err != nil {
		return View{}, err
	}
	return View{
		Enabled:   st.Enabled,
		Mode:      st.Mode,
		From:      st.From,
		HasAPIKey: st.APIKeyEnc != "",
		Source:    "settings",
	}, nil
}

// ResolveOpsSender 运营告警发信器：宿主 SMTP 已就绪即可（不依赖「注册邮箱验证」开关）。
// 无宿主桥时回退到 settings / boot（与 ResolveSender 相同）。
// 宿主 SMTP 已配但发信鉴权失败时，FailoverSender 会再试 boot/settings（本地 dev / Resend）。
func ResolveOpsSender(ctx context.Context, db DB, kr *crypto.Keyring, boot Boot, log *slog.Logger) (enabled bool, sender Sender, err error) {
	fallbackOn, fallback, ferr := resolveStoredOrBoot(ctx, db, kr, boot, log)
	if ferr != nil {
		return false, nil, ferr
	}
	if hostmail.Active() {
		hs, err := hostmail.LoadStatus(ctx)
		if err != nil {
			return false, nil, err
		}
		if hs.SMTPReady {
			// 仅回退到可真发信的 Resend；dev 日志通道不算成功，避免把未投递会话标成已预警。
			return true, FailoverSender{Primary: hostmail.Sender{}, Fallback: realMailFallback(fallbackOn, fallback), Log: log}, nil
		}
	}
	return fallbackOn, fallback, nil
}

func realMailFallback(on bool, s Sender) Sender {
	if !on || s == nil {
		return nil
	}
	if _, ok := s.(*Resend); ok {
		return s
	}
	return nil
}

// FailoverSender 主通道失败（常见：SMTP 535）时改走 Fallback。
type FailoverSender struct {
	Primary  Sender
	Fallback Sender
	Log      *slog.Logger
}

func (f FailoverSender) Send(ctx context.Context, to, subject, html string) error {
	if f.Primary == nil {
		if f.Fallback == nil {
			return ErrNotConfigured
		}
		return f.Fallback.Send(ctx, to, subject, html)
	}
	err := f.Primary.Send(ctx, to, subject, html)
	if err == nil || f.Fallback == nil || !smtpAuthFailed(err) {
		return err
	}
	log := f.Log
	if log == nil {
		log = slog.Default()
	}
	log.Warn("ops mail primary failed, trying fallback", "err", err, "to", to)
	return f.Fallback.Send(ctx, to, subject, html)
}

func smtpAuthFailed(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "smtp auth") ||
		strings.Contains(msg, "535") ||
		strings.Contains(msg, "authentication credentials invalid") ||
		strings.Contains(msg, "auth failed")
}

// ResolveSender 按当前配置构造发信器；未启用或不完整时返回 (false, Disabled, nil)。
func ResolveSender(ctx context.Context, db DB, kr *crypto.Keyring, boot Boot, log *slog.Logger) (enabled bool, sender Sender, err error) {
	if hostmail.Active() {
		hs, err := hostmail.LoadStatus(ctx)
		if err != nil {
			return false, nil, err
		}
		if hs.VerifyEnabled && hs.SMTPReady {
			return true, hostmail.Sender{}, nil
		}
		if !hs.VerifyEnabled {
			return false, Disabled{}, nil
		}
		// VerifyEnabled 但 SMTP 未就绪：继续走 boot/settings
	}
	return resolveStoredOrBoot(ctx, db, kr, boot, log)
}

func resolveStoredOrBoot(ctx context.Context, db DB, kr *crypto.Keyring, boot Boot, log *slog.Logger) (enabled bool, sender Sender, err error) {
	st, ok, err := loadStored(ctx, db)
	if err != nil {
		return false, nil, err
	}
	var mode, from, apiKey string
	var on bool
	if ok {
		on, mode, from = st.Enabled, normalizeMode(st.Mode), defaultFrom(st.From)
		if st.APIKeyEnc != "" {
			raw, decErr := base64.StdEncoding.DecodeString(st.APIKeyEnc)
			if decErr != nil {
				return false, nil, fmt.Errorf("decode email api key: %w", decErr)
			}
			if kr == nil {
				return false, nil, errors.New("keyring required")
			}
			apiKey, err = kr.OpenString(raw, apiKeyAAD)
			if err != nil {
				return false, nil, err
			}
		}
	} else {
		on, mode = boot.inferred()
		from = defaultFrom(boot.From)
		if mode == ModeResend {
			apiKey = strings.TrimSpace(boot.APIKey)
		}
	}
	if !on || mode == ModeOff {
		return false, Disabled{}, nil
	}
	switch mode {
	case ModeDev:
		return true, DevLog{Log: log}, nil
	case ModeResend:
		if apiKey == "" {
			return false, Disabled{}, nil
		}
		return true, NewResend(apiKey, from), nil
	default:
		return false, Disabled{}, nil
	}
}

func loadStored(ctx context.Context, db DB) (stored, bool, error) {
	var raw []byte
	err := db.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", settingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored{}, false, nil
	}
	if err != nil {
		return stored{}, false, err
	}
	var st stored
	if err := json.Unmarshal(raw, &st); err != nil {
		return stored{}, false, fmt.Errorf("decode email settings: %w", err)
	}
	st.Mode = normalizeMode(st.Mode)
	return st, true, nil
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeDev:
		return ModeDev
	case ModeResend:
		return ModeResend
	default:
		return ModeOff
	}
}

func defaultFrom(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return "闲猫 <onboarding@resend.dev>"
	}
	return from
}

func validateStored(st stored) error {
	switch st.Mode {
	case ModeOff, ModeDev, ModeResend:
	default:
		return errors.New("mode must be off, dev, or resend")
	}
	if st.Enabled && st.Mode == ModeOff {
		return errors.New("enabled requires mode dev or resend")
	}
	if st.Enabled && st.Mode == ModeResend && st.APIKeyEnc == "" {
		return errors.New("resend mode requires api_key")
	}
	if st.From == "" {
		return errors.New("from is required")
	}
	return nil
}
