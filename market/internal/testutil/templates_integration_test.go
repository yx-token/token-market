//go:build integration

package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yx-token/idlicat/market/internal/compat"
	"github.com/yx-token/idlicat/market/internal/platform/db"
	"github.com/yx-token/idlicat/market/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := CleanupTemplates(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup test database templates: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func TestTemplateConcurrentClonesAreIsolated(t *testing.T) {
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	manager := testTemplateManager(t)
	var initialized atomic.Int32
	initialize := func(ctx context.Context, dsn string) error {
		initialized.Add(1)
		return initializeRows(ctx, dsn, "shared")
	}
	const clones = 6
	names, dsns, errs := make([]string, clones), make([]string, clones), make([]error, clones)
	var wg sync.WaitGroup
	for i := range clones {
		wg.Go(func() {
			names[i], dsns[i], errs[i] = manager.clone(ctx, base, "same", initialize)
		})
	}
	wg.Wait()
	// 先登记所有成功 clone 的清理，再处理任一失败；否则 t.Fatal 会遗留其他库。
	for i := range clones {
		if errs[i] == nil {
			registerCloneDrop(t, base, names[i])
		}
	}
	seen := map[string]bool{}
	for i := range clones {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[names[i]] {
			t.Fatalf("clones reused database %s", names[i])
		}
		seen[names[i]] = true
		pool := openClonePool(t, dsns[i])
		var seed string
		if err := pool.QueryRow(ctx, "SELECT value FROM sample WHERE id=1").Scan(&seed); err != nil || seed != "shared" {
			t.Fatalf("clone seed=%q error=%v", seed, err)
		}
		var id int64
		if err := pool.QueryRow(ctx, "INSERT INTO sample(value) VALUES ($1) RETURNING id", fmt.Sprint(i)).Scan(&id); err != nil || id != 2 {
			t.Fatalf("cloned sequence id=%d error=%v", id, err)
		}
		var rows int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM sample").Scan(&rows); err != nil || rows != 2 {
			t.Fatalf("other clone polluted rows=%d error=%v", rows, err)
		}
	}
	if initialized.Load() != 1 {
		t.Fatalf("parallel clones initialized template %d times", initialized.Load())
	}
	name, dsn, err := manager.clone(ctx, base, "same", initialize)
	if err != nil {
		t.Fatal(err)
	}
	pool := clonePool(t, base, name, dsn)
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sample").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("clone writes polluted template rows=%d error=%v", rows, err)
	}
	assertTemplateSealed(t, ctx, base, manager)
}

func TestTemplateManagersKeepDifferentMigrationsAndOwnership(t *testing.T) {
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	first, second := testTemplateManager(t), testTemplateManager(t)
	for _, spec := range []struct {
		manager *templateManager
		value   string
	}{{first, "first migration"}, {second, "second migration"}} {
		name, dsn, err := spec.manager.clone(ctx, base, "same-key", func(ctx context.Context, dsn string) error {
			return initializeRows(ctx, dsn, spec.value)
		})
		if err != nil {
			t.Fatal(err)
		}
		pool := clonePool(t, base, name, dsn)
		var value string
		if err := pool.QueryRow(ctx, "SELECT value FROM sample WHERE id=1").Scan(&value); err != nil || value != spec.value {
			t.Fatalf("manager got another migration: %q %v", value, err)
		}
	}
	firstName, secondName := templateName(t, first), templateName(t, second)
	if firstName == secondName {
		t.Fatal("independent managers shared template name")
	}
	if err := first.cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	assertDatabaseExists(t, ctx, base, firstName, false)
	assertDatabaseExists(t, ctx, base, secondName, true)
	name, dsn, err := second.clone(ctx, base, "same-key", nil)
	if err != nil {
		t.Fatalf("one manager cleanup broke the other: %v", err)
	}
	clonePool(t, base, name, dsn)
	if err := second.cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	assertDatabaseExists(t, ctx, base, secondName, false)
}

