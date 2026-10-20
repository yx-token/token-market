package httpapi

import (
	"net/http"
	"strconv"

	"github.com/yx-token/idlicat/market/internal/listing"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func (s *Server) createCreditOffer(w http.ResponseWriter, r *http.Request) error {
	var in listing.CreditOfferInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	offer, err := listing.CreateCreditOffer(r.Context(), s.DB, httpx.MustUser(r).UserID, in)
	if err != nil {
		return err
	}
	return created(w, offer)
}

func (s *Server) updateCreditOffer(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.CreditOfferInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	offer, err := listing.UpdateCreditOffer(r.Context(), s.DB, httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return ok(w, offer)
}

func (s *Server) sellerCreditOffers(w http.ResponseWriter, r *http.Request) error {
	offers, err := listing.ListCreditOffers(r.Context(), s.DB, httpx.MustUser(r).UserID, false)
	if err != nil {
		return err
	}
	return ok(w, offers)
}

func (s *Server) marketCreditOffers(w http.ResponseWriter, r *http.Request) error {
	sellerID, err := strconv.ParseInt(r.URL.Query().Get("seller_id"), 10, 64)
	if err != nil || sellerID <= 0 {
		return httpx.BadRequest("INVALID_SELLER", "seller_id must be a positive integer")
	}
	offers, err := listing.ListCreditOffers(r.Context(), s.DB, sellerID, true)
	if err != nil {
		return err
	}
	return ok(w, offers)
}
