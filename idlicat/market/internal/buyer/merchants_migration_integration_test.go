//go:build integration

package buyer

import (
	"context"
	"testing"

	"github.com/yx-token/idlicat/market/internal/testutil"
	"github.com/yx-token/idlicat/market/migrations"
)

func TestMerchantMigrationPreservesExistingKeysAndPendingCheckouts(t *testing.T) {
	_, db := testutil.NewDatabase(t)
	ctx := context.Background()
	// 最小升级前结构，迁移本身负责新清单及历史数据回填。
	_, err := db.Exec(ctx, `
		CREATE TABLE users(id bigint PRIMARY KEY);
		CREATE TABLE listings(id bigint PRIMARY KEY, seller_id bigint);
		CREATE TABLE buyer_keys(id bigint PRIMARY KEY, user_id bigint, status text);
		CREATE TABLE buyer_key_hops(key_id bigint, listing_id bigint);
		CREATE TABLE pack_checkouts(buyer_id bigint, listing_id bigint, status text);
		INSERT INTO users VALUES(1),(2),(3),(4),(10),(11);
		INSERT INTO listings VALUES(100,10),(101,10),(102,11);
		INSERT INTO buyer_keys VALUES(1,1,'active'),(2,1,'disabled'),(3,2,'deleted');
		INSERT INTO buyer_key_hops VALUES(1,100),(1,101),(2,100),(3,102);
		INSERT INTO pack_checkouts VALUES(3,102,'pending'),(4,100,'cancelled'),(4,101,'failed');`)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.FS.ReadFile("0036_buyer_merchants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `SELECT user_id, seller_id FROM buyer_merchants ORDER BY user_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var pairs [][2]int64
	for rows.Next() {
		var pair [2]int64
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0] != [2]int64{1, 10} || pairs[1] != [2]int64{3, 11} {
		t.Fatalf("must deduplicate existing keys, preserve pending purchases, and exclude deleted/cancelled records: %v", pairs)
	}
}
