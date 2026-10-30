//go:build integration

package scheduling

import (
	"context"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func TestCompleteRemovalPreservesSharedProxy(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var sellerID, proxyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('removal-proxy@test.local','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO proxies(seller_id,name,protocol,host,port) VALUES($1,'shared','http','removal.local',8080) RETURNING id`, sellerID).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	upstreamIDs := make([]int64, 2)
	for index := range upstreamIDs {
		if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,proxy_id)
			VALUES($1,'openai','oauth','removing',$2) RETURNING id`, sellerID, proxyID).Scan(&upstreamIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	runner := &Runner{DB: pool}
	for index, upstreamID := range upstreamIDs {
		if err := runner.completeRemoval(ctx, upstreamID); err != nil {
			t.Fatal(err)
		}
		var status string
		var assignedID *int64
		if err := pool.QueryRow(ctx, `SELECT status,assigned_upstream_id FROM proxies WHERE id=$1`, proxyID).Scan(&status, &assignedID); err != nil {
			t.Fatal(err)
		}
		if index == 0 && (status != "assigned" || assignedID == nil || *assignedID != upstreamIDs[1]) {
			t.Fatalf("first removal released shared proxy: status=%s assigned=%v", status, assignedID)
		}
		if index == 1 && (status != "available" || assignedID != nil) {
			t.Fatalf("last removal did not release proxy: status=%s assigned=%v", status, assignedID)
		}
		var boundID *int64
		if err := pool.QueryRow(ctx, `SELECT proxy_id FROM upstreams WHERE id=$1`, upstreamID).Scan(&boundID); err != nil || boundID != nil {
			t.Fatalf("removed upstream still bound: %v err=%v", boundID, err)
		}
	}
}
