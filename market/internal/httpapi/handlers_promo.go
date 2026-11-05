package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/promo"
)

func (s *Server) promoVisit(w http.ResponseWriter, r *http.Request) error {
	accepted := false
	ip := httpx.ClientIP(r)
	if !s.promoVisitsByIP.Blocked(ip) {
		s.promoVisitsByIP.Fail(ip)
		var in promo.Visit
		if httpx.Decode(r, &in) == nil && s.Promo != nil {
			var err error
			accepted, err = s.Promo.RecordVisit(r.Context(), in, ip, r.UserAgent())
			if err != nil {
				s.promoLog().Warn("promotion visit failed", "err", err)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(map[string]bool{"accepted": accepted})
}

func (s *Server) promoLog() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Server) promoChannels(w http.ResponseWriter, r *http.Request) error {
	period, err := promo.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), false, time.Now())
	if err != nil {
		return err
	}
	out, err := s.Promo.List(r.Context(), period)
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) createPromoChannel(w http.ResponseWriter, r *http.Request) error {
	var in promo.Input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.Promo.Create(r.Context(), in, httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return created(w, out)
}

func (s *Server) updatePromoChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	// Code deliberately is not accepted by updates: issued links keep working.
	var in struct {
		Name        string `json:"name"`
		LandingPath string `json:"landing_path"`
		Note        string `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.Promo.Update(r.Context(), id, promo.Input{Name: in.Name, LandingPath: in.LandingPath, Note: in.Note})
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) promoChannelStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.Promo.SetStatus(r.Context(), id, in.Status)
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) deletePromoChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Promo.Delete(r.Context(), id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "deleted"})
}

func (s *Server) promoChannelDaily(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	period, err := promo.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), true, time.Now())
	if err != nil {
		return err
	}
	items, err := s.Promo.Daily(r.Context(), id, period)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"items": items})
}

func (s *Server) promoChannelUsers(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	pageSize := httpx.QueryInt(r, "page_size", 20, 1, 100)
	pageNum := httpx.QueryInt(r, "page", 1, 1, 1_000_000)
	out, err := s.Promo.Users(r.Context(), id, pageSize, (pageNum-1)*pageSize)
	if err != nil {
		return err
	}
	return ok(w, out)
}
