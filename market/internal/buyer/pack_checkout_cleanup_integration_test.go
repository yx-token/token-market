//go:build integration

package buyer

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/yx-token/idlicat/market/internal/edgecfg"
	"github.com/yx-token/idlicat/market/internal/testutil"
	"github.com/redis/go-redis/v9"
)

func TestPackCheckoutCleanupAfterAnotherWorkerFinishes(t *testing.T) {
	addr := os.Getenv("TM_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TM_TEST_REDIS_ADDR not set")
	}
	dsn, db := testutil.NewDatabase(t)
	ctx := t.Context()
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 9})
	t.Cleanup(func() { _ = rdb.Close() })
	s := &KeyService{DB: db, Edge: edgecfg.Store{Redis: rdb}}
	if _, err := db.Exec(ctx, `
		CREATE TABLE listings(id bigint PRIMARY KEY, mode text, stock_sold int, updated_at timestamptz);
		CREATE TABLE pack_checkouts(id bigint PRIMARY KEY, listing_id bigint, buyer_id bigint, recharge_id bigint,
			status text, pack_id bigint, error_reason text, fulfilled_at timestamptz);
		CREATE TABLE quota_packs(id bigint PRIMARY KEY, recharge_id bigint, buyer_key_id bigint);
		CREATE TABLE buyer_keys(id bigint PRIMARY KEY, user_id bigint, key_hash bytea, status text,
			deleted_at timestamptz, updated_at timestamptz, disabled_reason text, limits jsonb DEFAULT '{}',
			limit_blocked_until timestamptz, limit_block_reason text, config_version bigint DEFAULT 1);
		INSERT INTO listings VALUES(1,'pack',3,now());
		INSERT INTO pack_checkouts(id,listing_id,buyer_id,recharge_id,status,pack_id)
			VALUES(1,1,7,101,'pending',NULL),(2,1,7,102,'fulfilled',10);
		INSERT INTO quota_packs VALUES(10,102,3);`); err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 3; id++ {
		hash := edgecfg.HashKey(dsn + strconv.FormatInt(id, 10))
		if _, err := db.Exec(ctx, `INSERT INTO buyer_keys(id,user_id,key_hash,status) VALUES($1,7,decode($2,'hex'),'active')`, id, hash); err != nil {
			t.Fatal(err)
		}
		if err := s.Edge.Put(ctx, hash, edgecfg.KeyConfig{BuyerKeyID: id, Status: "active"}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Edge.Delete(context.Background(), hash) })
	}
	// 两个 worker 均已创建 Key，但第一个先完成失败收尾；第二个仍必须撤销自己的孤立 Key。
	for _, keyID := range []int64{1, 2, 1, 2} {
		if err := s.failPendingPackCheckout(ctx, 1, keyID, errors.New("pack write failed")); err != nil {
			t.Fatal(err)
		}
	}
	// 提交结果不确定时，已关联成功包的 Key 和库存必须保留。
	if err := s.failPendingPackCheckout(ctx, 2, 3, errors.New("commit acknowledgement lost")); err != nil {
		t.Fatal(err)
	}
	var sold int
	if err := db.QueryRow(ctx, "SELECT stock_sold FROM listings WHERE id=1").Scan(&sold); err != nil || sold != 2 {
		t.Fatalf("only the failed checkout releases one reservation: sold=%d err=%v", sold, err)
	}
	for id := int64(1); id <= 3; id++ {
		var status string
		if err := db.QueryRow(ctx, "SELECT status FROM buyer_keys WHERE id=$1", id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		cfg, err := s.Edge.Get(ctx, edgecfg.HashKey(dsn+strconv.FormatInt(id, 10)))
		if id < 3 {
			if status != "deleted" || !errors.Is(err, edgecfg.ErrNotFound) || cfg != nil {
				t.Fatalf("orphan %d remains usable: status=%s cfg=%+v err=%v", id, status, cfg, err)
			}
		} else if status != "active" || err != nil || cfg == nil {
			t.Fatalf("fulfilled pack key must remain usable: status=%s cfg=%+v err=%v", status, cfg, err)
		}
	}
}
