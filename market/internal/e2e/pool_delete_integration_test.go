//go:build integration

package e2e

import (
	"fmt"
	"testing"
)

// 删除默认分组：启用中拒绝；关闭后软删除本地行、删除 sub2api 分组、回收指向它的内部 key，名称可以重用。
func TestPoolDelete(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")

	var pool struct {
		ID             int64  `json:"id"`
		Sub2APIGroupID *int64 `json:"sub2api_group_id"`
	}
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-gone", "platform": "anthropic", "sell_multiplier": 0.8}, &pool)
	if pool.Sub2APIGroupID == nil {
		t.Fatalf("pool should be synced to sub2api on create: %+v", pool)
	}
	groupID := *pool.Sub2APIGroupID

	buyer := e.newUser("buyer@market.test")
	var key struct {
		Detail idOnly `json:"detail"`
	}
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "pool", "route": map[string]any{"type": "default", "pool": "tm-pool-gone"}}, &key)
	e.drain()
	var internalKeys int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM internal_keys WHERE buyer_key_id = $1 AND sub2api_group_id = $2 AND status = 'active'",
		key.Detail.ID, groupID), &internalKeys)
	if internalKeys != 1 {
		t.Fatalf("active internal keys for pool group = %d, want 1", internalKeys)
	}

	// 启用中不能删
	admin.expect(409, "DELETE", fmt.Sprintf("/api/admin/pools/%d", pool.ID), nil)
	admin.must("PUT", fmt.Sprintf("/api/admin/pools/%d", pool.ID), map[string]any{"enabled": false}, nil)
	admin.must("DELETE", fmt.Sprintf("/api/admin/pools/%d", pool.ID), nil, nil)
	// sub2api 旧分组删掉之前不能同名重建，否则会按名称找回旧分组
	admin.expect(409, "POST", "/api/admin/pools", map[string]any{"name": "tm-pool-gone", "platform": "anthropic", "sell_multiplier": 0.8})
	e.drain()

	// 本地软删除，列表与编辑都看不到
	var deleted bool
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT deleted_at IS NOT NULL FROM pools WHERE id = $1", pool.ID), &deleted)
	if !deleted {
		t.Fatal("pool row should be soft-deleted")
	}
	var pools []struct {
		ID int64 `json:"id"`
	}
	admin.must("GET", "/api/admin/pools", nil, &pools)
	for _, p := range pools {
		if p.ID == pool.ID {
			t.Fatalf("deleted pool still listed: %+v", pools)
		}
	}
	admin.expect(404, "PUT", fmt.Sprintf("/api/admin/pools/%d", pool.ID), map[string]any{"enabled": true})
	admin.expect(404, "POST", fmt.Sprintf("/api/admin/pools/%d/sync", pool.ID), nil)
	admin.expect(404, "DELETE", fmt.Sprintf("/api/admin/pools/%d", pool.ID), nil)

	// sub2api 分组已删除
	var groupGone bool
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT deleted_at IS NOT NULL FROM groups WHERE id = $1", groupID), &groupGone)
	if !groupGone {
		t.Fatalf("sub2api group %d should be deleted", groupID)
	}

	// 买家 Key 指向已删除分组的内部 key 被回收；重新开通不会再为它建 key
	if _, err := e.app.Keys.CleanupDeletedKeys(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Keys.Provision(e.ctx, key.Detail.ID); err != nil {
		t.Fatalf("provision key after pool deletion: %v", err)
	}
	e.drain()
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM internal_keys WHERE buyer_key_id = $1 AND status <> 'deleted'", key.Detail.ID), &internalKeys)
	if internalKeys != 0 {
		t.Fatalf("internal keys left for deleted pool = %d, want 0", internalKeys)
	}
	// 新建 Key 不能再选已删除的分组
	buyer.expect(404, "POST", "/api/buyer/keys", map[string]any{"name": "x", "route": map[string]any{"type": "default", "pool": "tm-pool-gone"}})

	// 同名可以重建，且得到新的 sub2api 分组
	var again struct {
		ID             int64  `json:"id"`
		Sub2APIGroupID *int64 `json:"sub2api_group_id"`
	}
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-gone", "platform": "anthropic", "sell_multiplier": 0.8}, &again)
	if again.ID == pool.ID || again.Sub2APIGroupID == nil || *again.Sub2APIGroupID == groupID {
		t.Fatalf("recreated pool should be a new row with a new group: old=%d/%d new=%+v", pool.ID, groupID, again)
	}
}
