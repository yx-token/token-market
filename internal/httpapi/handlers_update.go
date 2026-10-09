package httpapi

import (
	"errors"
	"net/http"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/update"
)

func (s *Server) systemVersion(w http.ResponseWriter, _ *http.Request) error {
	if s.Updater == nil {
		return httpx.Unavailable("UPDATE_DISABLED", "站内更新未配置")
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, s.Updater.Status())
}

func (s *Server) checkSystemUpdate(w http.ResponseWriter, r *http.Request) error {
	if s.Updater == nil {
		return httpx.Unavailable("UPDATE_DISABLED", "站内更新未配置")
	}
	check, err := s.Updater.Check(r.Context(), r.URL.Query().Get("force") == "true")
	if err != nil {
		return httpx.Unavailable("UPDATE_CHECK_FAILED", err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, check)
}

func (s *Server) startSystemUpdate(w http.ResponseWriter, r *http.Request) error {
	if s.Updater == nil {
		return httpx.Unavailable("UPDATE_DISABLED", "站内更新未配置")
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if body.Version == "" {
		return httpx.BadRequest("INVALID_VERSION", "请先检查更新并选择目标版本")
	}
	job, err := s.Updater.Start(r.Context(), body.Version)
	if err != nil {
		switch {
		case errors.Is(err, update.ErrBusy), errors.Is(err, update.ErrStale), errors.Is(err, update.ErrNoUpdate):
			return httpx.Conflict("UPDATE_CONFLICT", err.Error())
		case errors.Is(err, update.ErrDisabled):
			return httpx.Unavailable("UPDATE_DISABLED", err.Error())
		default:
			s.Log.Error("start system update", "err", err)
			return httpx.Unavailable("UPDATE_START_FAILED", "无法创建更新任务，请检查发布附件和服务日志")
		}
	}
	s.audit(r, "system.update", job.Version, map[string]string{"job_id": job.ID, "version": job.Version})
	httpx.WriteJSON(w, http.StatusAccepted, job)
	return nil
}
