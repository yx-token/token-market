package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
)

const KindBuyerStoreCredit = "buyer_store_credit"

// CreditPack 是人民币预付资金与美元面额的购买快照，不是全平台可消费余额。
type CreditPack struct {
	ID                    int64     `json:"id"`
	OfferID               int64     `json:"offer_id"`
	BuyerID               int64     `json:"buyer_id"`
	SellerID              int64     `json:"seller_id"`
	Title                 string    `json:"title"`
	PriceCNY              float64   `json:"price_cny"`
	FaceValueUSD          float64   `json:"face_value_usd"`
	RemainingUSD          float64   `json:"remaining_usd"`
	RemainingBalanceUnits float64   `json:"remaining_balance_units"`
	PrepaidRemainingCNY   float64   `json:"prepaid_remaining_cny"`
	USDToBalanceRate      float64   `json:"usd_to_balance_rate"`
	AllowedPlatforms      []string  `json:"allowed_platforms"`
	AllowedModels         []string  `json:"allowed_models"`
	Status                string    `json:"status"`
	CreatedAt             time.Time `json:"created_at"`
}

const creditPackColumns = `id,offer_id,buyer_id,seller_id,title,price_cny::float8,face_value_usd::float8,
 remaining_usd::float8,remaining_balance_units::float8,prepaid_remaining_cny::float8,
 usd_to_balance_rate::float8,allowed_platforms,allowed_models,status,created_at`

func scanCreditPack(row pgx.Row) (*CreditPack, error) {
	var p CreditPack
	err := row.Scan(&p.ID, &p.OfferID, &p.BuyerID, &p.SellerID, &p.Title, &p.PriceCNY, &p.FaceValueUSD, &p.RemainingUSD,
		&p.RemainingBalanceUnits, &p.PrepaidRemainingCNY, &p.USDToBalanceRate, &p.AllowedPlatforms, &p.AllowedModels, &p.Status, &p.CreatedAt)
	return &p, err
}

