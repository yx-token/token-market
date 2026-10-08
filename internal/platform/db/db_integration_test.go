//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/internal/testutil"
	"github.com/DueGin/token-market/migrations"
)

func TestMigrateIsIdempotent(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()

	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("expected migrations to be applied")
	}
	again, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run should apply nothing: %v %v", again, err)
	}
	var state string
	if err := pool.QueryRow(ctx, "SELECT guard_state FROM compat_state WHERE id = 1").Scan(&state); err != nil || state != "safe_mode" {
		t.Fatalf("compat_state seed row: %q %v", state, err)
	}
}

func TestMigrateRejectsModifiedMigration(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()

	v1 := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (id int);")}}
	if _, err := db.Migrate(ctx, pool, v1); err != nil {
		t.Fatal(err)
	}
	v2 := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (id bigint);")}}
	_, err := db.Migrate(ctx, pool, v2)
	if err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("expected modified-migration error, got %v", err)
	}
}

func TestMigrateRollsBackFailedFile(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()

	bad := fstest.MapFS{"0001_bad.sql": {Data: []byte("CREATE TABLE b (id int); SELECT * FROM missing_table;")}}
	if _, err := db.Migrate(ctx, pool, bad); err == nil {
		t.Fatal("expected failure")
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('b') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("failed migration must roll back completely: exists=%v err=%v", exists, err)
	}
}
