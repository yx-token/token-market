// Package mail 发送事务邮件（注册验证码等）。
package mail

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// Sender 发信接口；生产用 Resend，测试可注入假实现。
type Sender interface {
	Send(ctx context.Context, to, subject, html string) error
}

// Disabled 未配置发信时使用，调用会返回明确错误。
type Disabled struct{}

func (Disabled) Send(context.Context, string, string, string) error {
	return ErrNotConfigured
}

// ErrNotConfigured 表示未配置邮件服务。
var ErrNotConfigured = errNotConfigured{}

type errNotConfigured struct{}

func (errNotConfigured) Error() string { return "email sender is not configured" }

var codeRe = regexp.MustCompile(`>(\d{6})<`)

// DevLog 本地测试用：不真发信，把收件人与验证码打到日志。
type DevLog struct{ Log *slog.Logger }

func (d DevLog) Send(_ context.Context, to, subject, html string) error {
	code := ""
	if m := codeRe.FindStringSubmatch(html); len(m) == 2 {
		code = m[1]
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	log.Info("dev mail (not sent)", "to", to, "subject", subject, "code", code, "preview", strings.TrimSpace(stripTags(html)))
	return nil
}

func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	out := re.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(out), " ")
}
