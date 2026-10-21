package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/payment/merchants"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// paymentNotify 处理渠道的异步通知：验签 → 推进订单 → 按渠道要求应答。不经过登录鉴权，安全性完全依赖验签。
func (s *Server) paymentNotify(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	reg, err := s.Payments.Registry(r.Context())
	if err != nil {
		s.Log.Error("load payment merchants", "err", err)
		http.Error(w, "payment configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	// 停用的商户仍处理通知：已创建的订单可能还会付款
	p := reg.Get(name)
	if p == nil {
		http.NotFound(w, r)
		return
	}
	t, err := p.ParseNotify(r)
	if err != nil {
		s.Log.Warn("invalid payment notification", "provider", name, "err", err)
		s.Funds.RecordNotification(r.Context(), name, nil, "invalid", err)
		p.AckNotify(w, err)
		return
	}
	if t == nil {
		p.AckNotify(w, nil)
		return
	}
	result, err := s.Funds.ApplyTrade(r.Context(), name, t)
	s.Funds.RecordNotification(r.Context(), name, t, result, err)
	if err != nil {
		s.Log.Error("apply payment notification", "provider", name, "out_trade_no", t.OutTradeNo, "err", err)
	}
	p.AckNotify(w, err)
}

type paymentChannelStatus struct {
	Channel    string `json:"channel"`
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"` // 配置文件中已配置商户信息
	Enabled    bool   `json:"enabled"`    // 后台开关
	Available  bool   `json:"available"`  // 买家可用
}

type paymentNotification struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	OutTradeNo *string   `json:"out_trade_no,omitempty"`
	TradeNo    *string   `json:"trade_no,omitempty"`
	State      *string   `json:"state,omitempty"`
	AmountFen  *int64    `json:"amount_fen,omitempty"`
	Result     string    `json:"result"`
	Error      *string   `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) paymentStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := s.settings(r.Context())
	if err != nil {
		return err
	}
	available, err := s.Funds.RechargeChannels(r.Context())
	if err != nil {
		return err
	}
	avail := map[string]bool{}
	for _, c := range available {
		avail[c] = true
	}
	reg, err := s.Payments.Registry(r.Context())
	if err != nil {
		return err
	}
	configured := reg.Configured()
	var channels []paymentChannelStatus
	for _, c := range payment.AllChannels {
		channels = append(channels, paymentChannelStatus{Channel: c, Provider: payment.ProviderOf(c),
			Configured: c == payment.ChannelManual || configured[c], Enabled: st.PaymentChannels[c], Available: avail[c]})
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id, provider, out_trade_no, trade_no, state, amount_fen, result, error, created_at
		FROM payment_notifications ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	notes := []paymentNotification{}
	for rows.Next() {
		var n paymentNotification
		if err := rows.Scan(&n.ID, &n.Provider, &n.OutTradeNo, &n.TradeNo, &n.State, &n.AmountFen, &n.Result, &n.Error, &n.CreatedAt); err != nil {
			return err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return ok(w, map[string]any{"channels": channels, "public_url": st.PaymentPublicURL, "notifications": notes})
}

func (s *Server) syncRecharge(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	o, err := s.Funds.RefreshRecharge(r.Context(), 0, id)
	if err != nil {
		return err
	}
	s.audit(r, "recharge.sync", fmt.Sprintf("recharge:%d", id), nil)
	return ok(w, o)
}

func (s *Server) retryPayout(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Funds.RetryPayout(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "refund_payout.retry", fmt.Sprintf("refund_payout:%d", id), nil)
	return ok(w, p)
}

func (s *Server) payoutToManual(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Funds.PayoutToManual(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "refund_payout.manual", fmt.Sprintf("refund_payout:%d", id), nil)
	return ok(w, p)
}

func (s *Server) listMerchants(w http.ResponseWriter, r *http.Request) error {
	out := map[string]*merchants.View{}
	for _, p := range []string{payment.ProviderAlipay, payment.ProviderWechat} {
		v, err := s.Payments.Get(r.Context(), p)
		if err != nil {
			return err
		}
		out[p] = v
	}
	return ok(w, out)
}

func (s *Server) updateMerchant(w http.ResponseWriter, r *http.Request) error {
	var in merchants.Input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	provider := r.PathValue("provider")
	v, err := s.Payments.Update(r.Context(), provider, in, adminID(r))
	if err != nil {
		return err
	}
	// 审计日志不记录密钥
	s.audit(r, "payment_merchant.update", "payment_merchant:"+provider, map[string]any{
		"enabled": in.Enabled, "alipay": in.Alipay, "wechat": in.Wechat,
		"private_key_changed": in.PrivateKey != "", "api_v3_key_changed": in.APIv3Key != "",
	})
	return ok(w, v)
}

func (s *Server) testMerchant(w http.ResponseWriter, r *http.Request) error {
	if err := s.Payments.Test(r.Context(), r.PathValue("provider")); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}
