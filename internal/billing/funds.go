package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RiskChecker 由风控模块实现：提现是否需要人工审核。
type RiskChecker interface {
	WithdrawalNeedsReview(ctx context.Context, sellerID int64) (bool, string, error)
}

type KYCChecker interface {
	KYCVerified(ctx context.Context, userID int64) (bool, error)
}

type Funds struct {
	DB       *pgxpool.Pool
	Replica  *replica.Reader
	Shadow   *buyer.ShadowService
	Keys     *buyer.KeyService
	Keyring  *crypto.Keyring
	KYC      KYCChecker
	Risk     RiskChecker
	Settings func(ctx context.Context) (settings.Settings, error)
	// Payments 返回当前生效的支付渠道（站内"支付配置"，按配置动态构建）
	Payments PaymentRegistry
	Log      *slog.Logger
	Now      func() time.Time
}

type PaymentRegistry interface {
	Registry(ctx context.Context) (*payment.Registry, error)
}

// registry 返回当前的支付渠道；读取配置失败时记录日志并按没有在线渠道处理。
func (f *Funds) registry(ctx context.Context) *payment.Registry {
	if f.Payments == nil {
		return nil
	}
	reg, err := f.Payments.Registry(ctx)
	if err != nil {
		f.Log.Error("load payment merchants", "err", err)
		return nil
	}
	return reg
}

func (f *Funds) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// ---- 余额 ----

type Wallet struct {
	Balance      *float64        `json:"balance"`              // sub2api 实时余额，影子用户未开通时为空
	Refundable   *float64        `json:"refundable,omitempty"` // 可按充值单退款的余额：实时余额减去未用完的额度包
	LedgerCash   float64         `json:"ledger_cash"`          // 账面余额（用量入账有几分钟延迟）
	Recharges    []RechargeOrder `json:"recent_recharges"`
	ActiveRefund *Refund         `json:"active_refund,omitempty"`
}

func (f *Funds) Wallet(ctx context.Context, userID int64) (*Wallet, error) {
	w := &Wallet{}
	ident, err := f.Shadow.Get(ctx, buyer.KindBuyer, userID)
	if err != nil {
		return nil, err
	}
	if ident != nil && ident.Sub2APIUserID != nil {
		bs, err := f.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
		if err != nil {
			return nil, err
		}
		if len(bs) == 1 {
			w.Balance = &bs[0].Balance
			fen, err := f.refundableFen(ctx, userID, bs[0].Balance)
			if err != nil {
				return nil, err
			}
			v := float64(fen) / 100
			w.Refundable = &v
		}
	}
	bal, err := Balances(ctx, f.DB, "buyer", userID)
	if err != nil {
		return nil, err
	}
	w.LedgerCash = bal[KindBuyerCash]
	if w.Recharges, err = f.ListRecharges(ctx, userID, "", 10, 0); err != nil {
		return nil, err
	}
	if w.ActiveRefund, err = f.activeRefund(ctx, userID); err != nil {
		return nil, err
	}
	return w, nil
}

// ---- 退款 ----

// 退款类型：退全部余额（暂停 Key）、按充值单退指定金额、额度包原路退回。后两种不暂停 Key，预先生成打款明细。
const (
	RefundBalance = "balance"
	RefundOrder   = "order"
	RefundPack    = "pack"
)

