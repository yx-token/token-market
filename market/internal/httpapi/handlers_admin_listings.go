package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func (s *Server) adminListListings(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	f := listing.AdminListFilter{
		Status:   r.URL.Query().Get("status"),
		Platform: r.URL.Query().Get("platform"),
		Q:        r.URL.Query().Get("q"),
		Limit:    limit,
		Offset:   offset,
	}
	if v := r.URL.Query().Get("seller_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return httpx.BadRequest("INVALID_SELLER_ID", "seller_id must be int")
		}
		f.SellerID = id
	}
	if v := r.URL.Query().Get("upstream_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return httpx.BadRequest("INVALID_UPSTREAM_ID", "upstream_id must be int")
		}
		f.UpstreamID = id
	}
	rows, err := s.Listings.AdminListListings(r.Context(), f)
	if err != nil {
		return err
	}
	return ok(w, rows)
}

func (s *Server) adminGetListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	row, err := s.Listings.AdminGetListing(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, row)
}

func (s *Server) adminCreateListing(w http.ResponseWriter, r *http.Request) error {
	var in listing.AdminCreateListingInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	l, q, err := s.Listings.AdminCreateListing(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "listing.admin_create", fmt.Sprintf("listing:%d", l.ID), in)
	out := map[string]any{"listing": l}
	if q != nil {
		out["quote"] = q
	}
	return created(w, out)
}

func (s *Server) adminUpdateListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.AdminUpdateListingInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	l, err := s.Listings.AdminUpdateListing(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "listing.admin_update", fmt.Sprintf("listing:%d", id), in)
	return ok(w, l)
}

func (s *Server) adminPauseListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	l, err := s.Listings.AdminPauseListing(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "listing.admin_pause", fmt.Sprintf("listing:%d", id), nil)
	return ok(w, l)
}

func (s *Server) adminResumeListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	l, err := s.Listings.AdminResumeListing(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "listing.admin_resume", fmt.Sprintf("listing:%d", id), nil)
	return ok(w, l)
}

func (s *Server) adminCloseListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Listings.AdminCloseListing(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "listing.admin_close", fmt.Sprintf("listing:%d", id), nil)
	return ok(w, map[string]string{"status": "closed"})
}
