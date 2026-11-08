package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/yx-token/token-market/market/internal/mail"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/support"
)

func (s *Server) supportSvc() *support.Service {
	if s.Support != nil {
		return s.Support
	}
	return &support.Service{DB: s.DB}
}

func parseWaitMS(r *http.Request) time.Duration {
	ms, _ := strconv.Atoi(r.URL.Query().Get("wait_ms"))
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func parseSyncHint(r *http.Request) support.SyncHint {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
	peerRead, _ := strconv.ParseInt(r.URL.Query().Get("peer_read"), 10, 64)
	unread, _ := strconv.Atoi(r.URL.Query().Get("unread"))
	peerTyping := r.URL.Query().Get("peer_typing") == "1" || r.URL.Query().Get("peer_typing") == "true"
	return support.SyncHint{
		AfterID:    after,
		PeerTyping: peerTyping,
		PeerReadID: peerRead,
		Unread:     unread,
		Status:     r.URL.Query().Get("status"),
	}
}

func (s *Server) supportMine(w http.ResponseWriter, r *http.Request) error {
	out, err := s.supportSvc().GetOrCreateMine(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) supportPostMessage(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Body string `json:"body"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	msg, err := s.supportSvc().PostUserMessage(r.Context(), httpx.MustUser(r).UserID, in.Body)
	if err != nil {
		return err
	}
	return created(w, msg)
}

func (s *Server) supportPoll(w http.ResponseWriter, r *http.Request) error {
	hint := parseSyncHint(r)
	out, err := s.supportSvc().WaitSyncMine(r.Context(), httpx.MustUser(r).UserID, hint, parseWaitMS(r))
	if err != nil {
		// 客户端中止长轮询属正常，勿打 500
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	}
	return ok(w, out)
}

func (s *Server) supportMarkRead(w http.ResponseWriter, r *http.Request) error {
	if err := s.supportSvc().MarkUserRead(r.Context(), httpx.MustUser(r).UserID); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) supportTyping(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Typing bool `json:"typing"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.supportSvc().SetUserTyping(r.Context(), httpx.MustUser(r).UserID, in.Typing); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) supportClose(w http.ResponseWriter, r *http.Request) error {
	if err := s.supportSvc().CloseMine(r.Context(), httpx.MustUser(r).UserID); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) adminSupportList(w http.ResponseWriter, r *http.Request) error {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := s.supportSvc().AdminList(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"items": items, "total": total})
}

func (s *Server) adminSupportGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	hint := parseSyncHint(r)
	out, err := s.supportSvc().WaitAdminGet(r.Context(), id, hint, parseWaitMS(r))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	}
	return ok(w, out)
}

func (s *Server) adminSupportPostMessage(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	msg, err := s.supportSvc().PostStaffMessage(r.Context(), httpx.MustUser(r).UserID, id, in.Body)
	if err != nil {
		return err
	}
	return created(w, msg)
}

func (s *Server) adminSupportClose(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.supportSvc().Close(r.Context(), id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) adminSupportMarkRead(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.supportSvc().MarkStaffRead(r.Context(), id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) adminSupportTyping(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Typing bool `json:"typing"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.supportSvc().SetStaffTyping(r.Context(), id, in.Typing); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) adminSupportAlerts(w http.ResponseWriter, r *http.Request) error {
	pending, _ := strconv.Atoi(r.URL.Query().Get("pending"))
	overdue, _ := strconv.Atoi(r.URL.Query().Get("overdue"))
	unread, _ := strconv.Atoi(r.URL.Query().Get("unread"))
	out, err := s.supportSvc().WaitAlerts(r.Context(), pending, overdue, unread, parseWaitMS(r))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	}
	return ok(w, out)
}

func (s *Server) adminSupportAlertSettings(w http.ResponseWriter, r *http.Request) error {
	view, err := s.supportSvc().LoadAlertSettingsView(r.Context())
	if err != nil {
		return err
	}
	return ok(w, view)
}

func (s *Server) adminSupportUpdateAlertSettings(w http.ResponseWriter, r *http.Request) error {
	var in support.AlertConfig
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	cfg, err := s.supportSvc().SaveAlertConfig(r.Context(), in)
	if err != nil {
		return err
	}
	view, err := s.supportSvc().LoadAlertSettingsView(r.Context())
	if err != nil {
		return ok(w, cfg)
	}
	view.AlertConfig = cfg
	return ok(w, view)
}

func (s *Server) adminSupportTestAlertEmail(w http.ResponseWriter, r *http.Request) error {
	svc := s.supportSvc()
	cfg, err := svc.LoadAlertConfig(r.Context())
	if err != nil {
		return err
	}
	emails := cfg.NotifyEmails()
	if len(emails) == 0 {
		return httpx.BadRequest("NO_ALERT_EMAIL", "请先配置预警收件邮箱并启用邮件预警")
	}
	boot := mail.BootFromEnv(os.Getenv("TM_RESEND_API_KEY"), os.Getenv("TM_RESEND_FROM"))
	enabled, sender, err := mail.ResolveOpsSender(r.Context(), s.DB, nil, boot, s.Log)
	if err != nil {
		svc.RecordAlertDeliveryErr(r.Context(), err)
		return err
	}
	if !enabled || sender == nil {
		return httpx.BadRequest("MAIL_NOT_READY", "发信通道未就绪：请在系统设置配置可用的 SMTP，或设置 Resend/dev 发信")
	}
	subject := "【闲猫客服】预警邮件测试"
	body := `<div style="font-family:sans-serif;line-height:1.6"><p>这是一封客服预警测试邮件。若你收到它，说明发信通道可用。</p></div>`
	if err := sender.Send(r.Context(), emails[0], subject, body); err != nil {
		svc.RecordAlertDeliveryErr(r.Context(), err)
		return err
	}
	svc.RecordAlertDeliveryOK(r.Context())
	return ok(w, map[string]any{"status": "ok", "to": emails[0]})
}
