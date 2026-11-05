package httpapi

import (
	"net/http"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/rewardtask"
)

// ---- loxo 任务中心对接（docs/2026-11-06-loxo-reward-tasks.md） ----

// rewardLanding 落地时查票据的任务类型；始终 200，查不成时 valid 为 null，前端照常保留票据。
func (s *Server) rewardLanding(w http.ResponseWriter, r *http.Request) error {
	out := rewardtask.LandingResult{}
	ip := httpx.ClientIP(r)
	allowed := s.Reward != nil && !s.rewardLandingByIP.Blocked(ip) && (s.rewardLandingAll == nil || !s.rewardLandingAll.Blocked("*"))
	if allowed {
		s.rewardLandingByIP.Fail(ip)
		if s.rewardLandingAll != nil {
			s.rewardLandingAll.Fail("*")
		}
		var in struct {
			Ticket string `json:"ticket"`
		}
		if httpx.Decode(r, &in) == nil {
			out = s.Reward.Landing(r.Context(), in.Ticket)
		}
	}
	return ok(w, out)
}

// bindRewardTicket 已有账号登录后（或已登录时到达）绑定票据；这类用户不算新注册。
func (s *Server) bindRewardTicket(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Ticket string `json:"ticket"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	accepted := false
	if s.Reward != nil {
		var err error
		if accepted, err = s.Reward.Capture(r.Context(), httpx.MustUser(r).UserID, in.Ticket, false); err != nil {
			return err
		}
	}
	return ok(w, map[string]bool{"accepted": accepted})
}

func (s *Server) rewardConfig(w http.ResponseWriter, r *http.Request) error {
	out, err := s.Reward.View(r.Context())
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) saveRewardConfig(w http.ResponseWriter, r *http.Request) error {
	var in rewardtask.ConfigInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Reward.SaveConfig(r.Context(), in); err != nil {
		return err
	}
	return s.rewardConfig(w, r)
}

func (s *Server) rewardPing(w http.ResponseWriter, r *http.Request) error {
	out, err := s.Reward.Ping(r.Context())
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) rewardTickets(w http.ResponseWriter, r *http.Request) error {
	out, err := s.Reward.ListTickets(r.Context(), r.URL.Query().Get("state"),
		httpx.QueryInt(r, "page", 1, 1, 1_000_000), httpx.QueryInt(r, "page_size", 20, 1, 100))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) rewardEvents(w http.ResponseWriter, r *http.Request) error {
	out, err := s.Reward.ListEvents(r.Context(), r.URL.Query().Get("state"),
		httpx.QueryInt(r, "page", 1, 1, 1_000_000), httpx.QueryInt(r, "page_size", 20, 1, 100))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) retryRewardEvent(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	out, err := s.Reward.RetryEvent(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, out)
}
