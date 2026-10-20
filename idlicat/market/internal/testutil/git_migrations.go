package testutil

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/yx-token/idlicat/market/internal/compat"
)

var processMigrations = newMigrationCache(compat.LoadGitMigrations)

// LoadGitMigrations 按绝对仓库路径/ref 在测试进程内缓存读取成功的迁移集。
// 返回深拷贝，调用者改题/校验数据不会污染后续测试。仅用于测试，生产仍读原路径。
func LoadGitMigrations(ctx context.Context, repo, ref string) (*compat.MigrationSet, error) {
	absolute, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	return processMigrations.load(ctx, absolute, ref)
}

type migrationLoader func(context.Context, string, string) (*compat.MigrationSet, error)

type migrationEntry struct {
	ready chan struct{}
	set   *compat.MigrationSet
	err   error
}

type migrationCache struct {
	mu      sync.Mutex
	entries map[string]*migrationEntry
	loader  migrationLoader
}

func newMigrationCache(loader migrationLoader) *migrationCache {
	return &migrationCache{entries: make(map[string]*migrationEntry), loader: loader}
}

func (c *migrationCache) load(ctx context.Context, repo, ref string) (*compat.MigrationSet, error) {
	key := repo + "\x00" + ref
	c.mu.Lock()
	entry, exists := c.entries[key]
	if !exists {
		entry = &migrationEntry{ready: make(chan struct{})}
		c.entries[key] = entry
	}
	c.mu.Unlock()
	if exists {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return copyMigrationSet(entry.set), entry.err
		}
	}
	entry.set, entry.err = c.loader(ctx, repo, ref)
	c.mu.Lock()
	if entry.err != nil {
		delete(c.entries, key)
	}
	close(entry.ready)
	c.mu.Unlock()
	return copyMigrationSet(entry.set), entry.err
}

func copyMigrationSet(set *compat.MigrationSet) *compat.MigrationSet {
	if set == nil {
		return nil
	}
	copied := &compat.MigrationSet{Desc: set.Desc, Names: append([]string(nil), set.Names...), Contents: make(map[string][]byte, len(set.Contents))}
	for name, content := range set.Contents {
		copied.Contents[name] = append([]byte(nil), content...)
	}
	return copied
}
