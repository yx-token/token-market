package httpapi

import (
	"net/http"
	"strconv"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/review"
)

func (s *Server) reviews() *review.Service {
	if s.Reviews != nil {
		return s.Reviews
	}
	return &review.Service{DB: s.DB}
}

func (s *Server) createReview(w http.ResponseWriter, r *http.Request) error {
	var in review.CreateInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.reviews().Create(r.Context(), httpx.MustUser(r).UserID, in)
	if err != nil {
		return err
	}
	return created(w, out)
}

func (s *Server) followUpReview(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in review.FollowUpInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.reviews().FollowUp(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return created(w, out)
}

func (s *Server) myReviews(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.reviews().ListMine(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, rows)
}

func (s *Server) reviewEligible(w http.ResponseWriter, r *http.Request) error {
	sellerID, _ := strconv.ParseInt(r.URL.Query().Get("seller_id"), 10, 64)
	if sellerID > 0 {
		out, err := s.reviews().Eligible(r.Context(), httpx.MustUser(r).UserID, sellerID)
		if err != nil {
			return err
		}
		return ok(w, out)
	}
	rows, err := s.reviews().EligibleList(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, rows)
}

func (s *Server) marketSellerReviews(w http.ResponseWriter, r *http.Request) error {
	sellerID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	listingID, _ := strconv.ParseInt(r.URL.Query().Get("listing_id"), 10, 64)
	onlyListing := r.URL.Query().Get("only_listing") == "1" || r.URL.Query().Get("only_listing") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rows, err := s.reviews().ListPublic(r.Context(), sellerID, listingID, onlyListing, limit, offset)
	if err != nil {
		return err
	}
	st, _ := s.reviews().Stats(r.Context(), sellerID)
	return ok(w, map[string]any{"stats": st, "items": rows})
}

func (s *Server) marketSellerRating(w http.ResponseWriter, r *http.Request) error {
	sellerID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	st, err := s.reviews().Stats(r.Context(), sellerID)
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) sellerMyReviews(w http.ResponseWriter, r *http.Request) error {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rows, err := s.reviews().ListForSeller(r.Context(), httpx.MustUser(r).UserID, limit, offset)
	if err != nil {
		return err
	}
	st, _ := s.reviews().Stats(r.Context(), httpx.MustUser(r).UserID)
	return ok(w, map[string]any{"stats": st, "items": rows})
}

func (s *Server) adminListReviews(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	sellerID, _ := strconv.ParseInt(q.Get("seller_id"), 10, 64)
	buyerID, _ := strconv.ParseInt(q.Get("buyer_id"), 10, 64)
	listingID, _ := strconv.ParseInt(q.Get("listing_id"), 10, 64)
	minR, _ := strconv.Atoi(q.Get("min_rating"))
	maxR, _ := strconv.Atoi(q.Get("max_rating"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	rows, total, err := s.reviews().AdminList(r.Context(), review.ListFilter{
		SellerID: sellerID, BuyerID: buyerID, ListingID: listingID,
		Status: q.Get("status"), MinRating: minR, MaxRating: maxR,
		Q: q.Get("q"), Limit: limit, Offset: offset,
	})
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"items": rows, "total": total})
}

func (s *Server) adminHideReview(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = httpx.Decode(r, &in)
	if err := s.reviews().Hide(r.Context(), id, httpx.MustUser(r).UserID, in.Reason); err != nil {
		return err
	}
	s.audit(r, "review.hide", "review:"+strconv.FormatInt(id, 10), in)
	return ok(w, map[string]string{"status": "hidden"})
}

func (s *Server) adminUnhideReview(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.reviews().Unhide(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "review.unhide", "review:"+strconv.FormatInt(id, 10), nil)
	return ok(w, map[string]string{"status": "visible"})
}

func (s *Server) adminDeleteReview(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = httpx.Decode(r, &in)
	if err := s.reviews().Delete(r.Context(), id, httpx.MustUser(r).UserID, in.Reason); err != nil {
		return err
	}
	s.audit(r, "review.delete", "review:"+strconv.FormatInt(id, 10), in)
	return ok(w, map[string]string{"status": "deleted"})
}
