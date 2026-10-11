package billing

import (
	"context"
	"fmt"
	"strconv"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
)

// RequirePackBalance 购买额度包前校验可调用余额 ≥ 面额。
// 账面预付不扣 sub2api（调用仍从影子余额实时扣）；对账时 active 包 remaining 计入恒等式。
func (f *Funds) RequirePackBalance(ctx context.Context, userID int64, face float64) error {
	if face <= 0 {
		return httpx.BadRequest("INVALID_AMOUNT", "pack face value must be positive")
	}
	ident, err := f.Shadow.Ensure(ctx, buyer.KindBuyer, userID)
	if err != nil {
		return err
	}
	if ident.Sub2APIUserID == nil {
		return httpx.BadRequest("INSUFFICIENT_BALANCE", "请先充值后再购买额度包")
	}
	bs, err := f.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
	if err != nil {
		return err
	}
	bal := 0.0
	if len(bs) == 1 {
		bal = bs[0].Balance
	}
	if bal+1e-9 < face {
		return httpx.BadRequest("INSUFFICIENT_BALANCE", fmt.Sprintf("余额不足：需要 ¥%.2f，当前 ¥%.2f", face, bal))
	}
	return nil
}

// PostPackPurchase 账面预付：买家现金 → 平台现金。不改 sub2api 余额。
func (f *Funds) PostPackPurchase(ctx context.Context, userID, packID int64, face float64) error {
	amt, err := ValidAmount(face)
	if err != nil {
		return err
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := Post(ctx, tx, "pack_purchase", fmt.Sprintf("pack:%d", packID), "quota pack purchase", nil, []Entry{
		{Kind: KindBuyerCash, OwnerID: userID, Amount: amt},
		{Kind: KindPlatformCash, Amount: "-" + amt},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PostPackRefund 退未用量：平台现金 → 买家现金。不改 sub2api（预付从未扣影子余额）。
func (f *Funds) PostPackRefund(ctx context.Context, userID, packID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}
	amt, err := ValidAmount(amount)
	if err != nil {
		return err
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := Post(ctx, tx, "pack_refund", fmt.Sprintf("pack-refund:%d", packID), "quota pack refund", nil, []Entry{
		{Kind: KindBuyerCash, OwnerID: userID, Amount: "-" + amt},
		{Kind: KindPlatformCash, Amount: amt},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// StartPackPayment 为额度包创建在线充值单（金额=面额）。
func (f *Funds) StartPackPayment(ctx context.Context, userID int64, amount float64, channel, ip string) (*buyer.PackPayOrder, error) {
	o, err := f.CreateRecharge(ctx, userID, amount, channel, ip)
	if err != nil {
		return nil, err
	}
	out := &buyer.PackPayOrder{ID: o.ID, Amount: o.Amount, Channel: o.Channel, Status: o.Status}
	if o.Payment != nil {
		out.Payment = &buyer.PackPaymentInfo{
			Channel: o.Payment.Channel, Kind: o.Payment.Kind, URL: o.Payment.URL,
			QRCode: o.Payment.QRCode, Instructions: o.Payment.Instructions,
		}
	}
	return out, nil
}

// PostPackRefundOriginal 原路退回：先把未用量退回账面现金，再扣影子余额并打回购包充值单。
// 若已有进行中的余额退款、或充值单不可退，则保留账面退回（等同 dest=balance）。
func (f *Funds) PostPackRefundOriginal(ctx context.Context, userID, packID int64, amount float64, rechargeID *int64, channel string) error {
	if amount <= 0 {
		return nil
	}
	if err := f.PostPackRefund(ctx, userID, packID, amount); err != nil {
		return err
	}
	if rechargeID == nil || channel == "" || channel == "balance" || channel == payment.ChannelManual {
		return nil
	}
	amt, err := ValidAmount(amount)
	if err != nil {
		return err
	}
	amtF, err := strconv.ParseFloat(amt, 64)
	if err != nil {
		return err
	}
	ident, err := f.Shadow.Get(ctx, buyer.KindBuyer, userID)
	if err != nil {
		return err
	}
	if ident == nil || ident.Sub2APIUserID == nil {
		return nil
	}

	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var refundID int64
	err = tx.QueryRow(ctx, `INSERT INTO refunds (user_id, kind, status, amount, lock_until, note)
		VALUES ($1, 'pack', 'subtracting', $2::numeric, now(), $3) RETURNING id`,
		userID, amt, fmt.Sprintf("pack_original:%d", packID)).Scan(&refundID)
	if err != nil {
		if isUnique(err) {
			return nil
		}
		return err
	}

	tag, err := tx.Exec(ctx, `UPDATE recharge_orders SET refunded_amount = refunded_amount + $2::numeric
		WHERE id = $1 AND user_id = $3 AND status = 'credited' AND channel = $4
			AND amount - refunded_amount + 0.001 >= $2::numeric`, *rechargeID, amt, userID, channel)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO refund_payouts (refund_id, channel, amount, status)
			VALUES ($1, 'manual', $2::numeric, 'manual')`, refundID, amt); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `INSERT INTO refund_payouts (refund_id, recharge_order_id, channel, amount, out_refund_no)
			VALUES ($1, $2, $3, $4::numeric, $5)`,
			refundID, *rechargeID, channel, amt, fmt.Sprintf("TMP%dP%d", refundID, packID)); err != nil {
			return err
		}
	}

	opID, err := ops.Enqueue(ctx, tx, ops.NewOp{
		Type:           opshandlers.TypeUserBalanceSubtract,
		Target:         fmt.Sprintf("user:%d", *ident.Sub2APIUserID),
		Payload:        opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: amtF, Notes: fmt.Sprintf("tm pack refund %d", packID)},
		IdempotencyKey: fmt.Sprintf("tm-pack-refund-%d", packID),
		Priority:       ops.PriorityUrgent,
		MaxAttempts:    3,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refunds SET op_id = $2 WHERE id = $1`, refundID, opID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
