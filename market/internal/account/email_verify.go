package account

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/mail"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	registerCodeTTL       = 10 * time.Minute
	registerCodeCooldown  = 60 * time.Second
	registerCodeKeyPrefix = "tm:regcode:"
	registerCoolKeyPrefix = "tm:regcool:"
)

var (
	ErrCodeRequired   = httpx.BadRequest("CODE_REQUIRED", "email verification code is required")
	ErrInvalidCode    = httpx.BadRequest("INVALID_CODE", "invalid or expired verification code")
	ErrCodeCooldown   = httpx.TooMany("CODE_COOLDOWN", "please wait before requesting another code")
	ErrEmailNotReady  = httpx.Unavailable("EMAIL_NOT_CONFIGURED", "email verification is not configured")
	ErrSendCodeFailed = httpx.Unavailable("SEND_CODE_FAILED", "failed to send verification email")
)

// ConfigureEmail 接入 Redis；发信方式由 settings 表（管理端）或 boot 环境缺省决定，可热更新。
func (s *Service) ConfigureEmail(rdb *redis.Client, boot mail.Boot, log *slog.Logger) {
	s.rdb = rdb
	s.emailBoot = boot
	s.log = log
}

func (s *Service) emailReady(ctx context.Context) (bool, mail.Sender, error) {
	if s.rdb == nil {
		return false, nil, nil
	}
	if s.fixedMailer != nil {
		return s.fixedEnabled, s.fixedMailer, nil
	}
	return mail.ResolveSender(ctx, s.db, s.kr, s.emailBoot, s.log)
}

// ConfigureEmailFixed 单测用：固定发信器，不读 settings。
func (s *Service) ConfigureEmailFixed(rdb *redis.Client, sender mail.Sender, enabled bool) {
	s.rdb = rdb
	s.fixedMailer = sender
	s.fixedEnabled = enabled
}

// EmailVerifyEnabled 当前是否要求注册邮箱验证（读 settings，无需重启）。
func (s *Service) EmailVerifyEnabled(ctx context.Context) bool {
	ok, _, err := s.emailReady(ctx)
	return err == nil && ok
}

// GetEmailSettings / UpdateEmailSettings 供管理后台读写。
func (s *Service) GetEmailSettings(ctx context.Context) (mail.View, error) {
	return mail.LoadView(ctx, s.db, s.emailBoot)
}

func (s *Service) UpdateEmailSettings(ctx context.Context, patch mail.Patch) (mail.View, error) {
	return mail.Update(ctx, s.db, s.kr, s.emailBoot, patch)
}

// SendRegisterCode 向邮箱发送 6 位注册验证码。
func (s *Service) SendRegisterCode(ctx context.Context, email string) error {
	ready, mailer, err := s.emailReady(ctx)
	if err != nil {
		return err
	}
	if !ready || mailer == nil {
		return ErrEmailNotReady
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return err
	}
	taken, err := s.emailTaken(ctx, email)
	if err != nil {
		return err
	}
	if taken {
		return ErrEmailTaken
	}
	coolKey := registerCoolKeyPrefix + email
	ok, err := s.rdb.SetNX(ctx, coolKey, "1", registerCodeCooldown).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrCodeCooldown
	}
	code, err := randomDigits(6)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, registerCodeKeyPrefix+email, code, registerCodeTTL).Err(); err != nil {
		return err
	}
	subject := "闲猫注册验证码"
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><body style="font-family:-apple-system,BlinkMacSystemFont,'PingFang SC','Microsoft YaHei',sans-serif;background:#fafafa;padding:24px">
  <div style="max-width:420px;margin:0 auto;background:#fff;border:1px solid rgba(0,0,0,.08);border-radius:16px;padding:28px 24px">
    <div style="font-size:13px;font-weight:800;color:#888;margin-bottom:8px">闲猫·闲置Token交易市场</div>
    <h1 style="margin:0 0 12px;font-size:20px;color:#1f1f1f">邮箱验证</h1>
    <p style="margin:0 0 18px;font-size:14px;line-height:1.6;color:#555">你正在注册闲猫账号，请在 10 分钟内输入以下验证码：</p>
    <p style="margin:0 0 18px;font-size:32px;font-weight:800;letter-spacing:8px;text-align:center;color:#1f1f1f">%s</p>
    <p style="margin:0;font-size:12px;color:#999;line-height:1.5">如非本人操作，请忽略本邮件。</p>
  </div>
</body></html>`, code)
	if err := mailer.Send(ctx, email, subject, html); err != nil {
		_, _ = s.rdb.Del(ctx, registerCodeKeyPrefix+email, coolKey).Result()
		return ErrSendCodeFailed
	}
	return nil
}

func (s *Service) consumeRegisterCode(ctx context.Context, email, code string) error {
	ready, _, err := s.emailReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrCodeRequired
	}
	key := registerCodeKeyPrefix + email
	got, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return ErrInvalidCode
	}
	if err != nil {
		return err
	}
	if got != code {
		return ErrInvalidCode
	}
	_ = s.rdb.Del(ctx, key, registerCoolKeyPrefix+email).Err()
	return nil
}

func (s *Service) emailTaken(ctx context.Context, email string) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(1) FROM users WHERE email = $1`, email).Scan(&n)
	return n > 0, err
}

func randomDigits(n int) (string, error) {
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}