type Refund struct {
	ID              int64          `json:"id"`
	UserID          int64          `json:"user_id"`
	Kind            string         `json:"kind"`
	RechargeOrderID *int64         `json:"recharge_order_id,omitempty"`
	Status          string         `json:"status"`
	Amount          *float64       `json:"amount,omitempty"`
	PayoutRef       *string        `json:"payout_ref,omitempty"`
	Note            *string        `json:"note,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	Payouts         []RefundPayout `json:"payouts"`
}

const refundColumns = `id, user_id, kind, recharge_order_id, status, amount::float8, payout_ref, note, created_at, finished_at`

func scanRefund(row pgx.Row) (*Refund, error) {
	var r Refund
	if err := row.Scan(&r.ID, &r.UserID, &r.Kind, &r.RechargeOrderID, &r.Status, &r.Amount, &r.PayoutRef, &r.Note, &r.CreatedAt,
		&r.FinishedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func (f *Funds) activeRefund(ctx context.Context, userID int64) (*Refund, error) {
	r, err := scanRefund(f.DB.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE user_id = $1 AND status NOT IN ('done', 'failed')", userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Payouts, err = f.refundPayouts(ctx, r.ID); err != nil {
		return nil, err
	}
	return r, nil
}

// RequestRefund 申请退还全部剩余余额：先暂停买家全部 Key，等进行中的请求结束后再扣减 sub2api 余额。
func (f *Funds) RequestRefund(ctx context.Context, userID int64, note string) (*Refund, error) {
	st, err := f.Settings(ctx)
	if err != nil {
		return nil, err
	}
	ident, err := f.Shadow.Get(ctx, buyer.KindBuyer, userID)
	if err != nil {
		return nil, err
	}
	if ident == nil || ident.Sub2APIUserID == nil {
		return nil, httpx.BadRequest("NOTHING_TO_REFUND", "no balance to refund")
	}
	r, err := scanRefund(f.DB.QueryRow(ctx, `INSERT INTO refunds (user_id, lock_until, note) VALUES ($1, $2, NULLIF($3, '')) RETURNING `+refundColumns,
		userID, f.now().Add(time.Duration(st.RefundLockSeconds)*time.Second), note))
	if err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("REFUND_IN_PROGRESS", "a refund is already in progress")
		}
		return nil, err
	}
	if err := f.Keys.SetBuyerLocked(ctx, userID, true); err != nil {
		return nil, err
	}
	return r, nil
}

// AdvanceRefunds 推进退款状态机，由 worker 周期调用。
func (f *Funds) AdvanceRefunds(ctx context.Context) error {
	rows, err := f.DB.Query(ctx, "SELECT "+refundColumns+" FROM refunds WHERE status IN ('locking', 'subtracting') ORDER BY id")
	if err != nil {
		return err
	}
	var items []Refund
	for rows.Next() {
		r, err := scanRefund(rows)
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, *r)
	}
	rows.Close()
	for _, r := range items {
		if err := f.advanceRefund(ctx, r); err != nil {
			f.Log.Error("advance refund", "refund_id", r.ID, "err", err)
		}
	}
	return nil
}

func (f *Funds) advanceRefund(ctx context.Context, r Refund) error {
	ident, err := f.Shadow.Get(ctx, buyer.KindBuyer, r.UserID)
	if err != nil || ident == nil || ident.Sub2APIUserID == nil {
		return fmt.Errorf("shadow identity missing: %v", err)
	}
	switch r.Status {
	case "locking":
		var lockUntil time.Time
		if err := f.DB.QueryRow(ctx, "SELECT lock_until FROM refunds WHERE id = $1", r.ID).Scan(&lockUntil); err != nil {
			return err
		}
		if f.now().Before(lockUntil) {
			return nil
		}
		bs, err := f.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
		if err != nil {
			return err
		}
		balance := 0.0
		if len(bs) == 1 {
			// 退到分；加微小量避免 9.16×100 = 915.999… 之类的浮点误差少退一分
			balance = math.Floor(bs[0].Balance*100+1e-6) / 100
		}
		if balance <= 0 {
			if _, err := f.DB.Exec(ctx, "UPDATE refunds SET status = 'done', amount = 0, finished_at = now() WHERE id = $1", r.ID); err != nil {
				return err
			}
			return f.Keys.SetBuyerLocked(ctx, r.UserID, false)
		}
		attempt := time.Now().UnixNano()
		opID, err := ops.Enqueue(ctx, f.DB, ops.NewOp{
			Type:           opshandlers.TypeUserBalanceSubtract,
			Target:         fmt.Sprintf("user:%d", *ident.Sub2APIUserID),
			Payload:        opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: balance, Notes: fmt.Sprintf("tm refund %d", r.ID)},
			IdempotencyKey: fmt.Sprintf("tm-refund-%d-%d", r.ID, attempt),
			Priority:       ops.PriorityUrgent,
			MaxAttempts:    3,
		})
		if err != nil {
			return err
		}
		_, err = f.DB.Exec(ctx, "UPDATE refunds SET status = 'subtracting', amount = $2, op_id = $3 WHERE id = $1",
			r.ID, strconv.FormatFloat(balance, 'f', 2, 64), opID)
		return err
	case "subtracting":
		var opID int64
		var amount string
		if err := f.DB.QueryRow(ctx, "SELECT op_id, amount::text FROM refunds WHERE id = $1", r.ID).Scan(&opID, &amount); err != nil {
			return err
		}
		status, err := ops.Status(ctx, f.DB, opID)
		if err != nil {
			return err
		}
		switch status {
		case ops.StatusDone:
			tx, err := f.DB.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := Post(ctx, tx, "refund", fmt.Sprintf("refund:%d", r.ID), "", nil, []Entry{
				{Kind: KindBuyerCash, OwnerID: r.UserID, Amount: amount},
				{Kind: KindPlatformCash, Amount: "-" + amount},
			}); err != nil && !errors.Is(err, ErrDuplicateTxn) {
				return err
			}
			// 按单退款、额度包原路退款已预写打款明细；退全部余额时再按充值单拆分
			if r.Kind == RefundBalance {
				if err := f.planPayouts(ctx, tx, r.ID, r.UserID, amount); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, "UPDATE refunds SET status = 'paying' WHERE id = $1", r.ID); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			// 只有退全部余额锁了 Key，扣完影子余额后解锁
			if r.Kind != RefundBalance {
				return nil
			}
			return f.Keys.SetBuyerLocked(ctx, r.UserID, false)
		case ops.StatusDead:
			if r.Kind != RefundBalance {
				// 按单退款：期间余额被用掉；额度包原路：账面已退回余额。都直接结束，打款明细作废
				return f.abortRefund(ctx, r.ID)
			}
			// 期间余额发生变化导致扣减失败：重新读取余额再试
			_, err := f.DB.Exec(ctx, "UPDATE refunds SET status = 'locking', lock_until = now(), op_id = NULL, amount = NULL WHERE id = $1", r.ID)
			return err
		}
	}
	return nil
}

// MarkRefundPaid 管理员确认需要人工打款的部分已经打款；原路退回的部分由 worker 自动完成。
func (f *Funds) MarkRefundPaid(ctx context.Context, refundID int64, payoutRef string) (*Refund, error) {
	if payoutRef == "" {
		return nil, httpx.BadRequest("PAYOUT_REF_REQUIRED", "payout reference is required")
	}
	var status string
	err := f.DB.QueryRow(ctx, "SELECT status FROM refunds WHERE id = $1", refundID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("REFUND_NOT_FOUND", "refund not found")
	}
	if err != nil {
		return nil, err
	}
	if status != "paying" {
		return nil, httpx.Conflict("REFUND_NOT_PAYING", "refund is not waiting for payout")
	}
	var payouts, manual int
	if err := f.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status = 'manual') FROM refund_payouts WHERE refund_id = $1`,
		refundID).Scan(&payouts, &manual); err != nil {
		return nil, err
	}
	switch {
	case payouts == 0:
		// 升级前进入打款阶段的退款没有打款明细，整笔人工打款
		if _, err := f.DB.Exec(ctx, "UPDATE refunds SET status = 'done', payout_ref = $2, finished_at = now() WHERE id = $1 AND status = 'paying'",
			refundID, payoutRef); err != nil {
			return nil, err
		}
	case manual == 0:
		return nil, httpx.Conflict("NO_MANUAL_PAYOUT", "this refund has no part waiting for manual payout")
	default:
		if _, err := f.DB.Exec(ctx, `UPDATE refund_payouts SET status = 'success', provider_ref = $2, finished_at = now()
			WHERE refund_id = $1 AND status = 'manual'`, refundID, payoutRef); err != nil {
			return nil, err
		}
		if err := f.finishRefunds(ctx); err != nil {
			return nil, err
		}
	}
	return f.GetRefund(ctx, 0, refundID)
}