func (f *Funds) GetCreditPack(ctx context.Context, userID, packID int64) (*CreditPack, error) {
	p, err := scanCreditPack(f.DB.QueryRow(ctx, "SELECT "+creditPackColumns+" FROM store_credit_packs WHERE id=$1 AND buyer_id=$2", packID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("CREDIT_PACK_NOT_FOUND", "活动包不存在")
	}
	return p, err
}

func (f *Funds) ListCreditPacks(ctx context.Context, userID int64) ([]CreditPack, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+creditPackColumns+" FROM store_credit_packs WHERE buyer_id=$1 ORDER BY id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreditPack{}
	for rows.Next() {
		p, err := scanCreditPack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type PurchaseCreditPackInput struct {
	OfferID        int64  `json:"offer_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (f *Funds) PurchaseCreditPack(ctx context.Context, userID int64, in PurchaseCreditPackInput) (*CreditPack, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.OfferID <= 0 || len(in.IdempotencyKey) < 1 || len(in.IdempotencyKey) > 128 {
		return nil, httpx.BadRequest("INVALID_CREDIT_PURCHASE", "需要 offer_id 与不超过128字节的 idempotency_key")
	}
	// 重试无需依赖远端钱包或仍然在售的商品。
	existing, err := scanCreditPack(f.DB.QueryRow(ctx, "SELECT "+creditPackColumns+" FROM store_credit_packs WHERE buyer_id=$1 AND idempotency_key=$2", userID, in.IdempotencyKey))
	if err == nil {
		if existing.OfferID != in.OfferID {
			return nil, httpx.Conflict("IDEMPOTENCY_CONFLICT", "幂等键已用于其他活动")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	ident, err := f.Shadow.Ensure(ctx, buyer.KindBuyer, userID)
	if err != nil {
		return nil, err
	}
	if ident.Sub2APIUserID == nil {
		return nil, httpx.Conflict("WALLET_NOT_READY", "钱包尚未开通")
	}
	st, err := f.Settings(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 普通影子行串行购买。账面与实时余额同时约束，未送达的扣款不会被重复拿来购包。
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT keys_locked FROM shadow_identities WHERE id=$1 FOR UPDATE", ident.ID).Scan(&locked); err != nil {
		return nil, err
	}
	if locked {
		return nil, httpx.Conflict("REFUND_IN_PROGRESS", "钱包退款处理中")
	}
	existing, err = scanCreditPack(tx.QueryRow(ctx, "SELECT "+creditPackColumns+" FROM store_credit_packs WHERE buyer_id=$1 AND idempotency_key=$2", userID, in.IdempotencyKey))
	if err == nil {
		if existing.OfferID != in.OfferID {
			return nil, httpx.Conflict("IDEMPOTENCY_CONFLICT", "幂等键已用于其他活动")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var price, face, title string
	var sellerID int64
	var platforms, models []string
	err = tx.QueryRow(ctx, `SELECT price_cny::text,face_value_usd::text,title,seller_id,allowed_platforms,allowed_models
 FROM store_credit_offers WHERE id=$1 AND status='active' FOR SHARE`, in.OfferID).Scan(&price, &face, &title, &sellerID, &platforms, &models)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("CREDIT_OFFER_NOT_FOUND", "活动不存在或已停售")
	}
	if err != nil {
		return nil, err
	}
	balances, err := f.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
	if err != nil {
		return nil, err
	}
	realtime := 0.0
	if len(balances) == 1 {
		realtime = balances[0].Balance
	}
	var validConversion bool
	if err := tx.QueryRow(ctx, `SELECT $1::numeric*$2::numeric BETWEEN 0.00000001 AND 1000000000
 AND $1::numeric*$2::numeric=round($1::numeric*$2::numeric,8)`, face, st.CreditUSDToBalanceRate).Scan(&validConversion); err != nil {
		return nil, err
	}
	if !validConversion {
		return nil, httpx.BadRequest("INVALID_CREDIT_CONVERSION", "活动美元额度换算后的平台单位须在0.00000001到1000000000之间且最多8位小数")
	}
	var enough bool
	err = tx.QueryRow(ctx, `SELECT
 COALESCE((SELECT -sum(e.amount) FROM ledger_accounts a JOIN ledger_entries e ON e.account_id=a.id WHERE a.kind='buyer_cash' AND a.owner_type='buyer' AND a.owner_id=$1),0)>=$2::numeric
 AND $3::numeric-COALESCE((SELECT sum(remaining) FROM quota_packs WHERE buyer_id=$1 AND status='active'),0)
 -COALESCE((SELECT sum((payload->>'amount')::numeric) FROM sub2api_ops WHERE target=$4 AND op_type='user.balance_subtract' AND status IN('pending','sending','failed')),0)>=$2::numeric
 AND NOT EXISTS(SELECT 1 FROM refunds WHERE user_id=$1 AND status NOT IN('done','failed'))`, userID, price, realtime, fmt.Sprintf("user:%d", *ident.Sub2APIUserID)).Scan(&enough)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, httpx.BadRequest("INSUFFICIENT_BALANCE", "平台余额不足，请先充值")
	}
	var packID int64
	err = tx.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,remaining_usd,remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,allowed_platforms,allowed_models,idempotency_key)
 VALUES($1,$2,$3,$4,$5::numeric,$6::numeric,$6::numeric,$6::numeric*$7::numeric,$5::numeric,$7,$8,$9,$10) RETURNING id`, in.OfferID, userID, sellerID, title, price, face, st.CreditUSDToBalanceRate, platforms, models, in.IdempotencyKey).Scan(&packID)
	if err != nil {
		return nil, err
	}
	if _, err := Post(ctx, tx, "adjust", fmt.Sprintf("store-credit-purchase:%d", packID), "店铺活动包人民币预付", nil, []Entry{
		{Kind: KindBuyerCash, OwnerID: userID, Amount: price}, {Kind: KindBuyerStoreCredit, OwnerID: userID, Amount: "-" + price},
	}); err != nil {
		return nil, err
	}
	var priceF float64
	if err := tx.QueryRow(ctx, "SELECT $1::numeric::float8", price).Scan(&priceF); err != nil {
		return nil, err
	}
	opID, err := ops.Enqueue(ctx, tx, ops.NewOp{Type: opshandlers.TypeUserBalanceSubtract, Target: fmt.Sprintf("user:%d", *ident.Sub2APIUserID), Payload: opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: priceF, Notes: fmt.Sprintf("tm store credit purchase %d", packID)}, IdempotencyKey: fmt.Sprintf("tm-credit-purchase-%d", packID), Priority: ops.PriorityUrgent})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET purchase_op_id=$2 WHERE id=$1", packID, opID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.GetCreditPack(ctx, userID, packID)
}

// RefundCreditPack 先停止新调用；已在途请求由同包 advisory 锁等待结束。
// worker 追平用量后才退按实付款摊算的余额，赠送美元不能提现。
func (f *Funds) RefundCreditPack(ctx context.Context, userID, packID int64) (*CreditPack, error) {
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('store-credit-pack:' || $1::bigint::text,0))", packID); err != nil {
		return nil, err
	}
	var status string
	err = tx.QueryRow(ctx, "SELECT status FROM store_credit_packs WHERE id=$1 AND buyer_id=$2 FOR UPDATE", packID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("CREDIT_PACK_NOT_FOUND", "活动包不存在")
	}
	if err != nil {
		return nil, err
	}
	switch status {
	case "refunding", "refunded":
	case "active":
		if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET status='refunding',refund_requested_at=now() WHERE id=$1", packID); err != nil {
			return nil, err
		}
	default:
		return nil, httpx.Conflict("CREDIT_PACK_NOT_REFUNDABLE", "活动包尚未开通或额度已用尽")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.GetCreditPack(ctx, userID, packID)
}

