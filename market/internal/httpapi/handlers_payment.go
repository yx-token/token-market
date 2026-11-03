package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

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
	var selectedID int64
	if raw := r.PathValue("instance"); raw != "" {
		selectedID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || selectedID <= 0 {
			http.NotFound(w, r)
			return
		}
	}
	p := reg.GetInstance(name, selectedID)
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
	result, err := s.Funds.ApplyTrade(r.Context(), name, t, selectedID)
	s.Funds.RecordNotification(r.Context(), name, t, result, err)
	if err != nil {
		s.Log.Error("apply payment notification", "provider", name, "out_trade_no", t.OutTradeNo, "err", err)
	}
	p.AckNotify(w, err)
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
