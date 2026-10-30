//go:build integration

package idlecat

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestNativeProxyBindingsAndConcurrentDisable(t *testing.T) {
	dsn := os.Getenv("TM_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TM_TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("idlecat_proxy_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.Exec("DROP DATABASE " + name + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE users(id bigint PRIMARY KEY, email text);
		CREATE TABLE proxies(id bigint PRIMARY KEY, seller_id bigint, name text, protocol text, host text, port int,
			status text, username text, region text, sub2api_proxy_id bigint, assigned_upstream_id bigint,
			created_at timestamptz DEFAULT now(), connection_report jsonb, quality_report jsonb,
			connection_checked_at timestamptz, quality_checked_at timestamptz);
		CREATE TABLE upstreams(id bigint PRIMARY KEY, proxy_id bigint REFERENCES proxies(id), status text);
		INSERT INTO users VALUES(7,'seller@test.local');
		INSERT INTO proxies(id,seller_id,name,protocol,host,port,status,username,region,sub2api_proxy_id)
			VALUES(17,7,'proxy','http','proxy.test',8080,'assigned','user','sg',91);
		INSERT INTO upstreams VALUES(3,17,'failed'),(8,17,'removing'),(10,17,'removed');`)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{Market: db}
	rows, err := store.ListProxies(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("native proxy list: %+v %v", rows, err)
	}
	proxy := rows[0]
	ids, ok := proxy["assigned_upstream_ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != float64(3) || ids[1] != float64(8) || proxy["assigned_upstream_count"] != int64(2) || proxy["assigned_upstream_id"] != int64(3) || proxy["region"] != "sg" || proxy["sub2api_proxy_id"] != int64(91) || proxy["seller_email"] != "seller@test.local" {
		t.Fatalf("native proxy response is incomplete or trusts legacy: %+v", proxy)
	}
	for _, disabled := range []bool{true, false} {
		if err := store.SetProxyDisabled(ctx, 17, disabled); err == nil {
			t.Fatal("native write accepted bound proxy with NULL legacy field")
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE upstreams SET status='removed'; UPDATE proxies SET assigned_upstream_id=3`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyDisabled(ctx, 17, true); err != nil {
		t.Fatalf("stale legacy ID blocked disable: %v", err)
	}
	if err := store.SetProxyDisabled(ctx, 17, false); err != nil {
		t.Fatal(err)
	}
	writer, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	var lockedID int64
	if err := writer.QueryRowContext(ctx, `SELECT id FROM proxies WHERE id=17 FOR UPDATE`).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `INSERT INTO upstreams VALUES(11,17,'draft')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- store.SetProxyDisabled(ctx, 17, true) }()
	for {
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query='SELECT id FROM proxies WHERE id = $1 FOR UPDATE')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("native write did not wait for binding lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("native disable missed binding committed while waiting for proxy lock")
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=17`).Scan(&status); err != nil || status != "available" {
		t.Fatalf("native disable changed bound proxy: %s %v", status, err)
	}
}
