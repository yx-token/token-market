package httpapi

import (
	"fmt"
	"net/http"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
)

func (s *Server) listSellerProxies(w http.ResponseWriter, r *http.Request) error {
	ps, err := s.Proxies.ListSeller(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) createSellerProxy(w http.ResponseWriter, r *http.Request) error {
	var in listing.CreateProxyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Proxies.CreateSeller(r.Context(), httpx.MustUser(r).UserID, in)
	if err != nil {
		return err
	}
	s.audit(r, "seller_proxy.create", fmt.Sprintf("proxy:%d", p.ID), map[string]any{"host": p.Host, "port": p.Port})
	return created(w, p)
}

func (s *Server) syncSellerProxy(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Proxies.SyncSeller(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, p)
}

func (s *Server) setSellerProxyDisabled(disabled bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		p, err := s.Proxies.SetSellerDisabled(r.Context(), httpx.MustUser(r).UserID, id, disabled)
		if err != nil {
			return err
		}
		s.audit(r, "seller_proxy.set_disabled", fmt.Sprintf("proxy:%d", id), map[string]bool{"disabled": disabled})
		return ok(w, p)
	}
}