// AdvanceCreditPacks 等活动钱包消费与账本完全一致后推进退款。远程失败保留冻结，不提前返现。
func (f *Funds) AdvanceCreditPacks(ctx context.Context) error {
	if err := f.retryCreditRemainders(ctx); err != nil {
		return err
	}
	rows, err := f.DB.Query(ctx, "SELECT id FROM store_credit_packs WHERE status='refunding' ORDER BY id")
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := f.advanceCreditRefund(ctx, id); err != nil {
			return fmt.Errorf("credit refund %d: %w", id, err)
		}
	}
	return nil
}

func (f *Funds) advanceCreditRefund(ctx context.Context, packID int64) error {
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var buyerID int64
	var status, remaining, prepaid string
	var requested *time.Time
	var refundOp, creditOp *int64
	err = tx.QueryRow(ctx, `SELECT buyer_id,status,remaining_balance_units::text,prepaid_remaining_cny::text,
 refund_requested_at,refund_op_id,refund_credit_op_id FROM store_credit_packs WHERE id=$1 FOR UPDATE`, packID).
		Scan(&buyerID, &status, &remaining, &prepaid, &requested, &refundOp, &creditOp)
	if err != nil {
		return err
	}
	if status != "refunding" {
		return nil
	}
	if requested == nil || f.now().Before(requested.Add(ingestSafetyLag+time.Minute)) {
		return nil
	}
	if creditOp != nil {
		st, err := ops.Status(ctx, tx, *creditOp)
		if err != nil {
			return err
		}
		if st != ops.StatusDone {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET status='refunded',refunded_at=now() WHERE id=$1", packID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var packSubID, buyerSubID int64
	if err := tx.QueryRow(ctx, "SELECT sub2api_user_id FROM shadow_identities WHERE kind='credit_pack' AND owner_id=$1", packID).Scan(&packSubID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT sub2api_user_id FROM shadow_identities WHERE kind='buyer' AND owner_id=$1", buyerID).Scan(&buyerSubID); err != nil {
		return err
	}
	if refundOp == nil {
		// sub2api users.balance 是 numeric(20,8)，usage 与本地余量是10位。
		// 仅允许已入账消费、已完成充值/补回各自能解释的舍入误差，
		// 不使用与使用量无关的宽松容差，也不放行未入账/出站中的资金差额。
		balances, err := f.Replica.UserBalances(ctx, []int64{packSubID})
		if err != nil {
			return err
		}
		if len(balances) != 1 {
			return nil
		}
		var settled bool
		err = tx.QueryRow(ctx, `SELECT $1::numeric>=0 AND abs($1::numeric-$2::numeric)<=0.000000001
 +COALESCE((SELECT sum(abs(round(actual_cost,8)-actual_cost)) FROM usage_records
 WHERE credit_pack_id=$3 AND ledger_txn_id IS NOT NULL),0)
 +COALESCE((SELECT sum(abs(round((payload->>'amount')::numeric,8)-(payload->>'amount')::numeric))
 FROM sub2api_ops WHERE target=$4 AND status='done' AND op_type='user.balance_add'),0)
 +COALESCE((SELECT abs((o.payload->>'amount')::numeric-p.face_value_usd*p.usd_to_balance_rate)
 FROM store_credit_packs p JOIN sub2api_ops o ON o.id=p.funding_op_id WHERE p.id=$3 AND o.status='done'),0)
 +COALESCE((SELECT sum(abs((o.payload->>'amount')::numeric-a.cash_cny))
 FROM store_credit_allocations a JOIN sub2api_ops o ON o.idempotency_key='tm-credit-restore-'||a.usage_id::text
 WHERE a.pack_id=$3 AND o.status='done'),0)
 AND NOT EXISTS(SELECT 1 FROM usage_records WHERE credit_pack_id=$3 AND ledger_txn_id IS NULL)
 AND NOT EXISTS(SELECT 1 FROM sub2api_ops WHERE target=$4 AND status IN('pending','sending','failed','dead'))`, balances[0].Balance, remaining, packID, fmt.Sprintf("user:%d", packSubID)).Scan(&settled)
		if err != nil {
			return err
		}
		if !settled {
			return nil
		}
		// 撤销实际8位远端余额；发送10位账面余量会因极小尾差触发上游余额不足。
		left := balances[0].Balance
		if left > 0 {
			id, err := ops.Enqueue(ctx, tx, ops.NewOp{Type: opshandlers.TypeUserBalanceSubtract, Target: fmt.Sprintf("user:%d", packSubID), Payload: opshandlers.BalancePayload{UserID: packSubID, Amount: left, Notes: fmt.Sprintf("tm store credit refund %d", packID)}, IdempotencyKey: fmt.Sprintf("tm-credit-refund-debit-%d", packID), Priority: ops.PriorityUrgent})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET refund_op_id=$2 WHERE id=$1", packID, id); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
	} else {
		st, err := ops.Status(ctx, tx, *refundOp)
		if err != nil {
			return err
		}
		if st != ops.StatusDone {
			return nil
		}
	}
	var refundAmount float64
	if err := tx.QueryRow(ctx, "SELECT $1::numeric::float8", prepaid).Scan(&refundAmount); err != nil {
		return err
	}
	if refundAmount > 0 {
		if _, err := Post(ctx, tx, "adjust", fmt.Sprintf("store-credit-refund:%d", packID), "店铺活动包未消费人民币预付退回", nil, []Entry{
			{Kind: KindBuyerCash, OwnerID: buyerID, Amount: "-" + prepaid}, {Kind: KindBuyerStoreCredit, OwnerID: buyerID, Amount: prepaid},
		}); err != nil {
			return err
		}
		id, err := ops.Enqueue(ctx, tx, ops.NewOp{Type: opshandlers.TypeUserBalanceAdd, Target: fmt.Sprintf("user:%d", buyerSubID), Payload: opshandlers.BalancePayload{UserID: buyerSubID, Amount: refundAmount, Notes: fmt.Sprintf("tm store credit refund %d", packID)}, IdempotencyKey: fmt.Sprintf("tm-credit-refund-credit-%d", packID), Priority: ops.PriorityUrgent})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET refund_credit_op_id=$2,remaining_usd=0,remaining_balance_units=0,prepaid_remaining_cny=0 WHERE id=$1", packID, id); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, "UPDATE store_credit_packs SET status='refunded',refunded_at=now(),remaining_usd=0,remaining_balance_units=0,prepaid_remaining_cny=0 WHERE id=$1", packID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
