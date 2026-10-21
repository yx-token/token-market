package testutil

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/compat"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/migrations"
)

// 模板仅在当前测试进程内共享，不跨进程/worktree 复用。随机库名和所有权清单
// 避免不同迁移集碰撞；TestMain 必须调用 CleanupTemplates 并将清理错误转为失败。
var processTemplates = newTemplateManager()

// NewMarketDatabase 克隆已执行本系统迁移的模板，不包含 bootstrap 或测试业务数据。
func NewMarketDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	return newTestDatabase(t, "market", func(ctx context.Context, dsn string) error {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer pool.Close()
		_, err = db.Migrate(ctx, pool, migrations.FS)
		return err
	})
}

// NewSub2APIDatabase 克隆指定版本迁移后的模板，不包含替身/账号等测试数据。
func NewSub2APIDatabase(t *testing.T, repo, ref string) (string, *pgxpool.Pool) {
	t.Helper()
	absolute, err := filepath.Abs(repo)
	if err != nil {
		t.Fatalf("absolute migration repo: %v", err)
	}
	return newTestDatabase(t, "sub2api\x00"+absolute+"\x00"+ref, func(ctx context.Context, dsn string) error {
		set, err := LoadGitMigrations(ctx, absolute, ref)
		if err != nil {
			return err
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
		return compat.ApplySub2APIMigrations(ctx, conn, set)
	})
}

// CleanupTemplates 删除本进程精确拥有的模板，绝不按前缀扫描/删除他人的库。
// 应在 TestMain 中 m.Run() 返回、全部测试库 cleanup 完成后调用。
func CleanupTemplates() error {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	return processTemplates.cleanup(ctx)
}

type templateInitializer func(context.Context, string) error

type templateEntry struct {
	ready chan struct{}
	name  string
	err   error
}

type templateManager struct {
	// lifecycle 允许并行 clone，并使 cleanup 等待所有初始化/克隆结束。
	lifecycle sync.RWMutex
	mu        sync.Mutex
	entries   map[string]*templateEntry
	owned     map[string]string // 精确的模板名 -> 管理库 DSN
	closed    bool
}

func newTemplateManager() *templateManager {
	return &templateManager{entries: make(map[string]*templateEntry), owned: make(map[string]string)}
}

func (m *templateManager) clone(ctx context.Context, base, key string, initialize templateInitializer) (string, string, error) {
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	if m.closed {
		return "", "", errors.New("template manager already cleaned up")
	}
	entry, err := m.template(ctx, base, key, initialize)
	if err != nil {
		return "", "", err
	}
	// ALLOW_CONNECTIONS false 后不会出现新连接。迁移连接已经关闭，但服务器
	// 清理连接可能尚未结束；仅终止本进程拥有模板上的连接并有限重试 55006。
	for {
		name, dsn, err := createDatabase(ctx, base, entry.name, "tm_t_")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55006" {
			return name, dsn, err
		}
		if err := m.disconnectTemplate(ctx, base, entry.name); err != nil {
			return "", "", err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", "", ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *templateManager) template(ctx context.Context, base, key string, initialize templateInitializer) (*templateEntry, error) {
	cacheKey := base + "\x00" + key
	m.mu.Lock()
	entry, exists := m.entries[cacheKey]
	if !exists {
		entry = &templateEntry{ready: make(chan struct{})}
		m.entries[cacheKey] = entry
	}
	m.mu.Unlock()
	if exists {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return entry, entry.err
		}
	}
	entry.name, entry.err = m.initialize(ctx, base, initialize)
	m.mu.Lock()
	if entry.err != nil {
		delete(m.entries, cacheKey) // 失败不缓存，后续调用可以重新初始化。
	}
	close(entry.ready)
	m.mu.Unlock()
	return entry, entry.err
}

func (m *templateManager) initialize(ctx context.Context, base string, initialize templateInitializer) (string, error) {
	name, dsn, err := createDatabase(ctx, base, "", "tm_tpl_")
	if err != nil {
		return "", err
	}
	// CREATE 后立即记录所有权；即使迁移/封库失败，也能由 TestMain 兜底清理。
	m.mu.Lock()
	m.owned[name] = base
	m.mu.Unlock()
	err = initialize(ctx, dsn)
	if err == nil {
		err = m.sealTemplate(ctx, base, name)
	}
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
		defer cancel()
		if cleanupErr := dropDatabase(cleanupCtx, base, name); cleanupErr != nil {
			return "", errors.Join(err, fmt.Errorf("cleanup failed template %s: %w", name, cleanupErr))
		}
		m.mu.Lock()
		delete(m.owned, name)
		m.mu.Unlock()
		return "", err
	}
	return name, nil
}

func (m *templateManager) sealTemplate(ctx context.Context, base, name string) error {
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS false"); err != nil {
		return err
	}
	return m.disconnectTemplate(ctx, base, name)
}

func (m *templateManager) disconnectTemplate(ctx context.Context, base, name string) error {
	m.mu.Lock()
	ownedBase, owns := m.owned[name]
	m.mu.Unlock()
	if !owns || ownedBase != base {
		return errors.New("refusing to disconnect unowned template")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	_, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid<>pg_backend_pid()", name)
	return err
}

func (m *templateManager) cleanup(ctx context.Context) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.closed = true
	m.mu.Lock()
	names := make([]string, 0, len(m.owned))
	for name := range m.owned {
		names = append(names, name)
	}
	sort.Strings(names)
	m.mu.Unlock()
	var errs []error
	for _, name := range names {
		m.mu.Lock()
		base := m.owned[name]
		m.mu.Unlock()
		if err := dropDatabase(ctx, base, name); err != nil {
			errs = append(errs, fmt.Errorf("drop template %s: %w", name, err))
			continue
		}
		m.mu.Lock()
		delete(m.owned, name)
		m.mu.Unlock()
	}
	return errors.Join(errs...)
}
