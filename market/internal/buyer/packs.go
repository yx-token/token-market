package buyer

import (
	"context"
	"errors"
	"fmt"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type PurchasePackInput struct {
	ListingID int64  `json:"listing_id"`
	Channel   string `json:"channel"` // balance / alipay_page / alipay_f2f / wechat_native
	Name      string `json:"name"`
}

type PurchasePackResult struct {
	Pack     *listing.QuotaPack `json:"pack,omitempty"`
	Key      string             `json:"key,omitempty"`
	Note     string             `json:"note,omitempty"`
	Recharge *PackPayOrder      `json:"recharge,omitempty"`
}

// PurchasePack 余额即时履约；在线渠道占库存并创建充值单，到账后 FulfillPackCheckouts 签发 Key。
func (s *KeyService) PurchasePack(ctx context.Context, userID int64, in PurchasePackInput, clientIP string) (*PurchasePackResult, error) {
	if in.ListingID <= 0 {
		return nil, httpx.BadRequest("LISTING_REQUIRED", "listing_id is required")
	}
	ch := in.Channel
	if ch == "" {
		ch = "balance"
	}
	l, err := listing.GetListing(ctx, s.DB, in.ListingID)
	if err != nil {
		return nil, err
	}
	if l.Mode != listing.ModePack || l.Status != listing.ListingActive {
		return nil, httpx.BadRequest("NOT_A_PACK", "listing is not an active quota pack")
	}
	if l.FaceValue == nil || *l.FaceValue <= 0 {
		return nil, httpx.BadRequest("INVALID_PACK", "pack face value is missing")
	}
	if listing.PackStockLeft(l) < 1 {
		return nil, httpx.Conflict("PACK_SOLD_OUT", "this quota pack is sold out")
	}
	face := *l.FaceValue
	name := in.Name
	if name == "" {
		name = fmt.Sprintf("额度包 #%d", l.ID)
	}

	if ch == "balance" {
		return s.purchasePackBalance(ctx, userID, l, face, name)
	}
	switch ch {
	case payment.ChannelAlipayPage, payment.ChannelAlipayF2F, payment.ChannelWechatNative:
		return s.purchasePackOnline(ctx, userID, l, face, name, ch, clientIP)
	default:
		return nil, httpx.BadRequest("CHANNEL_UNSUPPORTED", "unsupported payment channel for pack")
	}
}

func (s *KeyService) purchasePackBalance(ctx context.Context, userID int64, l *listing.Listing, face float64, name string) (*PurchasePackResult, error) {
	if s.PackFunds != nil {
		if err := s.PackFunds.RequirePackBalance(ctx, userID, face); err != nil {
			return nil, err
		}
	}
	if err := listing.ReservePack(ctx, s.DB, l.ID); err != nil {
		return nil, err
	}
	plain, key, err := s.Create(ctx, userID, KeyInput{
		Name:   name,
		Route:  RouteInput{Type: "chain", Hops: []HopInput{{ListingID: l.ID}}},
		Limits: Limits{QuotaUSD: l.FaceValue},
	})
	if err != nil {
		_ = listing.ReleasePackStock(ctx, s.DB, l.ID)
		return nil, err
	}
	p, err := listing.InsertQuotaPack(ctx, s.DB, l.ID, userID, face, face, "balance", nil, &key.ID)
	if err != nil {
		_ = listing.ReleasePackStock(ctx, s.DB, l.ID)
		return nil, err
	}
	if s.PackFunds != nil {
		if err := s.PackFunds.PostPackPurchase(ctx, userID, p.ID, face); err != nil {
			_, _ = s.DB.Exec(ctx, `UPDATE quota_packs SET status = 'refunded', remaining = 0, refunded_at = now() WHERE id = $1`, p.ID)
			_ = listing.ReleasePackStock(ctx, s.DB, l.ID)
			if _, e := s.DB.Exec(ctx, `UPDATE buyer_keys SET status = 'disabled', disabled_reason = 'pack_payment_failed', updated_at = now()
				WHERE id = $1`, key.ID); e == nil {
				_ = s.Publish(ctx, key.ID)
			}
			return nil, err
		}
	}
	return &PurchasePackResult{Pack: p, Key: plain}, nil
}

func (s *KeyService) purchasePackOnline(ctx context.Context, userID int64, l *listing.Listing, face float64, name, channel, ip string) (*PurchasePackResult, error) {
	if s.PackFunds == nil {
		return nil, httpx.Unavailable("PAYMENTS_UNAVAILABLE", "payment service is not configured")
	}
	if err := listing.ReservePack(ctx, s.DB, l.ID); err != nil {
		return nil, err
	}
	order, err := s.PackFunds.StartPackPayment(ctx, userID, face, channel, ip)
	if err != nil {
		_ = listing.ReleasePackStock(ctx, s.DB, l.ID)
		return nil, err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO pack_checkouts (recharge_id, listing_id, buyer_id, key_name, status)
		VALUES ($1, $2, $3, $4, 'pending')`, order.ID, l.ID, userID, name)
	if err != nil {
		_ = listing.ReleasePackStock(ctx, s.DB, l.ID)
		_ = s.PackFunds.CancelRecharge(ctx, userID, order.ID)
		return nil, err
	}
	return &PurchasePackResult{
		Recharge: order,
		Note:     "complete payment to receive the pack key; stock is held while the order is pending",
	}, nil
}

func (s *KeyService) ListPacks(ctx context.Context, userID int64) ([]listing.QuotaPack, error) {
	return listing.ListBuyerPacks(ctx, s.DB, userID)
}

// FulfillPackCheckouts 充值到账后履约。
func (s *KeyService) FulfillPackCheckouts(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT c.id, c.recharge_id, c.listing_id, c.buyer_id, COALESCE(c.key_name, '')
		FROM pack_checkouts c
		JOIN recharge_orders r ON r.id = c.recharge_id
		WHERE c.status = 'pending' AND r.status = 'credited'
		ORDER BY c.id LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type job struct {
		id, rechargeID, listingID, buyerID int64
		name                               string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.rechargeID, &j.listingID, &j.buyerID, &j.name); err != nil {
			return err
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, j := range jobs {
		if keyID, err := s.fulfillOne(ctx, j.id, j.rechargeID, j.listingID, j.buyerID, j.name); err != nil {
			if s.Log != nil {
				s.Log.Error("fulfill pack checkout", "checkout_id", j.id, "err", err)
			}
			if cleanupErr := s.failPendingPackCheckout(ctx, j.id, keyID, err); cleanupErr != nil {
				return fmt.Errorf("fail pack checkout %d: %w", j.id, cleanupErr)
			}
		}
	}
	return nil
}

// failPendingPackCheckout 只释放尚未签包的预留库存；到账的钱已在普通余额中，不再入账或退款。
func (s *KeyService) failPendingPackCheckout(ctx context.Context, checkoutID, keyID int64, cause error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var listingID, buyerID int64
	if err := tx.QueryRow(ctx, `SELECT listing_id, buyer_id FROM pack_checkouts WHERE id = $1 FOR UPDATE`, checkoutID).Scan(&listingID, &buyerID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE pack_checkouts c SET status = 'failed', error_reason = $2, fulfilled_at = NULL
		WHERE c.id = $1 AND c.status = 'pending' AND c.pack_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM quota_packs p WHERE p.recharge_id = c.recharge_id)`, checkoutID, truncateErr(cause, 200))
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err := listing.ReleasePackStock(ctx, tx, listingID); err != nil {
			return err
		}
	}
	deletedKey := false
	if keyID != 0 {
		// Create 自行提交 Key；即使另一 worker 已完成订单收尾，仍需删除本次未关联包的 Key。
		// checkout 行锁确保并发签包已结束，既有任务继续清理内部凭证。
		tag, err := tx.Exec(ctx, `UPDATE buyer_keys k SET status = 'deleted', deleted_at = now(), updated_at = now()
			WHERE k.id = $1 AND k.user_id = $2
			AND NOT EXISTS (SELECT 1 FROM quota_packs p WHERE p.buyer_key_id = k.id)`, keyID, buyerID)
		if err != nil {
			return err
		}
		deletedKey = tag.RowsAffected() > 0
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if deletedKey {
		return s.Publish(ctx, keyID)
	}
	return nil
}

// 返回的 keyID 仅供失败收尾清理；已提交的包由 failPendingPackCheckout 的条件更新保护。
func (s *KeyService) fulfillOne(ctx context.Context, checkoutID, rechargeID, listingID, buyerID int64, name string) (keyID int64, err error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 保持 pending 直到包与 checkout 一起提交，避免把认领中的订单提前暴露为已履约。
	var claimedID int64
	err = tx.QueryRow(ctx, `SELECT id FROM pack_checkouts
		WHERE id = $1 AND status = 'pending' AND pack_id IS NULL FOR UPDATE SKIP LOCKED`, checkoutID).Scan(&claimedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	l, err := listing.GetListing(ctx, tx, listingID)
	if err != nil {
		return 0, err
	}
	if l.FaceValue == nil {
		return 0, httpx.BadRequest("INVALID_PACK", "pack face value is missing")
	}
	face := *l.FaceValue
	if name == "" {
		name = fmt.Sprintf("额度包 #%d", listingID)
	}
	var channel string
	if err := tx.QueryRow(ctx, `SELECT channel FROM recharge_orders WHERE id = $1`, rechargeID).Scan(&channel); err != nil {
		return 0, err
	}
	_, key, err := s.Create(ctx, buyerID, KeyInput{
		Name:   name,
		Route:  RouteInput{Type: "chain", Hops: []HopInput{{ListingID: listingID}}},
		Limits: Limits{QuotaUSD: l.FaceValue},
	})
	if err != nil {
		return 0, err
	}
	rid := rechargeID
	p, err := listing.InsertQuotaPack(ctx, tx, listingID, buyerID, face, face, channel, &rid, &key.ID)
	if err != nil {
		return key.ID, err
	}
	if _, err := tx.Exec(ctx, `UPDATE pack_checkouts SET status = 'fulfilled', fulfilled_at = now(), pack_id = $2
		WHERE id = $1 AND status = 'pending' AND pack_id IS NULL`, checkoutID, p.ID); err != nil {
		return key.ID, err
	}
	if err := tx.Commit(ctx); err != nil {
		return key.ID, err
	}
	if s.PackFunds != nil {
		if err := s.PackFunds.PostPackPurchase(ctx, buyerID, p.ID, face); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func (s *KeyService) RefundPack(ctx context.Context, userID, packID int64, dest string) (*listing.QuotaPack, error) {
	if dest == "" {
		dest = "balance"
	}
	if dest != "balance" && dest != "original" {
		return nil, httpx.BadRequest("DEST_UNSUPPORTED", "dest must be balance or original")
	}
	p, err := listing.GetQuotaPack(ctx, s.DB, packID)
	if err != nil {
		return nil, err
	}
	if p.BuyerID != userID {
		return nil, httpx.NotFound("PACK_NOT_FOUND", "quota pack not found")
	}
	refundAmt := listing.RefundableAmount(p)
	if refundAmt <= 0 {
		return nil, httpx.Conflict("NOT_REFUNDABLE", "this pack has no remaining quota to refund")
	}
	rechargeID, payCh := listing.PackPayMeta(ctx, s.DB, packID)
	if dest == "original" {
		if payCh == "" || payCh == "balance" {
			return nil, httpx.BadRequest("NO_ORIGINAL_ROUTE", "this pack was paid with balance; use dest=balance")
		}
		if s.PackFunds == nil {
			return nil, httpx.Unavailable("PAYMENTS_UNAVAILABLE", "payment service is not configured")
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE quota_packs SET status = 'refunded', remaining = 0, refunded_at = now()
		WHERE id = $1 AND status = 'active'`, packID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.Conflict("NOT_REFUNDABLE", "this pack is not active")
	}
	if err := listing.ReleasePackStock(ctx, tx, p.ListingID); err != nil {
		return nil, err
	}
	if p.BuyerKeyID != nil {
		if _, err := tx.Exec(ctx, `UPDATE buyer_keys SET status = 'disabled', disabled_reason = 'pack_refunded', updated_at = now()
			WHERE id = $1 AND user_id = $2 AND status <> 'deleted'`, *p.BuyerKeyID, userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if s.PackFunds != nil {
		if dest == "original" {
			if err := s.PackFunds.PostPackRefundOriginal(ctx, userID, packID, refundAmt, rechargeID, payCh); err != nil {
				if s.Log != nil {
					s.Log.Warn("pack original refund failed, falling back to balance", "pack_id", packID, "err", err)
				}
				if err2 := s.PackFunds.PostPackRefund(ctx, userID, packID, refundAmt); err2 != nil {
					return nil, err2
				}
			}
		} else if err := s.PackFunds.PostPackRefund(ctx, userID, packID, refundAmt); err != nil {
			if s.Log != nil {
				s.Log.Error("pack refund ledger", "pack_id", packID, "err", err)
			}
			return nil, err
		}
	}
	if p.BuyerKeyID != nil {
		_ = s.Publish(ctx, *p.BuyerKeyID)
	}
	return listing.GetQuotaPack(ctx, s.DB, packID)
}

func truncateErr(err error, n int) string {
	s := err.Error()
	if len(s) > n {
		return s[:n]
	}
	return s
}
