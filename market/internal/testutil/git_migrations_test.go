package testutil

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/compat"
)

func TestMigrationCacheConcurrentCopiesAndRefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	repo := migrationGitRepo(t)
	var reads atomic.Int32
	cache := newMigrationCache(func(ctx context.Context, repo, ref string) (*compat.MigrationSet, error) {
		reads.Add(1)
		return compat.LoadGitMigrations(ctx, repo, ref)
	})
	const callers = 8
	sets := make([]*compat.MigrationSet, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			sets[i], errs[i] = cache.load(ctx, repo, "first")
		})
	}
	wg.Wait()
	if reads.Load() != 1 {
		t.Fatalf("same ref loaded %d times, want 1", reads.Load())
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if string(sets[i].Contents["001.sql"]) != "SELECT 1;\n" {
			t.Fatalf("caller %d got wrong migration: %+v", i, sets[i])
		}
	}
	sets[0].Desc = "changed"
	sets[0].Names[0] = "renamed.sql"
	sets[0].Contents["001.sql"][0] = 'X'
	delete(sets[0].Contents, "001.sql")
	again, err := cache.load(ctx, repo, "first")
	if err != nil {
		t.Fatal(err)
	}
	if again.Desc != "git first" || again.Names[0] != "001.sql" || string(again.Contents["001.sql"]) != "SELECT 1;\n" || string(sets[1].Contents["001.sql"]) != "SELECT 1;\n" {
		t.Fatal("caller mutation contaminated cached or concurrent copy")
	}
	second, err := cache.load(ctx, repo, "second")
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 || string(second.Contents["001.sql"]) != "SELECT 2;\n" {
		t.Fatalf("refs shared cache contents: reads=%d set=%+v", reads.Load(), second)
	}
	other := migrationGitRepo(t)
	if _, err := cache.load(ctx, other, "first"); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 3 {
		t.Fatal("different repo incorrectly shared cache")
	}
}

func TestMigrationCacheFailureIsRetriedAndWaitIsCancellable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	repo := migrationGitRepo(t)
	cache := newMigrationCache(compat.LoadGitMigrations)
	if _, err := cache.load(ctx, repo, "later"); err == nil {
		t.Fatal("missing ref unexpectedly succeeded")
	}
	gitIn(t, repo, "tag", "later", "first")
	if _, err := cache.load(ctx, repo, "later"); err != nil {
		t.Fatalf("failed ref remained cached: %v", err)
	}

	started, release := make(chan struct{}), make(chan struct{})
	blocked := newMigrationCache(func(context.Context, string, string) (*compat.MigrationSet, error) {
		close(started)
		<-release
		return &compat.MigrationSet{Desc: "ready", Contents: map[string][]byte{}}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := blocked.load(ctx, repo, "blocked")
		done <- err
	}()
	<-started
	waitCtx, waitCancel := context.WithCancel(ctx)
	waitCancel()
	_, err := blocked.load(waitCtx, repo, "blocked")
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cache waiter: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func migrationGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "user.name", "Template DB Test")
	gitIn(t, repo, "config", "user.email", "template@test.invalid")
	dir := filepath.Join(repo, compat.MigrationsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1", "2"} {
		if err := os.WriteFile(filepath.Join(dir, "001.sql"), []byte("SELECT "+value+";\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, repo, "add", ".")
		gitIn(t, repo, "commit", "-q", "-m", "migration "+value)
		if value == "1" {
			gitIn(t, repo, "tag", "first")
		} else {
			gitIn(t, repo, "tag", "second")
		}
	}
	return repo
}

func gitIn(t *testing.T, repo string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	// 完全独立的临时仓库；不继承用户签名/hooks 等配置。
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
