// Package hostreg 是宿主（sub2api 引擎）向闲猫市场提供「注册邮箱域名策略」的接口。
//
// 管理端「安全与认证 → 邮箱域名白名单 / 非白名单限量注册」写在引擎 settings；
// 嵌入模式下市场发码与注册读取同一份配置，行为与引擎账号注册一致。
//
// 宿主启动时 SetSource；未注册时（市场独立进程）市场不限制域名。
//
// 本包是叶子包：不得 import market/internal/... 或 backend。
package hostreg

import (
	"context"
	"errors"
	"sync"
)

// Policy 某一时刻的注册邮箱域名策略快照。
type Policy struct {
	// Whitelist 已规范化的后缀项，例如 "@qq.com"、"*.edu.cn"。空表示不限制。
	Whitelist []string
	// DomainQuotaEnabled：白名单非空时，是否放行非白名单域名「每个根域名限 1 个账号」。
	// false 时非白名单域名一律拒绝。
	DomainQuotaEnabled bool
}

// Source 读取当前策略。实现应当只读且足够快。
type Source func(ctx context.Context) (Policy, error)

// ErrNoSource 宿主未注册注册策略来源。
var ErrNoSource = errors.New("hostreg: registration policy source is not registered by the host")

var (
	mu  sync.RWMutex
	src Source
)

// SetSource 由宿主在启动时调用；传 nil 取消注册。
func SetSource(s Source) {
	mu.Lock()
	src = s
	mu.Unlock()
}

// Load 返回当前策略；未注册来源时返回 ErrNoSource。
func Load(ctx context.Context) (Policy, error) {
	mu.RLock()
	s := src
	mu.RUnlock()
	if s == nil {
		return Policy{}, ErrNoSource
	}
	return s(ctx)
}
