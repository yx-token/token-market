package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

// compatGate 依据 worker 中兼容自检写入的 compat_state 决定是否允许同步写 sub2api。
// 结果缓存几秒，避免每次写入都查库。
type compatGate struct {
	db  *pgxpool.Pool
	ttl time.Duration

	mu      sync.Mutex
	checked time.Time
	state   string
	reason  string
}

func (g *compatGate) Check(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Since(g.checked) > g.ttl {
		var reason *string
		if err := g.db.QueryRow(ctx, "SELECT guard_state, reason FROM compat_state WHERE id = 1").Scan(&g.state, &reason); err != nil {
			return fmt.Errorf("read compat state: %w", err)
		}
		g.reason = ""
		if reason != nil {
			g.reason = *reason
		}
		g.checked = time.Now()
	}
	if g.state == "normal" {
		return nil
	}
	return fmt.Errorf("%w: %w", adminclient.ErrWritesPaused,
		httpx.Unavailable("SUB2API_SAFE_MODE", "upstream infrastructure is in safe mode, please retry later"))
}