func TestTemplateFailedInitializationCleansUpAndRetries(t *testing.T) {
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	manager := testTemplateManager(t)
	var failedName string
	expected := errors.New("migration failed")
	_, _, err := manager.clone(ctx, base, "retry", func(_ context.Context, dsn string) error {
		u, err := url.Parse(dsn)
		if err != nil {
			return err
		}
		failedName = u.Path[1:]
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("initialize error lost: %v", err)
	}
	assertDatabaseExists(t, ctx, base, failedName, false)
	manager.mu.Lock()
	owned := len(manager.owned)
	manager.mu.Unlock()
	if owned != 0 {
		t.Fatal("failed template retained after successful cleanup")
	}
	name, dsn, err := manager.clone(ctx, base, "retry", func(ctx context.Context, dsn string) error {
		return initializeRows(ctx, dsn, "retried")
	})
	if err != nil {
		t.Fatalf("failed initialization cached: %v", err)
	}
	clonePool(t, base, name, dsn)
}

func TestCreateFailureCleansAmbiguousCommitAndPreservesDuplicate(t *testing.T) {
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	// 已存在的随机候选模拟 CREATE 在服务器提交后，客户端收到超时/丢失确认。
	name, _, err := createDatabase(ctx, base, "", "tm_t_")
	if err != nil {
		t.Fatal(err)
	}
	registerCloneDrop(t, base, name)
	err = cleanupFailedCreate(base, name, context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("original CREATE error lost: %v", err)
	}
	assertDatabaseExists(t, ctx, base, name, false)

	name, _, err = createDatabase(ctx, base, "", "tm_t_")
	if err != nil {
		t.Fatal(err)
	}
	registerCloneDrop(t, base, name)
	duplicate := &pgconn.PgError{Code: "42P04", Message: "database already exists"}
	if err := cleanupFailedCreate(base, name, duplicate); !errors.Is(err, duplicate) {
		t.Fatalf("duplicate CREATE error lost: %v", err)
	}
	assertDatabaseExists(t, ctx, base, name, true)
}

func TestTemplateCloningDisconnectsOnlyItsOwnLeftoverConnection(t *testing.T) {
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	manager := testTemplateManager(t)
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	var leftover *pgx.Conn
	name, dsn, err := manager.clone(ctx, base, "leftover", func(ctx context.Context, dsn string) error {
		var err error
		leftover, err = pgx.Connect(ctx, dsn)
		return err // 模拟初始化方忘记关闭连接，只允许终止自己模板上的连接。
	})
	if leftover != nil {
		defer func() { _ = leftover.Close(context.WithoutCancel(ctx)) }()
	}
	if err != nil {
		t.Fatal(err)
	}
	clonePool(t, base, name, dsn)
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("unrelated connection terminated: %v", err)
	}
	assertTemplateSealed(t, ctx, base, manager)
}

func TestMarketTemplateMatchesFreshMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	_, fresh := NewDatabase(t)
	if _, err := db.Migrate(ctx, fresh, migrations.FS); err != nil {
		t.Fatal(err)
	}
	_, cloned := NewMarketDatabase(t)
	freshState, clonedState := databaseState(t, ctx, fresh), databaseState(t, ctx, cloned)
	if !reflect.DeepEqual(freshState, clonedState) {
		for key, value := range freshState {
			if clonedState[key] != value {
				t.Errorf("fresh/clone differ at %s\nfresh=%s\nclone=%s", key, value, clonedState[key])
			}
		}
		if len(freshState) != len(clonedState) {
			t.Errorf("fresh/clone snapshot sizes differ: %d/%d", len(freshState), len(clonedState))
		}
	}
	// 确认克隆迁移记录可供 app.Build 再次迁移时安全跳过。
	if applied, err := db.Migrate(ctx, cloned, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("clone migrations not idempotent: applied=%v err=%v", applied, err)
	}
}

// 自建临时 git 仓库覆盖事务迁移和 *_notx.sql（CONCURRENTLY），不写外部 sub2api。
func TestSub2APITemplateMatchesFreshMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	repo := migrationGitRepo(t)
	dir := filepath.Join(repo, compat.MigrationsDir)
	files := map[string]string{
		"001.sql":      "CREATE TABLE sample(id bigserial PRIMARY KEY, value text NOT NULL); INSERT INTO sample(value) VALUES ('seed');",
		"002_notx.sql": "CREATE INDEX CONCURRENTLY sample_value_idx ON sample(value);",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "schema and nontransactional migration")
	gitIn(t, repo, "tag", "schema")
	set, err := LoadGitMigrations(ctx, repo, "schema")
	if err != nil {
		t.Fatal(err)
	}
	dsn, fresh := NewDatabase(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	err = compat.ApplySub2APIMigrations(ctx, conn, set)
	_ = conn.Close(context.WithoutCancel(ctx))
	if err != nil {
		t.Fatal(err)
	}
	_, cloned := NewSub2APIDatabase(t, repo, "schema")
	if got, want := databaseState(t, ctx, cloned), databaseState(t, ctx, fresh); !reflect.DeepEqual(got, want) {
		t.Fatalf("sub2api template differs from fresh migrations: clone=%v fresh=%v", got, want)
	}
}

