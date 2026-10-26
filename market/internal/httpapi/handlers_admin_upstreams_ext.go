package httpapi

import (
	"fmt"
	"net/http"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func (s *Server) adminCreateUpstream(w http.ResponseWriter, r *http.Request) error {
	var in listing.AdminCreateUpstreamInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.AdminCreateUpstream(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.create", fmt.Sprintf("upstream:%d", u.ID), map[string]any{
		"seller_id": in.SellerID, "platform": in.Platform, "auth_type": in.AuthType,
	})
	return created(w, u)
}

func (s *Server) adminGetUpstream(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := s.Listings.AdminGetUpstream(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) adminUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	models, err := s.Listings.AdminGetModels(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, models)
}

func (s *Server) adminRefreshUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	models, err := s.Listings.AdminRefreshModels(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.models.refresh", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, models)
}

func (s *Server) adminSetUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Models []string `json:"models"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.AdminSetModels(r.Context(), id, in.Models)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.models.set", fmt.Sprintf("upstream:%d", id), map[string]any{"models": in.Models})
	return ok(w, u)
}

func (s *Server) adminStartUpstreamOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var opts listing.AuthorizationOptions
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &opts); err != nil {
			return err
		}
	}
	st, err := s.Listings.AdminStartAuthorization(r.Context(), id, opts)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.oauth.start", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, st)
}

func (s *Server) adminCompleteUpstreamOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.AdminCompleteAuthorization(r.Context(), id, in.Code)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.oauth.complete", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, u)
}

func (s *Server) adminSubmitUpstreamCodingKey(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.CodingKeyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.AdminSubmitCodingKey(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.coding_key", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, u)
}

func (s *Server) adminClearDeadEngineAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := s.Listings.AdminClearDeadEngineAccount(r.Context(), id)
	if err != nil {
		return err
	}
	s.audit(r, "upstream.clear_dead_account", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, u)
}
