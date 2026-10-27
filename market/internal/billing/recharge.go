package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/platform/randx"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
)

// ---- 充值 ----
//
// 订单状态：pending（待支付）→ crediting（已支付，正在给 sub2api 加余额）→ credited（已到账）；
// 未支付的订单会变为 cancelled（用户取消）或 expired（超时或渠道关单）。渠道确认已支付的订单无论当前是否已取消、
// 已过期都会入账——钱已经收到。到账的依据只有两个：验签通过的异步通知，或主动查单的结果。

type RechargeOrder struct {
	ID          int64        `json:"id"`
	UserID      int64        `json:"user_id"`
	UserEmail   string       `json:"user_email,omitempty"`
	Amount      float64      `json:"amount"`
	Provider    string       `json:"provider"`
	Channel     string       `json:"channel"`
	OutTradeNo  *string      `json:"out_trade_no,omitempty"`
	ProviderRef *string      `json:"provider_ref,omitempty"`
	Status      string       `json:"status"`
	CloseReason *string      `json:"close_reason,omitempty"`
	Refunded    float64      `json:"refunded_amount"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   *time.Time   `json:"expires_at,omitempty"`
	PaidAt      *time.Time   `json:"paid_at,omitempty"`
	CreditedAt  *time.Time   `json:"credited_at,omitempty"`
	Payment     *PaymentInfo `json:"payment,omitempty"` // 待支付订单的支付方式，前端据此继续支付
}

// PaymentInfo 告诉前端如何完成支付。
type PaymentInfo struct {
	Channel      string `json:"channel"`
	Kind         string `json:"kind"` // redirect / qrcode / manual
	URL          string `json:"url,omitempty"`
	QRCode       string `json:"qr_code,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

const rechargeColumns = `id, user_id, amount::float8, provider, channel, out_trade_no, provider_ref, status, close_reason,
	refunded_amount::float8, created_at, expires_at, paid_at, credited_at, pay_kind, pay_payload, last_synced_at`

type rechargeRow struct {
	RechargeOrder
	payKind, payPayload *string
	lastSynced          *time.Time
}

func scanRecharge(row pgx.Row) (*rechargeRow, error) {
	var o rechargeRow
	if err := row.Scan(&o.ID, &o.UserID, &o.Amount, &o.Provider, &o.Channel, &o.OutTradeNo, &o.ProviderRef, &o.Status, &o.CloseReason,
		&o.Refunded, &o.CreatedAt, &o.ExpiresAt, &o.PaidAt, &o.CreditedAt, &o.payKind, &o.payPayload, &o.lastSynced); err != nil {
		return nil, err
	}
	if o.Status == "pending" {
		switch {
		case o.Channel == payment.ChannelManual:
			o.Payment = &PaymentInfo{Channel: o.Channel, Kind: "manual", Instructions: manualInstructions}
		case o.payKind != nil && o.payPayload != nil:
			o.Payment = &PaymentInfo{Channel: o.Channel, Kind: *o.payKind}
			if *o.payKind == "redirect" {
				o.Payment.URL = *o.payPayload
			} else {
				o.Payment.QRCode = *o.payPayload
			}
		}
	}
	return &o, nil
}

const manualInstructions = "请按平台公布的收款方式付款并备注订单号，管理员确认到账后自动入账"

// RechargeChannels 返回买家当前可用的支付方式：在线渠道需要已配置商户信息且后台开关打开。
func (f *Funds) RechargeChannels(ctx context.Context) ([]string, error) {
	st, err := f.Settings(ctx)
	if err != nil {
		return nil, err
	}
	configured := f.registry(ctx).Configured()
	var out []string
	for _, ch := range payment.AllChannels {
		if !st.PaymentChannels[ch] {
			continue
		}
		if ch == payment.ChannelManual || configured[ch] {
			out = append(out, ch)
		}
	}
	return out, nil
}

// CreateRecharge 创建充值订单；在线渠道同时向渠道下单，返回收银台地址或二维码内容。
func (f *Funds) CreateRecharge(ctx context.Context, userID int64, amount float64, channel, ip string) (*RechargeOrder, error) {
	st, err := f.Settings(ctx)
	if err != nil {
		return nil, err
	}
	amt, err := ValidAmount(amount)
	if err != nil {
		return nil, err
	}
	if amount < st.RechargeMin || amount > st.RechargeMax {
		return nil, httpx.BadRequest("AMOUNT_OUT_OF_RANGE", fmt.Sprintf("recharge amount must be between %.2f and %.2f", st.RechargeMin, st.RechargeMax))
	}
	if channel == "" {
		channel = payment.ChannelManual
	}
	channels, err := f.RechargeChannels(ctx)
	if err != nil {
		return nil, err
	}
	available := false
	for _, c := range channels {
		available = available || c == channel
	}
	if !available {
		return nil, httpx.BadRequest("CHANNEL_UNAVAILABLE", "payment channel is not available")
	}
	provider := payment.ProviderOf(channel)
	var expires *time.Time
	if channel != payment.ChannelManual {
		t := f.now().Add(time.Duration(st.RechargeOrderTimeoutMinutes) * time.Minute)
		expires = &t
	}
	var id int64
	if err := f.DB.QueryRow(ctx, `INSERT INTO recharge_orders (user_id, amount, provider, channel, client_ip, expires_at)
		VALUES ($1, $2::numeric, $3, $4, NULLIF($5, ''), $6) RETURNING id`, userID, amt, provider, channel, ip, expires).Scan(&id); err != nil {
		return nil, err
	}
	if channel != payment.ChannelManual {
		if err := f.startPayment(ctx, id, channel, amt, ip, *expires, st.PaymentPublicURL); err != nil {
			return nil, err
		}
	}
	return f.GetRecharge(ctx, userID, id)
}

func (f *Funds) startPayment(ctx context.Context, id int64, channel, amt, ip string, expires time.Time, publicURL string) error {
	p := f.registry(ctx).Get(payment.ProviderOf(channel))
	if p == nil {
		return httpx.BadRequest("CHANNEL_UNAVAILABLE", "payment channel is not configured")
	}
	// 商户订单号只用字母数字，满足两家渠道的格式要求；随机后缀避免开发环境重建数据库后与渠道侧旧单号冲突
	outTradeNo := fmt.Sprintf("TMR%d%s", id, randx.String(6))
	fen, err := payment.Fen(amt)
	if err != nil {
		return err
	}
	order := payment.Order{OutTradeNo: outTradeNo, AmountFen: fen, Subject: "Token Market 余额充值", ExpireAt: expires, ClientIP: ip}
	if publicURL != "" {
		order.ReturnURL = fmt.Sprintf("%s/buyer/wallet?order=%d", publicURL, id)
	}
	if _, err := f.DB.Exec(ctx, "UPDATE recharge_orders SET out_trade_no = $2 WHERE id = $1", id, outTradeNo); err != nil {
		return err
	}
	intent, err := p.Create(ctx, channel, order)
	if err != nil {
		f.Log.Error("create payment", "order_id", id, "channel", channel, "err", err)
		_, _ = f.DB.Exec(ctx, "UPDATE recharge_orders SET status = 'failed', close_reason = $2 WHERE id = $1 AND status = 'pending'", id, truncate(err.Error(), 300))
		return httpx.Unavailable("PAYMENT_UNAVAILABLE", "payment channel is temporarily unavailable, please try another channel")
	}
	payload := intent.URL
	if intent.Kind == "qrcode" {
		payload = intent.QRCode
	}
	_, err = f.DB.Exec(ctx, "UPDATE recharge_orders SET pay_kind = $2, pay_payload = $3 WHERE id = $1", id, intent.Kind, payload)
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (f *Funds) GetRecharge(ctx context.Context, userID, orderID int64) (*RechargeOrder, error) {
	o, err := f.getRecharge(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	return &o.RechargeOrder, nil
}

func (f *Funds) getRecharge(ctx context.Context, userID, orderID int64) (*rechargeRow, error) {
	o, err := scanRecharge(f.DB.QueryRow(ctx, "SELECT "+rechargeColumns+" FROM recharge_orders WHERE id = $1 AND ($2 = 0 OR user_id = $2)", orderID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ORDER_NOT_FOUND", "recharge order not found")
	}
	return o, err
}

// RefreshRecharge 返回订单；待支付的在线订单距上次查单超过 5 秒时先向渠道查一次（前端轮询时用，弥补通知延迟）。
func (f *Funds) RefreshRecharge(ctx context.Context, userID, orderID int64) (*RechargeOrder, error) {
	o, err := f.getRecharge(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == "pending" && o.Channel != payment.ChannelManual && o.OutTradeNo != nil &&
		(o.lastSynced == nil || f.now().Sub(*o.lastSynced) > 5*time.Second) {
		if err := f.syncOrder(ctx, o, false); err != nil {
			f.Log.Warn("sync recharge order", "order_id", o.ID, "err", err)
		}
		return f.GetRecharge(ctx, userID, orderID)
	}
	return &o.RechargeOrder, nil
}

func (f *Funds) ListRecharges(ctx context.Context, userID int64, status string, limit, offset int) ([]RechargeOrder, error) {
	rows, err := f.DB.Query(ctx, "SELECT "+rechargeColumns+` FROM recharge_orders WHERE ($1 = 0 OR user_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY id DESC LIMIT $3 OFFSET $4`, userID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RechargeOrder
	for rows.Next() {
		o, err := scanRecharge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o.RechargeOrder)
	}
	return out, rows.Err()
}

// ConfirmRecharge 管理员确认人工充值单到账。在线支付的订单只能由渠道通知或查单确认。
func (f *Funds) ConfirmRecharge(ctx context.Context, orderID int64, providerRef string, confirmedBy *int64) (*RechargeOrder, error) {
	o, err := f.getRecharge(ctx, 0, orderID)
	if err != nil {
		return nil, err
	}
	if o.Channel != payment.ChannelManual {
		return nil, httpx.Conflict("ONLINE_ORDER", "online payment orders are confirmed by the payment channel")
	}
	if o.Status != "pending" {
		return nil, httpx.Conflict("ORDER_NOT_PENDING", "order is not pending")
	}
	if err := f.credit(ctx, o, providerRef, confirmedBy, nil); err != nil {
		return nil, err
	}
	return f.GetRecharge(ctx, 0, orderID)
}

// credit 记账并通过出站表给影子用户加 sub2api 余额（幂等键 = 订单号）。1:1 结算：充值 1 元 = sub2api 余额 1。
// 只有处于 from 状态（默认 pending）的订单会被入账，并发的重复确认返回 ORDER_NOT_PENDING。
func (f *Funds) credit(ctx context.Context, o *rechargeRow, providerRef string, confirmedBy *int64, paidAt *time.Time, from ...string) error {
	if len(from) == 0 {
		from = []string{"pending"}
	}
	ident, err := f.Shadow.Ensure(ctx, buyer.KindBuyer, o.UserID)
	if err != nil {
		return fmt.Errorf("prepare buyer account: %w", err)
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var amount string
	err = tx.QueryRow(ctx, `UPDATE recharge_orders SET status = 'crediting', provider_ref = COALESCE(NULLIF($2, ''), provider_ref),
			paid_at = COALESCE($3, now()), close_reason = NULL
		WHERE id = $1 AND status = ANY($4) AND (status <> 'failed' OR paid_at IS NULL) RETURNING amount::text`,
		o.ID, providerRef, paidAt, from).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.Conflict("ORDER_NOT_PENDING", "order is not pending")
	}
	if err != nil {
		return err
	}
	if _, err := Post(ctx, tx, "recharge", fmt.Sprintf("recharge:%d", o.ID), providerRef, confirmedBy, []Entry{
		{Kind: KindPlatformCash, Amount: amount},
		{Kind: KindBuyerCash, OwnerID: o.UserID, Amount: "-" + amount},
	}); err != nil {
		return err
	}
	amt, _ := strconv.ParseFloat(amount, 64)
	opID, err := ops.Enqueue(ctx, tx, ops.NewOp{
		Type:           opshandlers.TypeUserBalanceAdd,
		Target:         fmt.Sprintf("user:%d", *ident.Sub2APIUserID),
		Payload:        opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: amt, Notes: fmt.Sprintf("tm recharge %d", o.ID)},
		IdempotencyKey: fmt.Sprintf("tm-recharge-%d", o.ID),
		Priority:       ops.PriorityUrgent,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE recharge_orders SET op_id = $2 WHERE id = $1", o.ID, opID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CancelRecharge 取消未支付的订单；在线订单先向渠道关单，关单时发现已支付则转为入账。
func (f *Funds) CancelRecharge(ctx context.Context, userID, orderID int64) error {
	o, err := f.getRecharge(ctx, userID, orderID)
	if err != nil {
		return err
	}
	if o.Status != "pending" {
		return httpx.Conflict("ORDER_NOT_PENDING", "order is not pending")
	}
	if p := f.registry(ctx).Get(o.Provider); p != nil && o.OutTradeNo != nil {
		if err := p.Close(ctx, *o.OutTradeNo); err != nil {
			t, qerr := p.Query(ctx, *o.OutTradeNo)
			if qerr == nil && t.State == payment.TradePaid {
				if _, err := f.ApplyTrade(ctx, o.Provider, t); err != nil {
					return err
				}
				return httpx.Conflict("ORDER_PAID", "order has been paid and is being credited")
			}
			f.Log.Warn("close payment", "order_id", o.ID, "err", err)
			return httpx.Unavailable("PAYMENT_UNAVAILABLE", "cannot close the payment right now, please retry later")
		}
	}
	tag, err := f.DB.Exec(ctx, "UPDATE recharge_orders SET status = 'cancelled', close_reason = 'cancelled by user' WHERE id = $1 AND status = 'pending'", orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("ORDER_NOT_PENDING", "order is not pending")
	}
	return nil
}

// ApplyTrade 按渠道侧的交易状态推进订单，返回处理结果（写入通知日志）：
// 已支付且金额一致则入账（含已取消、已过期的订单），金额不一致只记录异常不入账；渠道已关单的待支付订单标记过期。
func (f *Funds) ApplyTrade(ctx context.Context, provider string, t *payment.Trade) (string, error) {
	o, err := scanRecharge(f.DB.QueryRow(ctx, "SELECT "+rechargeColumns+" FROM recharge_orders WHERE out_trade_no = $1", t.OutTradeNo))
	if errors.Is(err, pgx.ErrNoRows) {
		return "unknown_order", nil
	}
	if err != nil {
		return "error", err
	}
	if o.Provider != provider {
		return "ignored", nil
	}
	switch t.State {
	case payment.TradePaid:
		want, err := payment.Fen(strconv.FormatFloat(o.Amount, 'f', 2, 64))
		if err != nil {
			return "error", err
		}
		if t.AmountFen != want {
			f.Log.Error("payment amount mismatch", "order_id", o.ID, "want_fen", want, "got_fen", t.AmountFen)
			_, err := f.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, buyer_user_id, amount, detail)
				VALUES ('payment_amount_mismatch', $1, $2, $3, $4) ON CONFLICT (dedupe_key) DO NOTHING`,
				"PAY:"+t.OutTradeNo, o.UserID, float64(t.AmountFen)/100,
				map[string]any{"order_id": o.ID, "provider": provider, "trade_no": t.TradeNo, "order_fen": want, "paid_fen": t.AmountFen})
			return "amount_mismatch", err
		}
		// 已入账的订单（含加余额失败、待人工处理的 failed 订单）不再重复入账；failed 且未支付过的是下单失败的订单
		if o.Status == "crediting" || o.Status == "credited" || (o.Status == "failed" && o.PaidAt != nil) {
			return "duplicate", nil
		}
		err = f.credit(ctx, o, t.TradeNo, nil, t.PaidAt, "pending", "cancelled", "expired", "failed")
		var he *httpx.Error
		if errors.As(err, &he) && he.Code == "ORDER_NOT_PENDING" {
			return "duplicate", nil // 并发的通知与查单
		}
		if err != nil {
			return "error", err
		}
		return "credited", nil
	case payment.TradeClosed:
		if o.Status == "pending" {
			if _, err := f.DB.Exec(ctx, `UPDATE recharge_orders SET status = 'expired', close_reason = 'closed by payment channel'
				WHERE id = $1 AND status = 'pending'`, o.ID); err != nil {
				return "error", err
			}
			return "expired", nil
		}
	}
	return "ignored", nil
}

// RecordNotification 记录一次渠道通知的处理结果。
func (f *Funds) RecordNotification(ctx context.Context, provider string, t *payment.Trade, result string, procErr error) {
	var no, tradeNo, state *string
	var fen *int64
	if t != nil {
		no, tradeNo, state, fen = &t.OutTradeNo, &t.TradeNo, (*string)(&t.State), &t.AmountFen
	}
	var msg *string
	if procErr != nil {
		s := truncate(procErr.Error(), 500)
		msg = &s
	}
	if _, err := f.DB.Exec(ctx, `INSERT INTO payment_notifications (provider, out_trade_no, trade_no, state, amount_fen, result, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, provider, no, tradeNo, state, fen, result, msg); err != nil {
		f.Log.Warn("record payment notification", "err", err)
	}
}

// syncOrder 主动查单：已支付则入账，已关闭则过期；超过支付时限仍未支付时向渠道关单。closeExpired 为 false 时只查不关。
func (f *Funds) syncOrder(ctx context.Context, o *rechargeRow, closeExpired bool) error {
	p := f.registry(ctx).Get(o.Provider)
	if p == nil || o.OutTradeNo == nil {
		return nil
	}
	if _, err := f.DB.Exec(ctx, "UPDATE recharge_orders SET last_synced_at = now() WHERE id = $1", o.ID); err != nil {
		return err
	}
	t, err := p.Query(ctx, *o.OutTradeNo)
	switch {
	case errors.Is(err, payment.ErrTradeNotFound):
		t = &payment.Trade{OutTradeNo: *o.OutTradeNo, State: payment.TradePending}
	case err != nil:
		return err
	}
	if t.State != payment.TradePending {
		_, err := f.ApplyTrade(ctx, o.Provider, t)
		return err
	}
	if !closeExpired || o.Status != "pending" || o.ExpiresAt == nil || f.now().Before(o.ExpiresAt.Add(time.Minute)) {
		return nil
	}
	if err := p.Close(ctx, *o.OutTradeNo); err != nil {
		// 关单失败通常是刚刚付款成功，再查一次
		if t2, qerr := p.Query(ctx, *o.OutTradeNo); qerr == nil && t2.State == payment.TradePaid {
			_, err := f.ApplyTrade(ctx, o.Provider, t2)
			return err
		}
		return fmt.Errorf("close expired payment: %w", err)
	}
	_, err = f.DB.Exec(ctx, "UPDATE recharge_orders SET status = 'expired', close_reason = 'payment timeout' WHERE id = $1 AND status = 'pending'", o.ID)
	return err
}

// SyncPayments 由 worker 周期调用：补偿丢失的通知、关闭超时订单。待支付订单前 10 分钟每 20 秒查一次，之后每 2 分钟；
// 用户取消的订单在支付时限后 10 分钟内继续查询（当面付的二维码在关单前仍可能被扫码支付）。
func (f *Funds) SyncPayments(ctx context.Context) error {
	rows, err := f.DB.Query(ctx, "SELECT "+rechargeColumns+` FROM recharge_orders
		WHERE channel <> 'manual' AND out_trade_no IS NOT NULL AND created_at < now() - interval '20 seconds'
			AND (status = 'pending' OR (status = 'cancelled' AND expires_at > now() - interval '10 minutes'))
			AND (last_synced_at IS NULL OR last_synced_at < now() - CASE WHEN created_at > now() - interval '10 minutes'
				THEN interval '20 seconds' ELSE interval '2 minutes' END)
		ORDER BY id LIMIT 200`)
	if err != nil {
		return err
	}
	var orders []*rechargeRow
	for rows.Next() {
		o, err := scanRecharge(rows)
		if err != nil {
			rows.Close()
			return err
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range orders {
		if err := f.syncOrder(ctx, o, true); err != nil {
			f.Log.Warn("sync payment", "order_id", o.ID, "err", err)
		}
	}
	return nil
}

// TrackCrediting 推进 crediting 状态的订单：加余额操作完成则到账，进入死信则标记失败待人工处理。
func (f *Funds) TrackCrediting(ctx context.Context) error {
	if _, err := f.DB.Exec(ctx, `UPDATE recharge_orders r SET status = 'credited', credited_at = now()
		FROM sub2api_ops o WHERE o.id = r.op_id AND r.status = 'crediting' AND o.status = 'done'`); err != nil {
		return err
	}
	_, err := f.DB.Exec(ctx, `UPDATE recharge_orders r SET status = 'failed', close_reason = 'crediting sub2api balance failed'
		FROM sub2api_ops o WHERE o.id = r.op_id AND r.status = 'crediting' AND o.status = 'dead'`)
	return err
}