func initializeRows(ctx context.Context, dsn, seed string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, "CREATE TABLE sample(id bigserial PRIMARY KEY, value text NOT NULL)"); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, "INSERT INTO sample(value) VALUES ($1)", seed)
	return err
}

func testTemplateManager(t *testing.T) *templateManager {
	t.Helper()
	manager := newTemplateManager()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
		defer cancel()
		if err := manager.cleanup(ctx); err != nil {
			t.Errorf("cleanup manager: %v", err)
		}
	})
	return manager
}

func registerCloneDrop(t *testing.T, base, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
		defer cancel()
		if err := dropDatabase(ctx, base, name); err != nil {
			t.Errorf("drop clone: %v", err)
		}
	})
}

func clonePool(t *testing.T, base, name, dsn string) *pgxpool.Pool {
	t.Helper()
	registerCloneDrop(t, base, name)
	return openClonePool(t, dsn)
}

func openClonePool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	var err error
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func templateName(t *testing.T, manager *templateManager) string {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.owned) != 1 {
		t.Fatalf("want exactly one owned template, got %d", len(manager.owned))
	}
	for name := range manager.owned {
		return name
	}
	return ""
}

func assertTemplateSealed(t *testing.T, ctx context.Context, base string, manager *templateManager) {
	t.Helper()
	name := templateName(t, manager)
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	var allowed bool
	if err := admin.QueryRow(ctx, "SELECT datallowconn FROM pg_database WHERE datname=$1", name).Scan(&allowed); err != nil || allowed {
		t.Fatalf("template accepts connections: %v %v", allowed, err)
	}
	dsn, err := databaseDSN(base, name)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err == nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		t.Fatal("sealed template connection unexpectedly succeeded")
	}
	var active int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=$1", name).Scan(&active); err != nil || active != 0 {
		t.Fatalf("template still connected: count=%d err=%v", active, err)
	}
}

func assertDatabaseExists(t *testing.T, ctx context.Context, base, name string, want bool) {
	t.Helper()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&exists); err != nil || exists != want {
		t.Fatalf("database %s exists=%v want=%v err=%v", name, exists, want, err)
	}
}

// 比较列/约束/索引、所有迁移 checksum、所有静态 seed 和序列状态。
// applied_at/created_at/updated_at 自然继承模板的迁移执行时间，去掉这些时间元数据。
func databaseState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	state := map[string]string{}
	queries := map[string]string{
		"columns":     `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY table_name, ordinal_position),'[]'::jsonb)::text FROM (SELECT table_name, column_name, ordinal_position, data_type, udt_name, is_nullable, column_default FROM information_schema.columns WHERE table_schema='public') r`,
		"constraints": `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY relation, name),'[]'::jsonb)::text FROM (SELECT conrelid::regclass::text AS relation, conname AS name, pg_get_constraintdef(oid) AS definition FROM pg_constraint WHERE connamespace='public'::regnamespace) r`,
		"indexes":     `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY tablename,indexname),'[]'::jsonb)::text FROM (SELECT tablename,indexname,indexdef FROM pg_indexes WHERE schemaname='public') r`,
	}
	for key, query := range queries {
		var value string
		if err := pool.QueryRow(ctx, query).Scan(&value); err != nil {
			t.Fatal(err)
		}
		state[key] = value
	}
	rows, err := pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var value string
		query := "SELECT coalesce(jsonb_agg(value ORDER BY value::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(r)-'applied_at'-'created_at'-'updated_at' AS value FROM " + pgx.Identifier{"public", table}.Sanitize() + " r) seed"
		if err := pool.QueryRow(ctx, query).Scan(&value); err != nil {
			t.Fatal(err)
		}
		state["rows/"+table] = value
	}
	rows, err = pool.Query(ctx, "SELECT sequencename FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename")
	if err != nil {
		t.Fatal(err)
	}
	sequences, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range sequences {
		var value string
		if err := pool.QueryRow(ctx, "SELECT to_jsonb(r)::text FROM (SELECT last_value,is_called FROM "+pgx.Identifier{"public", sequence}.Sanitize()+") r").Scan(&value); err != nil {
			t.Fatal(err)
		}
		state["sequence/"+sequence] = value
	}
	return state
}
