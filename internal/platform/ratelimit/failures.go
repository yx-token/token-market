// Package ratelimit 提供进程内的失败次数限制（无效 Key 枚举、登录爆破）。多实例部署时各实例独立计数。
package ratelimit

import (
	"sync"
	"time"
)

// Failures 在固定窗口内统计某个键（IP、邮箱等）的失败次数，超过上限后在窗口剩余时间内拒绝。
type Failures struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*entry
	Now     func() time.Time
}

type entry struct {
	start    time.Time
	failures int
}

// NewFailures 创建限制器；limit <= 0 表示不限制。
func NewFailures(limit int, window time.Duration) *Failures {
	return &Failures{limit: limit, window: window, entries: map[string]*entry{}, Now: time.Now}
}

// Blocked 判断该键在当前窗口内是否已达到失败上限。
func (l *Failures) Blocked(key string) bool {
	if l == nil || l.limit <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	return e != nil && l.Now().Sub(e.start) < l.window && e.failures >= l.limit
}

// Fail 记录一次失败。
func (l *Failures) Fail(key string) {
	if l == nil || l.limit <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	if len(l.entries) > 100_000 {
		for k, e := range l.entries {
			if now.Sub(e.start) >= l.window {
				delete(l.entries, k)
			}
		}
	}
	e := l.entries[key]
	if e == nil || now.Sub(e.start) >= l.window {
		e = &entry{start: now}
		l.entries[key] = e
	}
	e.failures++
}

// Reset 清除某个键的失败记录（例如登录成功后）。
func (l *Failures) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
