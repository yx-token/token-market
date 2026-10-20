package httpapi

import (
	"fmt"
	"net/http"

	"github.com/yx-token/idlicat/market/internal/listing"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
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

func (s *Server) updateSellerProxy(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.CreateProxyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Proxies.UpdateSeller(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	s.audit(r, "seller_proxy.update", fmt.Sprintf("proxy:%d", p.ID), map[string]any{"host": p.Host, "port": p.Port})
	return ok(w, p)
}

func (s *Server) deleteSellerProxy(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Proxies.DeleteSeller(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	s.audit(r, "seller_proxy.delete", fmt.Sprintf("proxy:%d", id), nil)
	return ok(w, map[string]bool{"ok": true})
}