func (f *Funds) GetRefund(ctx context.Context, userID, refundID int64) (*Refund, error) {
	r, err := scanRefund(f.DB.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id = $1 AND ($2 = 0 OR user_id = $2)", refundID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("REFUND_NOT_FOUND", "refund not found")
	}
	if err != nil {
		return nil, err
	}
	if r.Payouts, err = f.refundPayouts(ctx, r.ID); err != nil {
		return nil, err
	}
	return r, nil
}

func (f *Funds) ListRefunds(ctx context.Context, userID int64, status string, limit, offset int) ([]Refund, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+refundColumns+` FROM refunds WHERE ($1 = 0 OR user_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY id DESC LIMIT $3 OFFSET $4`, userID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Refund
	for rows.Next() {
		r, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if out[i].Payouts, err = f.refundPayouts(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- 提现 ----

type Withdrawal struct {
	ID           int64      `json:"id"`
	SellerID     int64      `json:"seller_id"`
	Amount       float64    `json:"amount"`
	Channel      string     `json:"channel"`
	AccountInfo  string     `json:"account_info,omitempty"`
	Status       string     `json:"status"`
	ReviewReason *string    `json:"review_reason,omitempty"`
	PayoutRef    *string    `json:"payout_ref,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	PaidAt       *time.Time `json:"paid_at,omitempty"`
}

const withdrawalColumns = `id, seller_id, amount::float8, channel, account_info_enc, status, review_reason, payout_ref, created_at, reviewed_at, paid_at`

func (f *Funds) scanWithdrawal(row pgx.Row, reveal bool) (*Withdrawal, error) {
	var w Withdrawal
	var enc []byte
	if err := row.Scan(&w.ID, &w.SellerID, &w.Amount, &w.Channel, &enc, &w.Status, &w.ReviewReason, &w.PayoutRef, &w.CreatedAt, &w.ReviewedAt, &w.PaidAt); err != nil {
		return nil, err
	}
	if reveal {
		info, err := f.Keyring.OpenString(enc, fmt.Sprintf("withdrawal:%d", w.SellerID))
		if err != nil {
			return nil, err
		}
		w.AccountInfo = info
	}
	return &w, nil
}

// RequestWithdrawal 卖家申请提现：需实名、不低于最低金额、不超过可提现余额。1:1 换算为人民币。
func (f *Funds) RequestWithdrawal(ctx context.Context, sellerID int64, amount float64, channel, accountInfo string) (*Withdrawal, error) {
	st, err := f.Settings(ctx)
	if err != nil {
		return nil, err
	}
	amt, err := ValidAmount(amount)
	if err != nil {
		return nil, err
	}
	if amount < st.WithdrawMin {
		return nil, httpx.BadRequest("BELOW_MINIMUM", fmt.Sprintf("minimum withdrawal is %.2f", st.WithdrawMin))
	}
	if channel == "" || accountInfo == "" {
		return nil, httpx.BadRequest("INVALID_PAYOUT_ACCOUNT", "payout channel and account are required")
	}
	if ok, err := f.KYC.KYCVerified(ctx, sellerID); err != nil {
		return nil, err
	} else if !ok {
		return nil, httpx.Forbidden("KYC_REQUIRED", "real-name verification is required before withdrawal")
	}
	enc, err := f.Keyring.SealString(accountInfo, fmt.Sprintf("withdrawal:%d", sellerID))
	if err != nil {
		return nil, err
	}

	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 锁住卖家的可提现账户，防止并发提现超额
	acc, err := AccountID(ctx, tx, KindSellerAvailable, sellerID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM ledger_accounts WHERE id = $1 FOR UPDATE", acc); err != nil {
		return nil, err
	}
	available, err := balanceExact(ctx, tx, KindSellerAvailable, sellerID)
	if err != nil {
		return nil, err
	}
	var enough bool
	if err := tx.QueryRow(ctx, "SELECT $1::numeric >= $2::numeric", available, amt).Scan(&enough); err != nil {
		return nil, err
	}
	if !enough {
		return nil, httpx.BadRequest("INSUFFICIENT_AVAILABLE", "amount exceeds withdrawable balance")
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO withdrawals (seller_id, amount, channel, account_info_enc) VALUES ($1, $2::numeric, $3, $4) RETURNING id`,
		sellerID, amt, channel, enc).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := Post(ctx, tx, "withdraw_request", fmt.Sprintf("withdraw:%d", id), "", nil, []Entry{
		{Kind: KindSellerAvailable, OwnerID: sellerID, Amount: amt},
		{Kind: KindSellerWithdrawing, OwnerID: sellerID, Amount: "-" + amt},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// 自动审核：金额不超过阈值且没有未处理的风险事件
	needsReview, reason, err := f.Risk.WithdrawalNeedsReview(ctx, sellerID)
	if err != nil {
		return nil, err
	}
	if amount <= st.WithdrawAutoApproveMax && !needsReview {
		if _, err := f.DB.Exec(ctx, `UPDATE withdrawals SET status = 'approved', review_reason = 'auto approved', reviewed_at = now() WHERE id = $1`, id); err != nil {
			return nil, err
		}
	} else if reason != "" {
		if _, err := f.DB.Exec(ctx, `UPDATE withdrawals SET review_reason = $2 WHERE id = $1`, id, reason); err != nil {
			return nil, err
		}
	}
	return f.GetWithdrawal(ctx, sellerID, id, false)
}

func (f *Funds) GetWithdrawal(ctx context.Context, sellerID, id int64, reveal bool) (*Withdrawal, error) {
	w, err := f.scanWithdrawal(f.DB.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM withdrawals WHERE id = $1 AND ($2 = 0 OR seller_id = $2)", id, sellerID), reveal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("WITHDRAWAL_NOT_FOUND", "withdrawal not found")
	}
	return w, err
}

func (f *Funds) ListWithdrawals(ctx context.Context, sellerID int64, status string, reveal bool, limit, offset int) ([]Withdrawal, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+withdrawalColumns+` FROM withdrawals WHERE ($1 = 0 OR seller_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY id DESC LIMIT $3 OFFSET $4`, sellerID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Withdrawal
	for rows.Next() {
		w, err := f.scanWithdrawal(rows, reveal)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// ReviewWithdrawal 管理员审核：通过或驳回（驳回时金额退回可提现）。
func (f *Funds) ReviewWithdrawal(ctx context.Context, adminID, id int64, approve bool, reason string) (*Withdrawal, error) {
	w, err := f.GetWithdrawal(ctx, 0, id, false)
	if err != nil {
		return nil, err
	}
	if w.Status != "pending_review" && (w.Status != "approved" || approve) {
		return nil, httpx.Conflict("NOT_REVIEWABLE", "withdrawal is not pending review")
	}
	if approve {
		_, err = f.DB.Exec(ctx, `UPDATE withdrawals SET status = 'approved', review_reason = NULLIF($2, ''), reviewed_by = $3, reviewed_at = now() WHERE id = $1`,
			id, reason, adminID)
		if err != nil {
			return nil, err
		}
		return f.GetWithdrawal(ctx, 0, id, false)
	}
	return f.revertWithdrawal(ctx, w, "rejected", reason, &adminID)
}

func (f *Funds) revertWithdrawal(ctx context.Context, w *Withdrawal, status, reason string, by *int64) (*Withdrawal, error) {
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var amount string
	if err := tx.QueryRow(ctx, "SELECT amount::text FROM withdrawals WHERE id = $1", w.ID).Scan(&amount); err != nil {
		return nil, err
	}
	if _, err := Post(ctx, tx, "withdraw_reverted", fmt.Sprintf("withdraw:%d", w.ID), reason, by, []Entry{
		{Kind: KindSellerWithdrawing, OwnerID: w.SellerID, Amount: amount},
		{Kind: KindSellerAvailable, OwnerID: w.SellerID, Amount: "-" + amount},
	}); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE withdrawals SET status = $2, review_reason = NULLIF($3, ''), reviewed_by = $4, reviewed_at = now() WHERE id = $1`,
		w.ID, status, reason, by); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.GetWithdrawal(ctx, 0, w.ID, false)
}

// MarkWithdrawalPaid 管理员确认已打款（第一期为人工代付）；failed 为 true 时表示打款失败，金额退回可提现。
func (f *Funds) MarkWithdrawalPaid(ctx context.Context, adminID, id int64, payoutRef string, failed bool, reason string) (*Withdrawal, error) {
	w, err := f.GetWithdrawal(ctx, 0, id, false)
	if err != nil {
		return nil, err
	}
	if w.Status != "approved" {
		return nil, httpx.Conflict("NOT_APPROVED", "withdrawal is not approved")
	}
	if failed {
		return f.revertWithdrawal(ctx, w, "failed", reason, &adminID)
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var amount string
	if err := tx.QueryRow(ctx, "SELECT amount::text FROM withdrawals WHERE id = $1", id).Scan(&amount); err != nil {
		return nil, err
	}
	if _, err := Post(ctx, tx, "withdraw_paid", fmt.Sprintf("withdraw:%d", id), payoutRef, &adminID, []Entry{
		{Kind: KindSellerWithdrawing, OwnerID: w.SellerID, Amount: amount},
		{Kind: KindPlatformCash, Amount: "-" + amount},
	}); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE withdrawals SET status = 'paid', payout_ref = $2, paid_at = now() WHERE id = $1`, id, payoutRef); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.GetWithdrawal(ctx, 0, id, false)
}

// ---- 人工调整 ----

// Adjust 管理员调账：正数为给买家加余额（同时加 sub2api 余额），负数为扣减。必须填写原因。
func (f *Funds) Adjust(ctx context.Context, adminID, buyerUserID int64, amount float64, reason string, anomalyID *int64) error {
	if reason == "" {
		return httpx.BadRequest("REASON_REQUIRED", "adjustment reason is required")
	}
	amt, err := ValidAmount(math.Abs(amount))
	if err != nil {
		return err
	}
	ident, err := f.Shadow.Ensure(ctx, buyer.KindBuyer, buyerUserID)
	if err != nil {
		return err
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ref := fmt.Sprintf("adjust:%d:%d", buyerUserID, time.Now().UnixNano())
	buyerAmt, platformAmt, opType := "-"+amt, amt, opshandlers.TypeUserBalanceAdd
	if amount < 0 {
		buyerAmt, platformAmt, opType = amt, "-"+amt, opshandlers.TypeUserBalanceSubtract
	}
	txnID, err := Post(ctx, tx, "adjust", ref, reason, &adminID, []Entry{
		{Kind: KindPlatformAdjustment, Amount: platformAmt},
		{Kind: KindBuyerCash, OwnerID: buyerUserID, Amount: buyerAmt},
	})
	if err != nil {
		return err
	}
	if _, err := ops.Enqueue(ctx, tx, ops.NewOp{
		Type:           opType,
		Target:         fmt.Sprintf("user:%d", *ident.Sub2APIUserID),
		Payload:        opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: math.Abs(amount), Notes: "tm adjust: " + reason},
		IdempotencyKey: "tm-" + ref,
		Priority:       ops.PriorityUrgent,
	}); err != nil {
		return err
	}
	if anomalyID != nil {
		if _, err := tx.Exec(ctx, `UPDATE billing_anomalies SET status = 'adjusted', ledger_txn_id = $2, note = $3, resolved_at = now() WHERE id = $1`,
			*anomalyID, txnID, reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
