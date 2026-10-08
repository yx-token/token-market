package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// ---- 退款打款 ----
//
// 余额退款扣减 sub2api 余额并记账后进入 paying：按充值单从新到旧，把退款金额原路退回到可退款期限内的在线充值单
// （每单最多退其未退部分），剩余部分（人工充值、超过退款期限、渠道未配置）生成一条人工打款明细。
// 原路退回由 worker 调用渠道退款接口完成；全部明细成功后退款完成。

type RefundPayout struct {
	ID              int64      `json:"id"`
	RefundID        int64      `json:"refund_id"`
	RechargeOrderID *int64     `json:"recharge_order_id,omitempty"`
	Channel         string     `json:"channel"`
	Amount          float64    `json:"amount"`
	OutRefundNo     *string    `json:"out_refund_no,omitempty"`
	Status          string     `json:"status"` // pending / processing / success / failed / manual
	ProviderRef     *string    `json:"provider_ref,omitempty"`
	Error           *string    `json:"error,omitempty"`
	Attempts        int        `json:"attempts"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

const payoutColumns = `id, refund_id, recharge_order_id, channel, amount::float8, out_refund_no, status, provider_ref, error, attempts, created_at, finished_at`

func scanPayout(row pgx.CollectableRow) (RefundPayout, error) {
	var p RefundPayout
	err := row.Scan(&p.ID, &p.RefundID, &p.RechargeOrderID, &p.Channel, &p.Amount, &p.OutRefundNo, &p.Status, &p.ProviderRef, &p.Error,
		&p.Attempts, &p.CreatedAt, &p.FinishedAt)
	return p, err
}

func (f *Funds) refundPayouts(ctx context.Context, refundID int64) ([]RefundPayout, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+payoutColumns+" FROM refund_payouts WHERE refund_id = $1 ORDER BY id", refundID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, scanPayout)
	if out == nil {
		out = []RefundPayout{}
	}
	return out, err
}

// planPayouts 在退款记账的事务中生成打款明细，并占用充值单的可退金额。
func (f *Funds) planPayouts(ctx context.Context, tx pgx.Tx, refundID, userID int64, amount string) error {
	st, err := f.Settings(ctx)
	if err != nil {
		return err
	}
	remaining, err := payment.Fen(amount)
	if err != nil {
		return err
	}
	reg := f.registry(ctx)
	if st.RefundOriginalRoute {
		rows, err := tx.Query(ctx, `SELECT id, channel, amount::text, refunded_amount::text FROM recharge_orders
			WHERE user_id = $1 AND status = 'credited' AND channel <> 'manual' AND out_trade_no IS NOT NULL
				AND amount > refunded_amount AND created_at > now() - make_interval(days => $2)
			ORDER BY id DESC FOR UPDATE`, userID, st.RefundOriginalRouteDays)
		if err != nil {
			return err
		}
		type cand struct {
			id               int64
			channel          string
			amount, refunded string
		}
		var cands []cand
		for rows.Next() {
			var c cand
			if err := rows.Scan(&c.id, &c.channel, &c.amount, &c.refunded); err != nil {
				rows.Close()
				return err
			}
			cands = append(cands, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range cands {
			if remaining <= 0 {
				break
			}
			if reg.Get(payment.ProviderOf(c.channel)) == nil {
				continue // 渠道已下线，无法原路退回
			}
			total, err := payment.Fen(c.amount)
			if err != nil {
				return err
			}
			refunded, err := fenOrZero(c.refunded)
			if err != nil {
				return err
			}
			take := min(remaining, total-refunded)
			if take <= 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO refund_payouts (refund_id, recharge_order_id, channel, amount, out_refund_no)
				VALUES ($1, $2, $3, $4::numeric, $5)`, refundID, c.id, c.channel, payment.Yuan(take), fmt.Sprintf("TMF%dO%d", refundID, c.id)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE recharge_orders SET refunded_amount = refunded_amount + $2::numeric WHERE id = $1",
				c.id, payment.Yuan(take)); err != nil {
				return err
			}
			remaining -= take
		}
	}
	if remaining > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO refund_payouts (refund_id, channel, amount, status) VALUES ($1, 'manual', $2::numeric, 'manual')`,
			refundID, payment.Yuan(remaining)); err != nil {
			return err
		}
	}
	return nil
}

func fenOrZero(s string) (int64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, nil
	}
	return payment.Fen(strconv.FormatFloat(v, 'f', 2, 64))
}

var payoutBackoff = []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// AdvanceRefundPayouts 由 worker 周期调用：发起或查询原路退款，全部成功的退款标记完成。
func (f *Funds) AdvanceRefundPayouts(ctx context.Context) error {
	rows, err := f.DB.Query(ctx, `SELECT p.id, p.channel, p.amount::text, p.out_refund_no, p.status, p.attempts, o.out_trade_no, o.amount::text
		FROM refund_payouts p JOIN recharge_orders o ON o.id = p.recharge_order_id
		WHERE p.status IN ('pending', 'processing') AND p.next_attempt_at <= now() ORDER BY p.id LIMIT 100`)
	if err != nil {
		return err
	}
	type item struct {
		id                   int64
		channel, amount      string
		outRefundNo, status  string
		attempts             int
		outTradeNo, orderAmt string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.channel, &it.amount, &it.outRefundNo, &it.status, &it.attempts, &it.outTradeNo, &it.orderAmt); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	reg := f.registry(ctx)
	for _, it := range items {
		p := reg.Get(payment.ProviderOf(it.channel))
		if p == nil {
			if err := f.setPayout(ctx, it.id, "failed", nil, "payment channel is not configured", it.attempts, 0); err != nil {
				return err
			}
			continue
		}
		amt, err1 := payment.Fen(it.amount)
		total, err2 := payment.Fen(it.orderAmt)
		if err := errors.Join(err1, err2); err != nil {
			return err
		}
		var res *payment.RefundResult
		if it.status == "processing" {
			res, err = p.QueryRefund(ctx, it.outTradeNo, it.outRefundNo)
			if errors.Is(err, payment.ErrRefundNotFound) {
				err, res = nil, nil // 渠道没有收到：同一退款单号重新发起是幂等的
			}
		}
		if err == nil && res == nil {
			res, err = p.Refund(ctx, payment.Refund{OutTradeNo: it.outTradeNo, OutRefundNo: it.outRefundNo, AmountFen: amt, TotalFen: total,
				Reason: "余额退款"})
		}
		attempts := it.attempts + 1
		switch {
		case err != nil:
			f.Log.Warn("refund payout", "payout_id", it.id, "err", err)
			status := it.status
			if attempts >= 10 {
				status = "failed"
			}
			err = f.setPayout(ctx, it.id, status, nil, err.Error(), attempts, payoutBackoff[min(attempts-1, len(payoutBackoff)-1)])
		case res.State == payment.RefundSuccess:
			err = f.setPayout(ctx, it.id, "success", &res.RefundNo, "", attempts, 0)
		case res.State == payment.RefundFailed:
			err = f.setPayout(ctx, it.id, "failed", nil, res.Message, attempts, 0)
		default:
			err = f.setPayout(ctx, it.id, "processing", &res.RefundNo, "", attempts, payoutBackoff[min(attempts-1, len(payoutBackoff)-1)])
		}
		if err != nil {
			return err
		}
	}
	return f.finishRefunds(ctx)
}

func (f *Funds) setPayout(ctx context.Context, id int64, status string, ref *string, msg string, attempts int, retryIn time.Duration) error {
	if ref != nil && *ref == "" {
		ref = nil
	}
	_, err := f.DB.Exec(ctx, `UPDATE refund_payouts SET status = $2, provider_ref = COALESCE($3, provider_ref), error = NULLIF($4, ''), attempts = $5,
			next_attempt_at = now() + make_interval(secs => $6), finished_at = CASE WHEN $2 IN ('success', 'failed') THEN now() END
		WHERE id = $1`, id, status, ref, truncate(msg, 500), attempts, retryIn.Seconds())
	return err
}

// finishRefunds 把全部打款明细都已成功的退款标记完成。
func (f *Funds) finishRefunds(ctx context.Context) error {
	_, err := f.DB.Exec(ctx, `UPDATE refunds r SET status = 'done', finished_at = now(),
			payout_ref = (SELECT string_agg(COALESCE(p.provider_ref, p.channel), ',' ORDER BY p.id) FROM refund_payouts p WHERE p.refund_id = r.id)
		WHERE r.status = 'paying' AND EXISTS (SELECT 1 FROM refund_payouts p WHERE p.refund_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM refund_payouts p WHERE p.refund_id = r.id AND p.status <> 'success')`)
	return err
}

// RetryPayout 管理员重试失败的原路退款。
func (f *Funds) RetryPayout(ctx context.Context, payoutID int64) (*RefundPayout, error) {
	tag, err := f.DB.Exec(ctx, `UPDATE refund_payouts SET status = 'pending', attempts = 0, error = NULL, next_attempt_at = now(), finished_at = NULL
		WHERE id = $1 AND status = 'failed' AND recharge_order_id IS NOT NULL`, payoutID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.Conflict("PAYOUT_NOT_FAILED", "only failed original-route payouts can be retried")
	}
	return f.getPayout(ctx, payoutID)
}

// PayoutToManual 管理员把失败的原路退款改为人工打款，并释放原充值单的可退金额。
func (f *Funds) PayoutToManual(ctx context.Context, payoutID int64) (*RefundPayout, error) {
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var orderID *int64
	var amount string
	err = tx.QueryRow(ctx, `UPDATE refund_payouts SET status = 'manual', channel = 'manual', finished_at = NULL
		WHERE id = $1 AND status = 'failed' RETURNING recharge_order_id, amount::text`, payoutID).Scan(&orderID, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.Conflict("PAYOUT_NOT_FAILED", "only failed payouts can be switched to manual payout")
	}
	if err != nil {
		return nil, err
	}
	if orderID != nil {
		if _, err := tx.Exec(ctx, "UPDATE recharge_orders SET refunded_amount = refunded_amount - $2::numeric WHERE id = $1", *orderID, amount); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return f.getPayout(ctx, payoutID)
}

func (f *Funds) getPayout(ctx context.Context, id int64) (*RefundPayout, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+payoutColumns+" FROM refund_payouts WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanPayout)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PAYOUT_NOT_FOUND", "payout not found")
	}
	return &p, err
}
