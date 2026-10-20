package httpapi

import (
	"net/http"

	"github.com/yx-token/idlicat/market/internal/billing"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func (s *Server) purchaseCreditPack(w http.ResponseWriter, r *http.Request) error {
	var in billing.PurchaseCreditPackInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Funds.PurchaseCreditPack(r.Context(), httpx.MustUser(r).UserID, in)
	if err != nil {
		return err
	}
	return created(w, p)
}
func (s *Server) buyerCreditPacks(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Funds.ListCreditPacks(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, p)
}
func (s *Server) refundCreditPack(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Funds.RefundCreditPack(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, p)
}
