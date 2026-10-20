package buyer

import (
	"context"
	"time"

	"github.com/yx-token/idlicat/market/internal/listing"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type Merchant struct {
	SellerID int64     `json:"seller_id"`
	AddedAt  time.Time `json:"added_at"`
}

func (s *KeyService) Merchants(ctx context.Context, userID int64) ([]Merchant, error) {
	rows, err := s.DB.Query(ctx, `SELECT seller_id, added_at FROM buyer_merchants WHERE user_id = $1 ORDER BY added_at DESC, seller_id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Merchant, error) {
		var m Merchant
		err := row.Scan(&m.SellerID, &m.AddedAt)
		return m, err
	})
}

// AddMerchant 不信任前端传入的商家身份，只有公开可用挂单的商家才能被新加入。
func (s *KeyService) AddMerchant(ctx context.Context, userID, sellerID int64) error {
	if sellerID <= 0 {
		return httpx.BadRequest("INVALID_SELLER", "seller_id must be positive")
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO buyer_merchants(user_id, seller_id)
		SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM listings l JOIN upstreams u ON u.id = l.upstream_id
			WHERE l.seller_id = $2 AND `+listing.MarketAvailableSQL+`) ON CONFLICT DO NOTHING`, userID, sellerID)
	if err != nil {
		return err
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM buyer_merchants WHERE user_id = $1 AND seller_id = $2)`, userID, sellerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return httpx.NotFound("MERCHANT_UNAVAILABLE", "merchant has no available market listings")
	}
	// 已有 Key 保留选择与顺序，重新加入后发布最新路由。
	return s.PublishUserKeys(ctx, userID)
}

// RemoveMerchant 重发所有受影响 Key，失败时返回错误；重复删除仍重发以便安全重试。
func (s *KeyService) RemoveMerchant(ctx context.Context, userID, sellerID int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 与 Key 编辑/发布使用相同锁序：先 Key 行，再商家行，避免并发移除与保存死锁。
	if _, err := tx.Exec(ctx, `SELECT id FROM buyer_keys WHERE user_id = $1 AND status <> 'deleted' ORDER BY id FOR UPDATE`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM buyer_merchants WHERE user_id = $1 AND seller_id = $2`, userID, sellerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE buyer_keys SET config_version = config_version + 1, updated_at = now()
		WHERE user_id = $1 AND status <> 'deleted' AND id IN
		(SELECT h.key_id FROM buyer_key_hops h JOIN listings l ON l.id = h.listing_id WHERE l.seller_id = $2)`, userID, sellerID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.PublishUserKeys(ctx, userID)
}

// lockRouteMerchants 在保存路由的事务内重新校验并锁定清单项，防止校验后并发移除。
func lockRouteMerchants(ctx context.Context, tx pgx.Tx, userID int64, hops []resolvedHop) error {
	for _, h := range hops {
		if h.listingID == 0 {
			continue
		}
		var sellerID int64
		err := tx.QueryRow(ctx, `SELECT bm.seller_id FROM buyer_merchants bm JOIN listings l ON l.seller_id = bm.seller_id
			WHERE bm.user_id = $1 AND l.id = $2 FOR KEY SHARE OF bm`, userID, h.listingID).Scan(&sellerID)
		if err == pgx.ErrNoRows {
			return httpx.BadRequest("MERCHANT_NOT_SELECTED", "add this seller to your merchant list before selecting a listing")
		}
		if err != nil {
			return err
		}
	}
	return nil
}
