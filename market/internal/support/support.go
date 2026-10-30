// Package support 用户与官方的一对一轻量客服会话（轮询拉取，非 WebSocket）。
package support

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	StatusOpen   = "open"
	StatusClosed = "closed"

	SenderUser   = "user"
	SenderStaff  = "staff"
	SenderSystem = "system"

	maxBodyRunes = 2000

	// ReplyTimeout 用户末条消息后等待官方回复的告警阈值。
	ReplyTimeout = 15 * time.Minute
	// IdleCloseTimeout 双方均无新消息后自动结束会话。
	IdleCloseTimeout = 10 * time.Minute
	// PendingAlertCount 待回复会话数达到该值时触发积压告警。
	PendingAlertCount = 5
	// TypingTTL 输入中心跳有效期；超过则视为已停止输入。
	TypingTTL = 5 * time.Second

	welcomeBody     = "你好，这里是闲猫官方。直接说问题就行，我们看到会回。"
	closeNoteUser   = "你已结束本次会话。再发一条消息即可重新联系官方。"
	closeNoteStaff  = "官方已结束本次会话。再发一条消息即可重新打开。"
	closeNoteIdle   = "超过 10 分钟没有新消息，会话已自动结束。再发一条即可重新联系官方。"
	reopenNote      = "会话已重新打开，请继续描述问题。"

	convCols = `c.id, c.user_id, COALESCE(u.email, ''), c.status, c.last_message_at, c.last_preview, c.last_sender,
			c.user_unread_count, c.staff_unread_count, c.awaiting_staff_since,
			c.user_last_read_msg_id, c.staff_last_read_msg_id, c.user_typing_at, c.staff_typing_at,
			c.created_at, c.updated_at`
)

type Conversation struct {
	ID                 int64      `json:"id"`
	UserID             int64      `json:"user_id"`
	UserEmail          string     `json:"user_email,omitempty"`
	Status             string     `json:"status"`
	LastMessageAt      time.Time  `json:"last_message_at"`
	LastPreview        string     `json:"last_preview"`
	LastSender         string     `json:"last_sender"`
	UserUnreadCount    int        `json:"user_unread_count"`
	StaffUnreadCount   int        `json:"staff_unread_count"`
	AwaitingStaffSince *time.Time `json:"awaiting_staff_since,omitempty"`
	UserLastReadMsgID  int64      `json:"user_last_read_msg_id"`
	StaffLastReadMsgID int64      `json:"staff_last_read_msg_id"`
	UserTyping         bool       `json:"user_typing,omitempty"`
	StaffTyping        bool       `json:"staff_typing,omitempty"`
	PeerTyping         bool       `json:"peer_typing,omitempty"`
	Overdue            bool       `json:"overdue,omitempty"`
	WaitingMinutes     int        `json:"waiting_minutes,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	userTypingAt  *time.Time
	staffTypingAt *time.Time
}

type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Sender         string    `json:"sender"`
	SenderUserID   *int64    `json:"sender_user_id,omitempty"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}

type ConversationBundle struct {
	Conversation Conversation `json:"conversation"`
	Messages     []Message    `json:"messages"`
}

type Alerts struct {
	PendingCount         int            `json:"pending_count"`
	OverdueCount         int            `json:"overdue_count"`
	StaffUnreadTotal     int            `json:"staff_unread_total"`
	PendingAlertCount    int            `json:"pending_alert_threshold"`
	ReplyTimeoutMin      int            `json:"reply_timeout_minutes"`
	PendingAlertFired    bool           `json:"pending_alert_fired"`
	OverdueConversations []Conversation `json:"overdue_conversations"`
}

type Service struct {
	DB *pgxpool.Pool
}

func (s *Service) GetOrCreateMine(ctx context.Context, userID int64) (*ConversationBundle, error) {
	if userID <= 0 {
		return nil, httpx.BadRequest("INVALID_USER", "login required")
	}
	conv, err := s.getByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		conv, err = s.createWithWelcome(ctx, userID)
	}
	if err != nil {
		return nil, err
	}
	msgs, err := s.listMessages(ctx, conv.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	annotateViewer(conv, SenderUser)
	return &ConversationBundle{Conversation: *conv, Messages: msgs}, nil
}

