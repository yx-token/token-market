// Package hostmail 是宿主（sub2api 引擎）向闲猫市场提供「发信」的接口。
//
// 嵌入模式下，C 端注册验证码走管理端「设置 → 邮件」里的 SMTP，与引擎账号验证同一套配置；
// 「安全与认证 → 邮箱验证」打开后市场注册也要求验证码。
//
// 宿主启动时 SetBridge；未注册时（市场独立进程）市场仍可用自身 settings / 环境缺省（dev/Resend）。
//
// 本包是叶子包：不得 import market/internal/... 或 backend。
package hostmail

import (
	"context"
	"errors"
	"sync"
)

// Status 宿主当前发信能力。
type Status struct {
	VerifyEnabled bool   // 对应 email_verify_enabled
	SMTPReady     bool   // 已配置 SMTP 主机
	From          string // 发件人展示（from_name <from> 或 from）
}

// Bridge 宿主注入的只读能力。
type Bridge struct {
	Status func(ctx context.Context) (Status, error)
	Send   func(ctx context.Context, to, subject, html string) error
}

// ErrNoBridge 宿主未注册发信桥。
var ErrNoBridge = errors.New("hostmail: email bridge is not registered by the host")

var (
	mu sync.RWMutex
	b  *Bridge
)

// SetBridge 由宿主在启动时调用；传零值或 Status/Send 为空取消注册。
func SetBridge(bridge Bridge) {
	mu.Lock()
	defer mu.Unlock()
	if bridge.Status == nil || bridge.Send == nil {
		b = nil
		return
	}
	cp := bridge
	b = &cp
}

// Active 是否已注册宿主发信桥。
func Active() bool {
	mu.RLock()
	defer mu.RUnlock()
	return b != nil
}

// LoadStatus 读取宿主状态；未注册返回 ErrNoBridge。
func LoadStatus(ctx context.Context) (Status, error) {
	mu.RLock()
	bridge := b
	mu.RUnlock()
	if bridge == nil {
		return Status{}, ErrNoBridge
	}
	return bridge.Status(ctx)
}

// Send 经宿主 SMTP 发信；未注册返回 ErrNoBridge。
func Send(ctx context.Context, to, subject, html string) error {
	mu.RLock()
	bridge := b
	mu.RUnlock()
	if bridge == nil {
		return ErrNoBridge
	}
	return bridge.Send(ctx, to, subject, html)
}

// Sender 实现 mail.Sender，把发送交给宿主。
type Sender struct{}

func (Sender) Send(ctx context.Context, to, subject, html string) error {
	return Send(ctx, to, subject, html)
}
