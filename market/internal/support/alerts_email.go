package support

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const backlogAlertKey = "backlog"

// MailSender 客服预警发信依赖（可注入假实现）。
type MailSender interface {
	Send(ctx context.Context, to, subject, html string) error
}

type overdueAlertRow struct {
	ID        int64
	UserEmail string
	Waiting   time.Duration
	Preview   string
}

// NotifyAlertEmails 扫描超时未回与积压阈值，向配置的预警邮箱发邮件（每个会话 / 每次积压越界只发一次）。
// sender 为 nil 或未配置收件人时跳过。返回发出的邮件封数。
func (s *Service) NotifyAlertEmails(ctx context.Context, sender MailSender, workbenchURL string, log *slog.Logger) (int, error) {
	if sender == nil {
		return 0, nil
	}
	if log == nil {
		log = slog.Default()
	}
	cfg, err := s.LoadAlertConfig(ctx)
	if err != nil {
		return 0, err
	}
	admins := cfg.NotifyEmails()
	if len(admins) == 0 {
		return 0, nil
	}
	sent := 0
	n, err := s.notifyOverdueEmails(ctx, sender, admins, cfg, workbenchURL, log)
	sent += n
	if err != nil {
		return sent, err
	}
	n, err = s.notifyBacklogEmail(ctx, sender, admins, cfg, workbenchURL, log)
	sent += n
	return sent, err
}

func (s *Service) notifyOverdueEmails(ctx context.Context, sender MailSender, admins []string, cfg AlertConfig, workbenchURL string, log *slog.Logger) (int, error) {
	cutoff := time.Now().Add(-cfg.ReplyTimeout())
	rows, err := s.DB.Query(ctx, `
		SELECT c.id, COALESCE(u.email, ''), c.awaiting_staff_since, c.last_preview
		FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.status = 'open'
		  AND c.awaiting_staff_since IS NOT NULL
		  AND c.awaiting_staff_since <= $1
		  AND c.overdue_alerted_at IS NULL
		ORDER BY c.awaiting_staff_since ASC
		LIMIT 50`, cutoff)
	if err != nil {
		if strings.Contains(err.Error(), "overdue_alerted_at") {
			return 0, nil
		}
		return 0, err
	}
	defer rows.Close()

	now := time.Now()
	items := make([]overdueAlertRow, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		var email string
		var since time.Time
		var preview string
		if err := rows.Scan(&id, &email, &since, &preview); err != nil {
			return 0, err
		}
		items = append(items, overdueAlertRow{
			ID: id, UserEmail: email, Waiting: now.Sub(since), Preview: preview,
		})
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	timeoutMin := cfg.ReplyTimeoutMinutes
	subject := fmt.Sprintf("【闲猫客服】%d 条会话超过 %d 分钟未回复", len(items), timeoutMin)
	body := buildOverdueAlertHTML(items, timeoutMin, workbenchURL)
	if err := sendToAdmins(ctx, sender, admins, subject, body); err != nil {
		return 0, err
	}
	_, err = s.DB.Exec(ctx, `
		UPDATE support_conversations
		SET overdue_alerted_at = now(), updated_at = now()
		WHERE id = ANY($1) AND overdue_alerted_at IS NULL`, ids)
	if err != nil {
		return 0, err
	}
	log.Info("support overdue alert emailed", "conversations", len(ids), "admins", len(admins))
	return 1, nil
}

func (s *Service) notifyBacklogEmail(ctx context.Context, sender MailSender, admins []string, cfg AlertConfig, workbenchURL string, log *slog.Logger) (int, error) {
	var pending int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM support_conversations
		WHERE status = 'open' AND awaiting_staff_since IS NOT NULL`).Scan(&pending); err != nil {
		return 0, err
	}

	var firedAt *time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT fired_at FROM support_alert_state WHERE key = $1`, backlogAlertKey).Scan(&firedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = s.DB.Exec(ctx, `
				INSERT INTO support_alert_state (key) VALUES ($1) ON CONFLICT (key) DO NOTHING`, backlogAlertKey)
			firedAt = nil
		} else if strings.Contains(err.Error(), "support_alert_state") {
			return 0, nil
		} else {
			return 0, err
		}
	}

	threshold := cfg.PendingThreshold()
	if pending < threshold {
		if firedAt != nil {
			_, _ = s.DB.Exec(ctx, `
				UPDATE support_alert_state SET fired_at = NULL, updated_at = now() WHERE key = $1`, backlogAlertKey)
		}
		return 0, nil
	}
	if firedAt != nil {
		return 0, nil
	}

	subject := fmt.Sprintf("【闲猫客服】待回复积压 %d 条（阈值 %d）", pending, threshold)
	body := buildBacklogAlertHTML(pending, threshold, workbenchURL)
	if err := sendToAdmins(ctx, sender, admins, subject, body); err != nil {
		return 0, err
	}
	_, err = s.DB.Exec(ctx, `
		UPDATE support_alert_state SET fired_at = now(), updated_at = now(),
			meta = jsonb_build_object('pending', $2::int)
		WHERE key = $1`, backlogAlertKey, pending)
	if err != nil {
		return 0, err
	}
	log.Info("support backlog alert emailed", "pending", pending, "admins", len(admins))
	return 1, nil
}