func (s *Service) PostUserMessage(ctx context.Context, userID int64, body string) (*Message, error) {
	body, err := normalizeBody(body)
	if err != nil {
		return nil, err
	}
	bundle, err := s.GetOrCreateMine(ctx, userID)
	if err != nil {
		return nil, err
	}
	conv := bundle.Conversation
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	reopened := conv.Status == StatusClosed
	if reopened {
		_, err = tx.Exec(ctx, `
			INSERT INTO support_messages (conversation_id, sender, body)
			VALUES ($1, $2, $3)`, conv.ID, SenderSystem, reopenNote)
		if err != nil {
			return nil, err
		}
	}

	var msg Message
	err = tx.QueryRow(ctx, `
		INSERT INTO support_messages (conversation_id, sender, sender_user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, conversation_id, sender, sender_user_id, body, created_at`,
		conv.ID, SenderUser, userID, body,
	).Scan(&msg.ID, &msg.ConversationID, &msg.Sender, &msg.SenderUserID, &msg.Body, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}
	preview := clipPreview(body)
	_, err = tx.Exec(ctx, `
		UPDATE support_conversations SET
			status = 'open',
			last_message_at = $2,
			last_preview = $3,
			last_sender = $4,
			staff_unread_count = staff_unread_count + 1,
			user_last_read_msg_id = GREATEST(user_last_read_msg_id, $5),
			awaiting_staff_since = COALESCE(awaiting_staff_since, $2),
			user_typing_at = NULL,
			updated_at = now()
		WHERE id = $1`,
		conv.ID, msg.CreatedAt, preview, SenderUser, msg.ID,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	Notify(conv.ID)
	return &msg, nil
}

// SyncMine 用户侧增量同步：新消息 + 会话水位/输入中（无新消息也会返回 conversation）。
func (s *Service) SyncMine(ctx context.Context, userID, afterID int64) (*ConversationBundle, error) {
	conv, err := s.getByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ConversationBundle{Messages: []Message{}}, nil
	}
	if err != nil {
		return nil, err
	}
	msgs, err := s.listMessages(ctx, conv.ID, afterID, 200)
	if err != nil {
		return nil, err
	}
	annotateViewer(conv, SenderUser)
	return &ConversationBundle{Conversation: *conv, Messages: msgs}, nil
}

