package billing

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/yx-token/idlicat/market/internal/buyer"
	"github.com/yx-token/idlicat/market/internal/payment"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/yx-token/idlicat/market/internal/sub2api/ops"
	"github.com/yx-token/idlicat/market/internal/sub2api/opshandlers"
	"github.com/jackc/pgx/v5"
)

// ---- 按充值单退款 ----

// RefundRechargeOrder 从一笔已到账充值单的未退金额中退回指定金额。userID 为发起退款的买家；管理员代为发起时传 0。
// 不暂停 Key：直接扣减 sub2api 余额，扣减成功后记账并原路退回这笔订单；人工充值、超过退款期限或渠道不可用时人工打款。
// 期间余额被用掉导致扣减失败时，退款失败并释放订单的可退金额。
func (f *Funds) RefundRechargeOrder(ctx context.Context, userID, orderID int64, amount float64, note string) (*Refund, error) {
	amt, err := ValidAmount(amount)
	if err != nil {
		return nil, err
	}
	want, err := payment.Fen(amt)
	if err != nil {
		return nil, err
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
	// 锁住订单，防止同一笔订单并发退款超过实付金额
	var buyerID int64
	var channel, status, left string
	var outTradeNo *string
	var recent bool
	err = tx.QueryRow(ctx, `SELECT user_id, channel, status, out_trade_no, (amount - refunded_amount)::text,
			created_at > now() - make_interval(days => $3)
		FROM recharge_orders WHERE id = $1 AND ($2 = 0 OR user_id = $2) FOR UPDATE`, orderID, userID, st.RefundOriginalRouteDays).
		Scan(&buyerID, &channel, &status, &outTradeNo, &left, &recent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ORDER_NOT_FOUND", "recharge order not found")
	}
	if err != nil {
		return nil, err
	}
	if status != "credited" {
		return nil, httpx.Conflict("ORDER_NOT_CREDITED", "only credited recharge orders can be refunded")
	}
	leftFen, err := fenOrZero(left)
	if err != nil {
		return nil, err
	}
	if want > leftFen {
		return nil, httpx.BadRequest("EXCEEDS_ORDER_REFUNDABLE", fmt.Sprintf("超过这笔订单的可退金额 ¥%s", payment.Yuan(leftFen)))
	}

	ident, err := f.Shadow.Get(ctx, buyer.KindBuyer, buyerID)
	if err != nil {
		return nil, err
	}
	if ident == nil || ident.Sub2APIUserID == nil {
		return nil, httpx.BadRequest("NOTHING_TO_REFUND", "no balance to refund")
	}
	bs, err := f.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
	if err != nil {
		return nil, err
	}
	balance := 0.0
	if len(bs) == 1 {
		balance = bs[0].Balance
	}
	avail, err := f.refundableFen(ctx, buyerID, balance)
	if err != nil {
		return nil, err
	}
	if want > avail {
		return nil, httpx.BadRequest("INSUFFICIENT_BALANCE", fmt.Sprintf("余额不足：当前可退 ¥%s（未用完的额度包不计入）", payment.Yuan(avail)))
	}

	var refundID int64
	err = tx.QueryRow(ctx, `INSERT INTO refunds (user_id, kind, recharge_order_id, status, amount, lock_until, note)
		VALUES ($1, 'order', $2, 'subtracting', $3::numeric, now(), NULLIF($4, '')) RETURNING id`, buyerID, orderID, amt, note).Scan(&refundID)
	if err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("REFUND_IN_PROGRESS", "a refund is already in progress")
		}
		return nil, err
	}
	// 人工打款的明细也关联这笔订单，扣减失败时据此释放可退金额
	payCh, payStatus := payment.ChannelManual, "manual"
	var outRefundNo *string
	if st.RefundOriginalRoute && channel != payment.ChannelManual && outTradeNo != nil && recent &&
		f.registry(ctx).Get(payment.ProviderOf(channel)) != nil {
		no := fmt.Sprintf("TMF%dO%d", refundID, orderID)
		payCh, payStatus, outRefundNo = channel, "pending", &no
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refund_payouts (refund_id, recharge_order_id, channel, amount, out_refund_no, status)
		VALUES ($1, $2, $3, $4::numeric, $5, $6)`, refundID, orderID, payCh, amt, outRefundNo, payStatus); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE recharge_orders SET refunded_amount = refunded_amount + $2::numeric WHERE id = $1", orderID, amt); err != nil {
		return nil, err
	}
	opID, err := ops.Enqueue(ctx, tx, ops.NewOp{
		Type:   opshandlers.TypeUserBalanceSubtract,
		Target: fmt.Sprintf("user:%d", *ident.Sub2APIUserID),
		Payload: opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: float64(want) / 100,
			Notes: fmt.Sprintf("tm refund %d order %d", refundID, orderID)},
		IdempotencyKey: fmt.Sprintf("tm-order-refund-%d", refundID),
		Priority:       ops.PriorityUrgent,
		MaxAttempts:    3,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE refunds SET op_id = $2 WHERE id = $1", refundID, opID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.GetRefund(ctx, 0, refundID)
}

// refundableFen 可按充值单退走的余额（分）：sub2api 余额减去未用完的额度包——额度包是账面预付，没有扣 sub2api 余额。
func (f *Funds) refundableFen(ctx context.Context, userID int64, balance float64) (int64, error) {
	var packs float64
	if err := f.DB.QueryRow(ctx, `SELECT COALESCE(sum(remaining), 0)::float8 FROM quota_packs WHERE buyer_id = $1 AND status = 'active'`,
		userID).Scan(&packs); err != nil {
		return 0, err
	}
	// 现金欠款/尚未送达的购包扣款已经减少账面现金，不能趁远端尚未扣到时把它退走。
	// 正常用量入账滞后时账面现金更高，此上界不会扩大可退余额。
	var cash float64
	if err := f.DB.QueryRow(ctx, `SELECT COALESCE(-sum(e.amount),0)::float8
		FROM ledger_accounts a JOIN ledger_entries e ON e.account_id=a.id
		WHERE a.owner_type='buyer' AND a.owner_id=$1 AND a.kind='buyer_cash'`, userID).Scan(&cash); err != nil {
		return 0, err
	}
	// 退到分；加微小量避免浮点误差少退一分
	return max(0, int64(math.Floor(min(balance-packs, cash)*100+1e-6))), nil
}
