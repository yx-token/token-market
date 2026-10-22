// Package edge 是买家 API 的入口：鉴权买家 Key、调度选跳、转发到引擎 Gateway。
//
// 分工（方案 1）：Edge = 调度脑；引擎 = 协议/上游/组内选号与额度。
// 人民币账本不在热路径入账，由 worker 消费 usage_logs 与 edge_attempts。
// 调度入口见 BuildSchedulePlan；契约见 docs/dev/edge-engine-contract.md。
// 热路径除活动包选包外不访问市场库。
package edge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/yx-token/token-market/market/internal/edgecfg"
)

// Cache 是 Edge 进程内的路由配置缓存。Redis 不可用时继续使用已缓存的配置。
type Cache struct {
	store edgecfg.Store
	log   *slog.Logger

	mu    sync.RWMutex
	keys  map[string]*edgecfg.KeyConfig
	skip  map[int64]bool
	ready bool
}

func NewCache(store edgecfg.Store, log *slog.Logger) *Cache {
	return &Cache{store: store, log: log, keys: map[string]*edgecfg.KeyConfig{}, skip: map[int64]bool{}}
}

// Load 全量加载。冷启动时必须成功一次才对外服务。
func (c *Cache) Load(ctx context.Context) error {
	keys, err := c.store.LoadAll(ctx)
	if err != nil {
		return err
	}
	skip, err := c.store.SkipList(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.keys, c.skip, c.ready = keys, skip, true
	c.mu.Unlock()
	return nil
}

func (c *Cache) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

func (c *Cache) Key(hash string) *edgecfg.KeyConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.keys[hash]
}

func (c *Cache) Skipped(listingID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.skip[listingID]
}

func (c *Cache) refreshKey(ctx context.Context, hash string) {
	cfg, err := c.store.Get(ctx, hash)
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err == nil:
		c.keys[hash] = cfg
	case errors.Is(err, edgecfg.ErrNotFound):
		delete(c.keys, hash)
	default:
		c.log.Warn("edge refresh key", "err", err)
	}
}

func (c *Cache) refreshSkip(ctx context.Context) {
	skip, err := c.store.SkipList(ctx)
	if err != nil {
		c.log.Warn("edge refresh skip list", "err", err)
		return
	}
	c.mu.Lock()
	c.skip = skip
	c.mu.Unlock()
}

// Watch 订阅变更通知并定期全量刷新，直到 ctx 结束。
func (c *Cache) Watch(ctx context.Context, fullInterval time.Duration) {
	sub := c.store.Subscribe(ctx)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel()
	ticker := time.NewTicker(fullInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-msgs:
			if !ok {
				return
			}
			if m.Payload == "skip" {
				c.refreshSkip(ctx)
			} else {
				c.refreshKey(ctx, m.Payload)
			}
		case <-ticker.C:
			if err := c.Load(ctx); err != nil {
				c.log.Warn("edge full reload failed, keep cached config", "err", err)
			}
		}
	}
}

// breaker 以挂单（或默认分组）为单位熔断，所有买家共享。
type breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	state     map[string]*breakerState
	now       func() time.Time
}

type breakerState struct {
	failures  int
	openUntil time.Time
	probing   bool
}

func newBreaker(threshold int, cooldown time.Duration) *breaker {
	return &breaker{threshold: threshold, cooldown: cooldown, state: map[string]*breakerState{}, now: time.Now}
}

// Allow 判断是否放行；熔断到期后只放行一个试探请求。
func (b *breaker) Allow(label string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[label]
	if st == nil || st.openUntil.IsZero() {
		return true
	}
	if b.now().Before(st.openUntil) {
		return false
	}
	if st.probing {
		return false
	}
	st.probing = true
	return true
}

func (b *breaker) Success(label string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, label)
}

// Failure 记录一次失败，返回是否因此进入熔断。
func (b *breaker) Failure(label string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[label]
	if st == nil {
		st = &breakerState{}
		b.state[label] = st
	}
	st.failures++
	if st.probing || st.failures >= b.threshold {
		st.openUntil = b.now().Add(b.cooldown)
		st.probing = false
		return true
	}
	return false
}
