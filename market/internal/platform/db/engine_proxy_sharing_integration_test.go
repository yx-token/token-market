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

func TestEngineProxySharingMigrationPreservesLegacyMultiBindings(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	legacy := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0049" {
			continue
		}
		raw, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		legacy[name] = &fstest.MapFile{Data: raw}
	}
	if _, err := db.Migrate(ctx, pool, legacy); err != nil {
		t.Fatal(err)
	}
	var sellerA, sellerB, proxyID int64
	for email, target := range map[string]*int64{"engine-proxy-a@test.local": &sellerA, "engine-proxy-b@test.local": &sellerB} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, email).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO proxies(name,protocol,host,port,is_shared,sub2api_proxy_id) VALUES('old shared','http','old-shared.test',8080,true,9001) RETURNING id`).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	for _, sellerID := range []int64{sellerA, sellerB} {
		if _, err := pool.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'openai','oauth',$2)`, sellerID, proxyID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var engineID, marketID int64
	var shared bool
	if err := pool.QueryRow(ctx, `SELECT proxy_id,market_proxy_id,is_shared FROM engine_proxy_sharing WHERE proxy_id=9001`).Scan(&engineID, &marketID, &shared); err != nil || engineID != 9001 || marketID != proxyID || !shared {
		t.Fatalf("legacy shared proxy mapping lost: engine=%d market=%d shared=%v err=%v", engineID, marketID, shared, err)
	}
	var bindings int
	var legacyBit bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM upstreams WHERE proxy_id=$1 AND status<>'removed'`, proxyID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT is_shared FROM proxies WHERE id=$1`, proxyID).Scan(&legacyBit); err != nil {
		t.Fatal(err)
	}
	if bindings != 2 || legacyBit {
		t.Fatalf("mapping should preserve both bindings and clear legacy bit: bindings=%d legacy=%v", bindings, legacyBit)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'openai','oauth',$2)`, sellerA, proxyID); err != nil {
		t.Fatalf("mapped legacy shared proxy must continue accepting bindings: %v", err)
	}
}