func sendToAdmins(ctx context.Context, sender MailSender, admins []string, subject, htmlBody string) error {
	var first error
	ok := 0
	for _, to := range admins {
		to = strings.TrimSpace(to)
		if to == "" {
			continue
		}
		if err := sender.Send(ctx, to, subject, htmlBody); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		ok++
	}
	if ok == 0 {
		if first != nil {
			return first
		}
		return fmt.Errorf("no alert email delivered")
	}
	return nil
}

func buildOverdueAlertHTML(items []overdueAlertRow, timeoutMin int, workbenchURL string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;line-height:1.6;color:#1f2937">`)
	fmt.Fprintf(&b, `<p>有 <strong>%d</strong> 条客服会话已超过 <strong>%d</strong> 分钟未回复，请尽快处理。</p>`,
		len(items), timeoutMin)
	if workbenchURL != "" {
		fmt.Fprintf(&b, `<p><a href="%s">打开官方客服工作台</a></p>`, html.EscapeString(workbenchURL))
	}
	b.WriteString(`<ul>`)
	for _, it := range items {
		who := it.UserEmail
		if who == "" {
			who = fmt.Sprintf("会话 #%d", it.ID)
		}
		mins := int(it.Waiting.Minutes())
		if mins < 1 {
			mins = 1
		}
		preview := strings.TrimSpace(it.Preview)
		if utf8Len(preview) > 80 {
			preview = string([]rune(preview)[:80]) + "…"
		}
		fmt.Fprintf(&b, `<li><strong>%s</strong>（#%d）已等待 %d 分钟`,
			html.EscapeString(who), it.ID, mins)
		if preview != "" {
			fmt.Fprintf(&b, `：%s`, html.EscapeString(preview))
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul><p style="color:#6b7280;font-size:12px">本邮件由闲猫客服预警自动发送；回复用户后不会重复提醒同一会话。</p></div>`)
	return b.String()
}

func buildBacklogAlertHTML(pending, threshold int, workbenchURL string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;line-height:1.6;color:#1f2937">`)
	fmt.Fprintf(&b, `<p>待回复会话已达 <strong>%d</strong> 条（积压阈值 <strong>%d</strong>）。</p>`,
		pending, threshold)
	if workbenchURL != "" {
		fmt.Fprintf(&b, `<p><a href="%s">打开官方客服工作台</a></p>`, html.EscapeString(workbenchURL))
	}
	b.WriteString(`<p style="color:#6b7280;font-size:12px">待回复回落到阈值以下后，再次积压会重新提醒。</p></div>`)
	return b.String()
}

func utf8Len(s string) int { return len([]rune(s)) }
