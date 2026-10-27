package httpapi

import (
	"context"

	"github.com/yx-token/token-market/market/internal/billing"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/usermail"
	"github.com/yx-token/token-market/market/internal/quality"
)

func (s *Server) attachProxySellerEmails(ctx context.Context, ps []listing.Proxy) error {
	ids := make([]int64, 0, len(ps))
	for _, p := range ps {
		if p.SellerID != nil && *p.SellerID > 0 {
			ids = append(ids, *p.SellerID)
		}
	}
	emails, err := usermail.Lookup(ctx, s.DB, ids)
	if err != nil {
		return err
	}
	for i := range ps {
		if ps[i].SellerID != nil {
			ps[i].SellerEmail = emails[*ps[i].SellerID]
		}
	}
	return nil
}

func (s *Server) attachAppealSellerEmails(ctx context.Context, as []quality.Appeal) error {
	ids := make([]int64, 0, len(as))
	for _, a := range as {
		ids = append(ids, a.SellerID)
	}
	emails, err := usermail.Lookup(ctx, s.DB, ids)
	if err != nil {
		return err
	}
	for i := range as {
		as[i].SellerEmail = emails[as[i].SellerID]
	}
	return nil
}

func (s *Server) attachWithdrawalSellerEmails(ctx context.Context, ws []billing.Withdrawal) error {
	ids := make([]int64, 0, len(ws))
	for _, w := range ws {
		ids = append(ids, w.SellerID)
	}
	emails, err := usermail.Lookup(ctx, s.DB, ids)
	if err != nil {
		return err
	}
	for i := range ws {
		ws[i].SellerEmail = emails[ws[i].SellerID]
	}
	return nil
}

func (s *Server) attachRechargeUserEmails(ctx context.Context, os []billing.RechargeOrder) error {
	ids := make([]int64, 0, len(os))
	for _, o := range os {
		ids = append(ids, o.UserID)
	}
	emails, err := usermail.Lookup(ctx, s.DB, ids)
	if err != nil {
		return err
	}
	for i := range os {
		os[i].UserEmail = emails[os[i].UserID]
	}
	return nil
}

func (s *Server) attachRefundUserEmails(ctx context.Context, rs []billing.Refund) error {
	ids := make([]int64, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.UserID)
	}
	emails, err := usermail.Lookup(ctx, s.DB, ids)
	if err != nil {
		return err
	}
	for i := range rs {
		rs[i].UserEmail = emails[rs[i].UserID]
	}
	return nil
}
