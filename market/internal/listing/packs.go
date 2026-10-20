package listing

import (
	"context"
	"errors"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

const (
	MinPackFace  = 10.0
	MinPackStock = 1
)

type QuotaPack struct {
	ID         int64   `json:"id"`
	ListingID  int64   `json:"listing_id"`
	BuyerID    int64   `json:"buyer_id"`
	BuyerKeyID *int64  `json:"buyer_key_id,omitempty"`
	FaceValue  float64 `json:"face_value"`
	Remaining  float64 `json:"remaining"`
	Status     string  `json:"status"`
	PaidAmount float64 `json:"paid_amount"`
	PayChannel *string `json:"pay_channel,omitempty"`
	RechargeID *int64  `json:"recharge_id,omitempty"`
}

func PackStockLeft(l *Listing) int {
	if l.Stock == nil {
		return 0
	}
	left := *l.Stock - l.StockSold
	if left < 0 {
		return 0
	}
	return left
}

// ReservePack 占用一份额度包库存。无货返回 PACK_SOLD_OUT。
func ReservePack(ctx context.Context, db DB, listingID int64) error {
	tag, err := db.Exec(ctx, `UPDATE listings SET stock_sold = stock_sold + 1, updated_at = now()
		WHERE id = $1 AND mode = 'pack' AND status = 'active' AND stock IS NOT NULL AND stock_sold < stock`, listingID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("PACK_SOLD_OUT", "this quota pack is sold out")
	}
	return nil
}

func ReleasePackStock(ctx context.Context, db DB, listingID int64) error {
	_, err := db.Exec(ctx, `UPDATE listings SET stock_sold = GREATEST(stock_sold - 1, 0), updated_at = now()
		WHERE id = $1 AND mode = 'pack'`, listingID)
	return err
}

func InsertQuotaPack(ctx context.Context, db DB, listingID, buyerID int64, face, paid float64, channel string, rechargeID, keyID *int64) (*QuotaPack, error) {
	var p QuotaPack
	err := db.QueryRow(ctx, `INSERT INTO quota_packs (listing_id, buyer_id, buyer_key_id, face_value, remaining, status, paid_amount, pay_channel, recharge_id)
		VALUES ($1, $2, $3, $4, $4, 'active', $5, NULLIF($6, ''), $7)
		RETURNING id, listing_id, buyer_id, buyer_key_id, face_value::float8, remaining::float8, status, paid_amount::float8, pay_channel`,
		listingID, buyerID, keyID, face, paid, channel, rechargeID).
		Scan(&p.ID, &p.ListingID, &p.BuyerID, &p.BuyerKeyID, &p.FaceValue, &p.Remaining, &p.Status, &p.PaidAmount, &p.PayChannel)
	return &p, err
}

func GetQuotaPack(ctx context.Context, db DB, id int64) (*QuotaPack, error) {
	var p QuotaPack
	err := db.QueryRow(ctx, `SELECT id, listing_id, buyer_id, buyer_key_id, face_value::float8, remaining::float8, status, paid_amount::float8, pay_channel, recharge_id
		FROM quota_packs WHERE id = $1`, id).
		Scan(&p.ID, &p.ListingID, &p.BuyerID, &p.BuyerKeyID, &p.FaceValue, &p.Remaining, &p.Status, &p.PaidAmount, &p.PayChannel, &p.RechargeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PACK_NOT_FOUND", "quota pack not found")
	}
	return &p, err
}

func ListBuyerPacks(ctx context.Context, db DB, buyerID int64) ([]QuotaPack, error) {
	rows, err := db.Query(ctx, `SELECT id, listing_id, buyer_id, buyer_key_id, face_value::float8, remaining::float8, status, paid_amount::float8, pay_channel, recharge_id
		FROM quota_packs WHERE buyer_id = $1 ORDER BY id DESC`, buyerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuotaPack, error) {
		var p QuotaPack
		err := row.Scan(&p.ID, &p.ListingID, &p.BuyerID, &p.BuyerKeyID, &p.FaceValue, &p.Remaining, &p.Status, &p.PaidAmount, &p.PayChannel, &p.RechargeID)
		return p, err
	})
}

// PackPayMeta 返回额度包关联的充值单与支付渠道，供原路退款使用。
func PackPayMeta(ctx context.Context, db DB, packID int64) (*int64, string) {
	var rid *int64
	var ch *string
	_ = db.QueryRow(ctx, `SELECT recharge_id, pay_channel FROM quota_packs WHERE id = $1`, packID).Scan(&rid, &ch)
	if ch == nil {
		return rid, ""
	}
	return rid, *ch
}

func RefundableAmount(p *QuotaPack) float64 {
	if p.Status != "active" {
		return 0
	}
	if p.Remaining < 0 {
		return 0
	}
	return p.Remaining
}

func validatePackInput(in CreateListingInput) error {
	if in.Multiplier == nil {
		return httpx.BadRequest("MULTIPLIER_REQUIRED", "pack listing needs a billing multiplier")
	}
	if in.FaceValue == nil || *in.FaceValue < MinPackFace {
		return httpx.BadRequest("FACE_VALUE_REQUIRED", "pack face value must be at least 10")
	}
	if in.Stock == nil || *in.Stock < MinPackStock {
		return httpx.BadRequest("STOCK_REQUIRED", "pack stock must be at least 1")
	}
	if in.JoinDefaultPool != nil && *in.JoinDefaultPool {
		return httpx.BadRequest("PACK_NO_POOL", "quota packs cannot join the default pool")
	}
	return nil
}
