//go:build integration

package db_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func TestSellerProxySharingMigrationPreservesLegacyBindings(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	legacy := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0043" {
			continue
		}
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		legacy[name] = &fstest.MapFile{Data: content}
	}
	if _, err := db.Migrate(ctx, pool, legacy); err != nil {
		t.Fatal(err)
	}
	var sellerID, upstreamID, removedID, proxyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('legacy-proxy@test.local','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type) VALUES($1,'openai','oauth') RETURNING id`, sellerID).Scan(&upstreamID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO proxies(seller_id,name,protocol,host,port,status,assigned_upstream_id)
		VALUES($1,'legacy','http','legacy.local',8080,'assigned',$2) RETURNING id`, sellerID, upstreamID).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,proxy_id)
		VALUES($1,'openai','oauth','removed',$2) RETURNING id`, sellerID, proxyID).Scan(&removedID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var boundID int64
	if err := pool.QueryRow(ctx, `SELECT proxy_id FROM upstreams WHERE id=$1`, upstreamID).Scan(&boundID); err != nil || boundID != proxyID {
		t.Fatalf("legacy binding lost: proxy=%d err=%v", boundID, err)
	}
	var removedProxy *int64
	if err := pool.QueryRow(ctx, `SELECT proxy_id FROM upstreams WHERE id=$1`, removedID).Scan(&removedProxy); err != nil || removedProxy != nil {
		t.Fatalf("removed upstream still bound: %v err=%v", removedProxy, err)
	}
	var status string
	var assignedID int64
	if err := pool.QueryRow(ctx, `SELECT status,assigned_upstream_id FROM proxies WHERE id=$1`, proxyID).Scan(&status, &assignedID); err != nil || status != "assigned" || assignedID != upstreamID {
		t.Fatalf("legacy projection inconsistent: status=%s id=%d err=%v", status, assignedID, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'openai','oauth',$2)`, sellerID, proxyID); err != nil {
		t.Fatalf("migrated seller proxy cannot be shared: %v", err)
	}
}
