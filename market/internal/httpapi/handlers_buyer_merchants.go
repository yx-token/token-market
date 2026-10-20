package httpapi

import (
	"net/http"

	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/market"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func (s *Server) buyerMerchants(w http.ResponseWriter, r *http.Request) error {
	userID := httpx.MustUser(r).UserID
	merchants, err := s.Keys.Merchants(r.Context(), userID)
	if err != nil {
		return err
	}
	items, err := s.Market.List(r.Context(), market.Filter{BuyerID: userID}, 2147483647, 0)
	if err != nil {
		return err
	}
	type view struct {
		buyer.Merchant
		Listings []market.Item `json:"listings"`
	}
	bySeller := map[int64][]market.Item{}
	for _, it := range items {
		bySeller[it.SellerID] = append(bySeller[it.SellerID], it)
	}
	out := make([]view, 0, len(merchants))
	for _, m := range merchants {
		ls := bySeller[m.SellerID]
		if ls == nil {
			ls = []market.Item{}
		}
		out = append(out, view{Merchant: m, Listings: ls})
	}
	return ok(w, out)
}

func (s *Server) buyerMerchantListings(w http.ResponseWriter, r *http.Request) error {
	items, err := s.Market.List(r.Context(), market.Filter{BuyerID: httpx.MustUser(r).UserID}, 2147483647, 0)
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) addBuyerMerchant(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Keys.AddMerchant(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "added"})
}

func (s *Server) removeBuyerMerchant(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Keys.RemoveMerchant(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "removed"})
}
