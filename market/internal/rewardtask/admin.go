package rewardtask

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

var (
	ticketStates = []string{"pending", "active", "completed", "skipped", "invalid"}
	eventStates  = []string{"pending", "accepted", "completed", "ignored", "rejected", "failed"}
)

// Summary 是票据 / 事件按状态的计数，所有状态都会出现（没有为 0）。
type Summary struct {
	Tickets map[string]int64 `json:"tickets"`
	Events  map[string]int64 `json:"events"`
}

func countByState(ctx context.Context, s *Service, table string, states []string) (map[string]int64, error) {
	out := make(map[string]int64, len(states))
	for _, st := range states {
		out[st] = 0
	}
	// table 只来自本文件的常量，不接受外部输入。
	rows, err := s.DB.Query(ctx, `SELECT state, count(*) FROM `+table+` GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

func (s *Service) summary(ctx context.Context) (Summary, error) {
	tickets, err := countByState(ctx, s, "reward_tickets", ticketStates)
	if err != nil {
		return Summary{}, err
	}
	events, err := countByState(ctx, s, "reward_events", eventStates)
	if err != nil {
		return Summary{}, err
	}
	return Summary{Tickets: tickets, Events: events}, nil
}

type PingView struct {
	PartnerName      string `json:"partner_name"`
	AppKey           string `json:"app_key"`
	ServerTime       int64  `json:"server_time"`
	ClockSkewSeconds int64  `json:"clock_skew_seconds"`
}

// Ping 用已保存的凭证调 loxo ping；不要求已启用，方便启用前先自测。
func (s *Service) Ping(ctx context.Context) (*PingView, error) {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.AppKey == "" || cfg.Secret == "" {
		return nil, ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := s.client(cfg).Ping(ctx)
	if err != nil {
		return nil, httpx.NewError(502, "REWARD_PING_FAILED", errReason(err))
	}
	return &PingView{PartnerName: res.PartnerName, AppKey: res.AppKey, ServerTime: res.ServerTime,
		ClockSkewSeconds: res.ServerTime - s.now().Unix()}, nil
}

func validState(state string, allowed []string) bool {
	if state == "" {
		return true
	}
	for _, a := range allowed {
		if a == state {
			return true
		}
	}
	return false
}

type TicketRow struct {
	ID        int64      `json:"id"`
	Ticket    string     `json:"ticket"`
	UserID    int64      `json:"user_id"`
	UserEmail string     `json:"user_email"`
	NewUser   bool       `json:"new_user"`
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	TaskID    *int64     `json:"task_id"`
	TaskTitle string     `json:"task_title"`
	TaskType  string     `json:"task_type"`
	PeriodKey string     `json:"period_key"`
	ExpiresAt *time.Time `json:"expires_at"`
	Attempts  int        `json:"attempts"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

func (s *Service) ListTickets(ctx context.Context, state string, page, size int) (*Page[TicketRow], error) {
	if !validState(state, ticketStates) {
		return nil, httpx.BadRequest("INVALID_STATE", "invalid state")
	}
	out := &Page[TicketRow]{Items: []TicketRow{}}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM reward_tickets WHERE ($1 = '' OR state = $1)`, state).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT t.id, t.ticket, t.user_id, COALESCE(u.email, ''), t.new_user, t.state, t.reason,
			t.task_id, t.task_title, t.task_type, t.period_key, t.expires_at, t.attempts, t.created_at, t.updated_at
		FROM reward_tickets t LEFT JOIN users u ON u.id = t.user_id
		WHERE ($1 = '' OR t.state = $1) ORDER BY t.id DESC LIMIT $2 OFFSET $3`, state, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TicketRow, error) {
		var t TicketRow
		err := r.Scan(&t.ID, &t.Ticket, &t.UserID, &t.UserEmail, &t.NewUser, &t.State, &t.Reason,
			&t.TaskID, &t.TaskTitle, &t.TaskType, &t.PeriodKey, &t.ExpiresAt, &t.Attempts, &t.CreatedAt, &t.UpdatedAt)
		return t, err
	})
	if err != nil {
		return nil, err
	}
	out.Items = append(out.Items, items...)
	return out, nil
}

type EventRow struct {
	ID              int64      `json:"id"`
	TicketID        int64      `json:"ticket_id"`
	Ticket          string     `json:"ticket"`
	UserID          int64      `json:"user_id"`
	UserEmail       string     `json:"user_email"`
	TaskTitle       string     `json:"task_title"`
	EventID         string     `json:"event_id"`
	EventType       string     `json:"event_type"`
	RechargeOrderID *int64     `json:"recharge_order_id"`
	Amount          *string    `json:"amount"`
	State           string     `json:"state"`
	Reason          string     `json:"reason"`
	Rewarded        bool       `json:"rewarded"`
	Attempts        int        `json:"attempts"`
	NextAttemptAt   time.Time  `json:"next_attempt_at"`
	SentAt          *time.Time `json:"sent_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

const eventSelect = `SELECT e.id, e.ticket_id, t.ticket, t.user_id, COALESCE(u.email, ''), t.task_title, e.event_id, e.event_type,
		e.recharge_order_id, e.amount::text, e.state, e.reason, e.rewarded, e.attempts, e.next_attempt_at, e.sent_at, e.created_at
	FROM reward_events e JOIN reward_tickets t ON t.id = e.ticket_id LEFT JOIN users u ON u.id = t.user_id`

func scanEvent(r pgx.Row) (EventRow, error) {
	var e EventRow
	err := r.Scan(&e.ID, &e.TicketID, &e.Ticket, &e.UserID, &e.UserEmail, &e.TaskTitle, &e.EventID, &e.EventType,
		&e.RechargeOrderID, &e.Amount, &e.State, &e.Reason, &e.Rewarded, &e.Attempts, &e.NextAttemptAt, &e.SentAt, &e.CreatedAt)
	return e, err
}

func (s *Service) ListEvents(ctx context.Context, state string, page, size int) (*Page[EventRow], error) {
	if !validState(state, eventStates) {
		return nil, httpx.BadRequest("INVALID_STATE", "invalid state")
	}
	out := &Page[EventRow]{Items: []EventRow{}}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM reward_events WHERE ($1 = '' OR state = $1)`, state).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, eventSelect+` WHERE ($1 = '' OR e.state = $1) ORDER BY e.id DESC LIMIT $2 OFFSET $3`, state, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (EventRow, error) { return scanEvent(r) })
	if err != nil {
		return nil, err
	}
	out.Items = append(out.Items, items...)
	return out, nil
}

// RetryEvent 把被拒或重试用尽的事件重新放回队列，用同一个 event_id 再发一次。
func (s *Service) RetryEvent(ctx context.Context, id int64) (*EventRow, error) {
	tag, err := s.DB.Exec(ctx, `UPDATE reward_events SET state = 'pending', reason = '', attempts = 0, next_attempt_at = now(), updated_at = now()
		WHERE id = $1 AND state IN ('rejected', 'failed')`, id)
	if err != nil {
		return nil, err
	}
	e, err := scanEvent(s.DB.QueryRow(ctx, eventSelect+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrEventNotRetryable
	}
	return &e, nil
}