func (s *Service) MarkUserRead(ctx context.Context, userID int64) error {
	var id int64
	err := s.DB.QueryRow(ctx, `
		UPDATE support_conversations SET
			user_unread_count = 0,
			user_last_read_msg_id = GREATEST(
				user_last_read_msg_id,
				COALESCE((SELECT max(id) FROM support_messages WHERE conversation_id = support_conversations.id), 0)
			),
			updated_at = now()
		WHERE user_id = $1
		RETURNING id`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil && id > 0 {
		Notify(id)
	}
	return err
}

func (s *Service) SetUserTyping(ctx context.Context, userID int64, typing bool) error {
	if userID <= 0 {
		return httpx.BadRequest("INVALID_USER", "login required")
	}
	if _, err := s.getByUser(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
		if _, err2 := s.createWithWelcome(ctx, userID); err2 != nil {
			return err2
		}
	} else if err != nil {
		return err
	}
	var id int64
	var err error
	if typing {
		err = s.DB.QueryRow(ctx, `
			UPDATE support_conversations SET user_typing_at = now(), updated_at = now()
			WHERE user_id = $1 RETURNING id`, userID).Scan(&id)
	} else {
		err = s.DB.QueryRow(ctx, `
			UPDATE support_conversations SET user_typing_at = NULL, updated_at = now()
			WHERE user_id = $1 RETURNING id`, userID).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil && id > 0 {
		Notify(id)
	}
	return err
}

func (s *Service) AdminList(ctx context.Context, status, q string, limit, offset int) ([]Conversation, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := []any{}
	where := []string{"1=1"}
	if status == StatusOpen || status == StatusClosed {
		args = append(args, status)
		where = append(where, "c.status = $"+itoa(len(args)))
	} else if status == "pending" {
		where = append(where, "c.status = 'open' AND c.awaiting_staff_since IS NOT NULL")
	} else if status == "overdue" {
		args = append(args, time.Now().Add(-ReplyTimeout))
		where = append(where, "c.status = 'open' AND c.awaiting_staff_since IS NOT NULL AND c.awaiting_staff_since <= $"+itoa(len(args)))
	}
	q = strings.TrimSpace(q)
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, "(u.email ILIKE $"+itoa(len(args))+" OR c.last_preview ILIKE $"+itoa(len(args))+")")
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	limitPH, offsetPH := itoa(len(args)-1), itoa(len(args))
	rows, err := s.DB.Query(ctx, `
		SELECT `+convCols+`
		FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE `+whereSQL+`
		ORDER BY
			CASE WHEN c.awaiting_staff_since IS NOT NULL THEN 0 ELSE 1 END,
			c.awaiting_staff_since ASC NULLS LAST,
			c.last_message_at DESC
		LIMIT $`+limitPH+` OFFSET $`+offsetPH, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Conversation, 0)
	now := time.Now()
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, 0, err
		}
		annotatePresence(&c, now)
		annotateViewer(&c, SenderStaff)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (s *Service) AdminGet(ctx context.Context, id, afterID int64) (*ConversationBundle, error) {
	conv, err := s.getByID(ctx, id)
	if err != nil {
		return nil, err
	}
	msgs, err := s.listMessages(ctx, id, afterID, 500)
	if err != nil {
		return nil, err
	}
	annotateViewer(conv, SenderStaff)
	return &ConversationBundle{Conversation: *conv, Messages: msgs}, nil
}

func (s *Service) PostStaffMessage(ctx context.Context, staffUserID, conversationID int64, body string) (*Message, error) {
	if staffUserID <= 0 {
		return nil, httpx.BadRequest("INVALID_USER", "login required")
	}
	body, err := normalizeBody(body)
	if err != nil {
		return nil, err
	}
	if _, err := s.getByID(ctx, conversationID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var msg Message
	err = tx.QueryRow(ctx, `
		INSERT INTO support_messages (conversation_id, sender, sender_user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, conversation_id, sender, sender_user_id, body, created_at`,
		conversationID, SenderStaff, staffUserID, body,
	).Scan(&msg.ID, &msg.ConversationID, &msg.Sender, &msg.SenderUserID, &msg.Body, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}
	preview := clipPreview(body)
	_, err = tx.Exec(ctx, `
		UPDATE support_conversations SET
			status = 'open',
			last_message_at = $2,
			last_preview = $3,
			last_sender = $4,
			user_unread_count = user_unread_count + 1,
			staff_unread_count = 0,
			staff_last_read_msg_id = GREATEST(staff_last_read_msg_id, $5),
			awaiting_staff_since = NULL,
			staff_typing_at = NULL,
			updated_at = now()
		WHERE id = $1`,
		conversationID, msg.CreatedAt, preview, SenderStaff, msg.ID,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	Notify(conversationID)
	return &msg, nil
}

func (s *Service) Close(ctx context.Context, conversationID int64) error {
	return s.closeWithNote(ctx, conversationID, closeNoteStaff)
}

// CloseMine 用户主动结束自己的会话。
func (s *Service) CloseMine(ctx context.Context, userID int64) error {
	conv, err := s.getByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if conv.Status == StatusClosed {
		return nil
	}
	return s.closeWithNote(ctx, conv.ID, closeNoteUser)
}

// CloseIdle 结束超时无新消息的会话；供 worker 周期调用。
func (s *Service) CloseIdle(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-IdleCloseTimeout)
	rows, err := s.DB.Query(ctx, `
		SELECT id FROM support_conversations
		WHERE status = 'open' AND last_message_at <= $1
		ORDER BY id ASC
		LIMIT 100`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if err := s.closeWithNote(ctx, id, closeNoteIdle); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) closeWithNote(ctx context.Context, conversationID int64, note string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM support_conversations WHERE id = $1 FOR UPDATE`, conversationID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("CONVERSATION_NOT_FOUND", "conversation not found")
	}
	if err != nil {
		return err
	}
	if status == StatusClosed {
		return nil
	}

	var msg Message
	err = tx.QueryRow(ctx, `
		INSERT INTO support_messages (conversation_id, sender, body)
		VALUES ($1, $2, $3)
		RETURNING id, conversation_id, sender, sender_user_id, body, created_at`,
		conversationID, SenderSystem, note,
	).Scan(&msg.ID, &msg.ConversationID, &msg.Sender, &msg.SenderUserID, &msg.Body, &msg.CreatedAt)
	if err != nil {
		return err
	}
	preview := clipPreview(note)
	_, err = tx.Exec(ctx, `
		UPDATE support_conversations SET
			status = 'closed',
			last_message_at = $2,
			last_preview = $3,
			last_sender = $4,
			awaiting_staff_since = NULL,
			staff_unread_count = 0,
			user_unread_count = user_unread_count + 1,
			user_typing_at = NULL,
			staff_typing_at = NULL,
			updated_at = now()
		WHERE id = $1`,
		conversationID, msg.CreatedAt, preview, SenderSystem,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	Notify(conversationID)
	return nil
}

func (s *Service) MarkStaffRead(ctx context.Context, conversationID int64) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE support_conversations SET
			staff_unread_count = 0,
			staff_last_read_msg_id = GREATEST(
				staff_last_read_msg_id,
				COALESCE((SELECT max(id) FROM support_messages WHERE conversation_id = $1), 0)
			),
			updated_at = now()
		WHERE id = $1`, conversationID)
	if err == nil {
		Notify(conversationID)
	}
	return err
}

func (s *Service) SetStaffTyping(ctx context.Context, conversationID int64, typing bool) error {
	if _, err := s.getByID(ctx, conversationID); err != nil {
		return err
	}
	var err error
	if typing {
		_, err = s.DB.Exec(ctx, `
			UPDATE support_conversations SET staff_typing_at = now(), updated_at = now()
			WHERE id = $1`, conversationID)
	} else {
		_, err = s.DB.Exec(ctx, `
			UPDATE support_conversations SET staff_typing_at = NULL, updated_at = now()
			WHERE id = $1`, conversationID)
	}
	if err == nil {
		Notify(conversationID)
	}
	return err
}

func (s *Service) Alerts(ctx context.Context) (*Alerts, error) {
	now := time.Now()
	cutoff := now.Add(-ReplyTimeout)
	var pending, overdue, unread int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM support_conversations
		WHERE status = 'open' AND awaiting_staff_since IS NOT NULL`).Scan(&pending); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM support_conversations
		WHERE status = 'open' AND awaiting_staff_since IS NOT NULL AND awaiting_staff_since <= $1`, cutoff).Scan(&overdue); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(staff_unread_count), 0) FROM support_conversations
		WHERE status = 'open'`).Scan(&unread); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `
		SELECT `+convCols+`
		FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.status = 'open' AND c.awaiting_staff_since IS NOT NULL AND c.awaiting_staff_since <= $1
		ORDER BY c.awaiting_staff_since ASC
		LIMIT 20`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Conversation, 0)
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		annotatePresence(&c, now)
		list = append(list, c)
	}
	return &Alerts{
		PendingCount:         pending,
		OverdueCount:         overdue,
		StaffUnreadTotal:     unread,
		PendingAlertCount:    PendingAlertCount,
		ReplyTimeoutMin:      int(ReplyTimeout / time.Minute),
		PendingAlertFired:    pending >= PendingAlertCount,
		OverdueConversations: list,
	}, rows.Err()
}

func (s *Service) createWithWelcome(ctx context.Context, userID int64) (*Conversation, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO support_conversations (user_id, status, last_preview, last_sender)
		VALUES ($1, 'open', $2, 'system')
		ON CONFLICT (user_id) DO UPDATE SET updated_at = support_conversations.updated_at
		RETURNING id`, userID, clipPreview(welcomeBody),
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM support_messages WHERE conversation_id = $1`, id).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		var createdAt time.Time
		var msgID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO support_messages (conversation_id, sender, body)
			VALUES ($1, 'system', $2)
			RETURNING id, created_at`, id, welcomeBody,
		).Scan(&msgID, &createdAt)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE support_conversations SET
				last_message_at = $2, last_preview = $3, last_sender = 'system',
				user_unread_count = 1, updated_at = now()
			WHERE id = $1`, id, createdAt, clipPreview(welcomeBody))
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getByID(ctx, id)
}

func (s *Service) getByUser(ctx context.Context, userID int64) (*Conversation, error) {
	row := s.DB.QueryRow(ctx, `
		SELECT `+convCols+`
		FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.user_id = $1`, userID)
	c, err := scanConv(row)
	if err != nil {
		return nil, err
	}
	annotatePresence(&c, time.Now())
	return &c, nil
}

func (s *Service) getByID(ctx context.Context, id int64) (*Conversation, error) {
	row := s.DB.QueryRow(ctx, `
		SELECT `+convCols+`
		FROM support_conversations c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.id = $1`, id)
	c, err := scanConv(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("CONVERSATION_NOT_FOUND", "conversation not found")
	}
	if err != nil {
		return nil, err
	}
	annotatePresence(&c, time.Now())
	return &c, nil
}

func (s *Service) listMessages(ctx context.Context, conversationID, afterID int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id, conversation_id, sender, sender_user_id, body, created_at
		FROM support_messages
		WHERE conversation_id = $1 AND id > $2
		ORDER BY id ASC
		LIMIT $3`, conversationID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Sender, &m.SenderUserID, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanConv(row scannable) (Conversation, error) {
	var c Conversation
	err := row.Scan(
		&c.ID, &c.UserID, &c.UserEmail, &c.Status, &c.LastMessageAt, &c.LastPreview, &c.LastSender,
		&c.UserUnreadCount, &c.StaffUnreadCount, &c.AwaitingStaffSince,
		&c.UserLastReadMsgID, &c.StaffLastReadMsgID, &c.userTypingAt, &c.staffTypingAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	return c, err
}

func annotatePresence(c *Conversation, now time.Time) {
	annotateOverdue(c, now)
	c.UserTyping = c.userTypingAt != nil && now.Sub(*c.userTypingAt) < TypingTTL
	c.StaffTyping = c.staffTypingAt != nil && now.Sub(*c.staffTypingAt) < TypingTTL
}

func annotateViewer(c *Conversation, viewer string) {
	switch viewer {
	case SenderUser:
		c.PeerTyping = c.StaffTyping
	case SenderStaff:
		c.PeerTyping = c.UserTyping
	}
}

func normalizeBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", httpx.BadRequest("EMPTY_BODY", "message required")
	}
	if utf8.RuneCountInString(body) > maxBodyRunes {
		return "", httpx.BadRequest("BODY_TOO_LONG", "message too long")
	}
	return body, nil
}

func clipPreview(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func annotateOverdue(c *Conversation, now time.Time) {
	if c.AwaitingStaffSince == nil || c.Status != StatusOpen {
		return
	}
	mins := int(now.Sub(*c.AwaitingStaffSince) / time.Minute)
	if mins < 0 {
		mins = 0
	}
	c.WaitingMinutes = mins
	c.Overdue = now.Sub(*c.AwaitingStaffSince) >= ReplyTimeout
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
