package buyer

import (
	"context"
	"fmt"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/platform/httpx"
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
	return &PurchasePackResult{Pack: p, Key: plain, Note: "store this key now; it will not be shown again"}, nil
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
		if err := s.fulfillOne(ctx, j.id, j.rechargeID, j.listingID, j.buyerID, j.name); err != nil {
			if s.Log != nil {
				s.Log.Error("fulfill pack checkout", "checkout_id", j.id, "err", err)
			}
			_, _ = s.DB.Exec(ctx, `UPDATE pack_checkouts SET status = 'failed', error_reason = $2 WHERE id = $1 AND status = 'pending'`,
				j.id, truncateErr(err, 200))
		}
	}
	return nil
}

func (s *KeyService) fulfillOne(ctx context.Context, checkoutID, rechargeID, listingID, buyerID int64, name string) error {
	l, err := listing.GetListing(ctx, s.DB, listingID)
	if err != nil {
		return err
	}
	if l.FaceValue == nil {
		return httpx.BadRequest("INVALID_PACK", "pack face value is missing")
	}
	face := *l.FaceValue
	if name == "" {
		name = fmt.Sprintf("额度包 #%d", listingID)
	}
	tag, err := s.DB.Exec(ctx, `UPDATE pack_checkouts SET status = 'fulfilled', fulfilled_at = now()
		WHERE id = $1 AND status = 'pending'`, checkoutID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	var channel string
	_ = s.DB.QueryRow(ctx, `SELECT channel FROM recharge_orders WHERE id = $1`, rechargeID).Scan(&channel)
	_, key, err := s.Create(ctx, buyerID, KeyInput{
		Name:   name,
		Route:  RouteInput{Type: "chain", Hops: []HopInput{{ListingID: listingID}}},
		Limits: Limits{QuotaUSD: l.FaceValue},
	})
	if err != nil {
		_, _ = s.DB.Exec(ctx, `UPDATE pack_checkouts SET status = 'pending', fulfilled_at = NULL WHERE id = $1`, checkoutID)
		return err
	}
	rid := rechargeID
	p, err := listing.InsertQuotaPack(ctx, s.DB, listingID, buyerID, face, face, channel, &rid, &key.ID)
	if err != nil {
		_, _ = s.DB.Exec(ctx, `UPDATE pack_checkouts SET status = 'pending', fulfilled_at = NULL WHERE id = $1`, checkoutID)
		return err
	}
	_, _ = s.DB.Exec(ctx, `UPDATE pack_checkouts SET pack_id = $2 WHERE id = $1`, checkoutID, p.ID)
	if s.PackFunds != nil {
		if err := s.PackFunds.PostPackPurchase(ctx, buyerID, p.ID, face); err != nil {
			return err
		}
	}
	return nil
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
