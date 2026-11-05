package rewardtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

var ticketPattern = regexp.MustCompile(`^rt_[A-Za-z0-9_-]{8,61}$`)

var (
	ErrInvalidTicket     = httpx.BadRequest("INVALID_REWARD_TICKET", "invalid reward ticket")
	ErrEventNotFound     = httpx.NotFound("REWARD_EVENT_NOT_FOUND", "reward event not found")
	ErrEventNotRetryable = httpx.Conflict("REWARD_EVENT_NOT_RETRYABLE", "only rejected or failed events can be retried")
	ErrNotConfigured     = httpx.BadRequest("REWARD_NOT_CONFIGURED", "app_key and app_secret are not configured")
)

const (
	batchSize        = 20
	callTimeout      = 8 * time.Second
	landingTimeout   = 4 * time.Second
	maxSendAttempts  = 48
	retryLaterDelay  = 6 * time.Hour
	ticketGiveUpAge  = 30 * 24 * time.Hour
	originCacheTTL   = time.Minute
	maxReasonRunes   = 300
	reasonNotNewUser = "NOT_NEW_USER"
	reasonBrowseTask = "BROWSE_TASK"
)

// ValidTicket 票据格式：rt_ 开头，总长不超过 64（loxo 的上限）。
func ValidTicket(t string) bool { return ticketPattern.MatchString(t) }

type Service struct {
	DB      *pgxpool.Pool
	Keyring *crypto.Keyring
	HTTP    *http.Client
	Log     *slog.Logger
	Now     func() time.Time

	origin     atomic.Pointer[originCache]
	refreshing atomic.Bool
}

type originCache struct {
	origin string
	at     time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) client(cfg Config) *Client {
	return &Client{BaseURL: cfg.BaseURL, AppKey: cfg.AppKey, Secret: cfg.Secret, HTTP: s.HTTP, Now: s.Now}
}

func externalUserID(userID int64) string { return strconv.FormatInt(userID, 10) }

func truncate(s string) string {
	r := []rune(s)
	if len(r) > maxReasonRunes {
		return string(r[:maxReasonRunes])
	}
	return s
}

// errReason 是落库的失败原因：优先 loxo 的 reason，否则是错误摘要。
func errReason(err error) string {
	if r := reasonOf(err); r != "" {
		return r
	}
	return truncate(err.Error())
}

// backoff 第 n 次失败后的等待：30s 起翻倍，封顶 1 小时。
func backoff(n int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < n && d < time.Hour; i++ {
		d *= 2
	}
	return min(d, time.Hour)
}

// ---- CSP：浏览任务 SDK 的脚本来源 ----

func (s *Service) storeScriptOrigin(cfg Config) {
	origin := ""
	if cfg.Ready() && cfg.SDKEnabled {
		origin = cfg.BaseURL
	}
	s.origin.Store(&originCache{origin: origin, at: s.now()})
}

// ScriptOrigins 返回需要加入 CSP script-src 的来源。读缓存不阻塞；缓存过期时后台刷新。
func (s *Service) ScriptOrigins() []string {
	c := s.origin.Load()
	if c == nil || s.now().Sub(c.at) > originCacheTTL {
		s.refreshOrigin()
	}
	if c == nil || c.origin == "" {
		return nil
	}
	return []string{c.origin}
}

func (s *Service) refreshOrigin() {
	if s.DB == nil || !s.refreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.refreshing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		st, _, err := s.loadStored(ctx)
		if err != nil {
			s.log().Warn("reward task: refresh sdk origin failed", "err", err)
			return
		}
		s.storeScriptOrigin(Config{Enabled: st.Enabled, BaseURL: st.BaseURL, AppKey: st.AppKey, Secret: secretMarker(st.SecretEnc), SDKEnabled: st.SDKEnabled})
	}()
}

// ---- C 端：落地与绑定 ----

type SDKInfo struct {
	ScriptURL string `json:"script_url"`
	AppKey    string `json:"app_key"`
}

// LandingResult 告诉前端如何处理落地票据。Valid 为 nil 表示没查成，前端应保留票据等待绑定。
type LandingResult struct {
	Valid    *bool    `json:"valid"`
	TaskType string   `json:"task_type"`
	SDK      *SDKInfo `json:"sdk"`
}

func boolPtr(v bool) *bool { return &v }

// Landing 用 tickets/query 判断落地票据的任务类型；任何失败都只返回「未知」。
func (s *Service) Landing(ctx context.Context, ticket string) LandingResult {
	if !ValidTicket(ticket) {
		return LandingResult{Valid: boolPtr(false)}
	}
	cfg, err := s.LoadConfig(ctx)
	if err != nil || !cfg.Ready() {
		return LandingResult{}
	}
	ctx, cancel := context.WithTimeout(ctx, landingTimeout)
	defer cancel()
	view, err := s.client(cfg).QueryTicket(ctx, ticket)
	if err != nil {
		if ticketReason(reasonOf(err)) {
			return LandingResult{Valid: boolPtr(false)}
		}
		s.log().Warn("reward task: landing ticket query failed", "err", err)
		return LandingResult{}
	}
	if view.Expired || view.Status == "completed" {
		return LandingResult{Valid: boolPtr(false), TaskType: view.TaskType}
	}
	out := LandingResult{Valid: boolPtr(true), TaskType: view.TaskType}
	if view.TaskType == "browse" && cfg.SDKEnabled {
		out.SDK = &SDKInfo{ScriptURL: cfg.SDKScriptURL(), AppKey: cfg.AppKey}
	}
	return out
}

// Capture 把票据绑定到闲猫用户。newUser 表示用户是带着这张票据新注册的（只有这样才上报注册事件）。
// 票据已被别的用户绑定、或对接未启用时返回 false。
func (s *Service) Capture(ctx context.Context, userID int64, ticket string, newUser bool) (bool, error) {
	if !ValidTicket(ticket) {
		return false, ErrInvalidTicket
	}
	st, _, err := s.loadStored(ctx)
	if err != nil {
		return false, err
	}
	if !st.Enabled {
		return false, nil
	}
	var owner int64
	err = s.DB.QueryRow(ctx, `INSERT INTO reward_tickets (ticket, user_id, new_user) VALUES ($1, $2, $3)
		ON CONFLICT (ticket) DO UPDATE SET new_user = reward_tickets.new_user OR EXCLUDED.new_user, updated_at = now()
			WHERE reward_tickets.user_id = EXCLUDED.user_id AND reward_tickets.state = 'pending'
		RETURNING user_id`, ticket, userID, newUser).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.DB.QueryRow(ctx, `SELECT user_id FROM reward_tickets WHERE ticket = $1`, ticket).Scan(&owner)
	}
	if err != nil {
		return false, err
	}
	return owner == userID, nil
}

// ---- worker ----

// Sync 由 worker 周期调用：查询新绑定的票据、把到账充值入队、发送回调。
func (s *Service) Sync(ctx context.Context) error {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return err
	}
	s.storeScriptOrigin(cfg)
	if !cfg.Ready() {
		return nil
	}
	c := s.client(cfg)
	if err := s.checkTickets(ctx, c); err != nil {
		return fmt.Errorf("check tickets: %w", err)
	}
	if err := s.enqueueRecharges(ctx); err != nil {
		return fmt.Errorf("enqueue recharges: %w", err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE reward_tickets SET state = 'invalid', reason = 'REWARD_TICKET_EXPIRED', updated_at = now()
		WHERE state = 'active' AND expires_at IS NOT NULL AND expires_at <= $1`, s.now()); err != nil {
		return fmt.Errorf("expire tickets: %w", err)
	}
	if err := s.sendEvents(ctx, c); err != nil {
		return fmt.Errorf("send events: %w", err)
	}
	return nil
}

// ticketReason 是「票据本身不可用」的拒绝原因：票据记录应置为 invalid。
func ticketReason(reason string) bool {
	switch reason {
	case "REWARD_TICKET_NOT_FOUND", "REWARD_TICKET_EXPIRED", "REWARD_TICKET_PARTNER_MISMATCH",
		"REWARD_EXTERNAL_USER_USED", "REWARD_EXTERNAL_USER_MISMATCH", "REWARD_EVENT_TYPE_MISMATCH":
		return true
	}
	return false
}

type pendingTicket struct {
	id, userID int64
	ticket     string
	newUser    bool
	attempts   int
	createdAt  time.Time
}

func (s *Service) checkTickets(ctx context.Context, c *Client) error {
	rows, err := s.DB.Query(ctx, `SELECT id, user_id, ticket, new_user, attempts, created_at FROM reward_tickets
		WHERE state = 'pending' AND next_check_at <= $1 ORDER BY next_check_at, id LIMIT $2`, s.now(), batchSize)
	if err != nil {
		return err
	}
	tickets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pendingTicket, error) {
		var t pendingTicket
		err := r.Scan(&t.id, &t.userID, &t.ticket, &t.newUser, &t.attempts, &t.createdAt)
		return t, err
	})
	if err != nil {
		return err
	}
	for _, t := range tickets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		view, qerr := c.QueryTicket(callCtx, t.ticket)
		cancel()
		if qerr != nil {
			err = s.ticketQueryFailed(ctx, t, qerr)
		} else {
			err = s.applyTicket(ctx, t, view)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ticketQueryFailed(ctx context.Context, t pendingTicket, qerr error) error {
	reason := errReason(qerr)
	if ticketReason(reasonOf(qerr)) || reasonOf(qerr) == "REWARD_TASK_UNAVAILABLE" || s.now().Sub(t.createdAt) > ticketGiveUpAge {
		_, err := s.DB.Exec(ctx, `UPDATE reward_tickets SET state = 'invalid', reason = $2, attempts = attempts + 1, updated_at = now()
			WHERE id = $1 AND state = 'pending'`, t.id, reason)
		return err
	}
	s.log().Warn("reward task: ticket query failed, will retry", "ticket_id", t.id, "err", qerr)
	_, err := s.DB.Exec(ctx, `UPDATE reward_tickets SET reason = $2, attempts = attempts + 1, next_check_at = $3, updated_at = now()
		WHERE id = $1 AND state = 'pending'`, t.id, reason, s.now().Add(backoff(t.attempts+1)))
	return err
}

func (s *Service) applyTicket(ctx context.Context, t pendingTicket, v *TicketView) error {
	state, reason := "active", ""
	switch {
	case v.ExternalUserID != "" && v.ExternalUserID != externalUserID(t.userID):
		state, reason = "invalid", "REWARD_EXTERNAL_USER_MISMATCH"
	case v.Status == "completed":
		state = "completed"
	case v.Expired:
		state, reason = "invalid", "REWARD_TICKET_EXPIRED"
	case v.TaskType == "register" && !t.newUser:
		state, reason = "skipped", reasonNotNewUser
	case v.TaskType == "register", v.TaskType == "recharge":
	case v.TaskType == "browse":
		state, reason = "skipped", reasonBrowseTask
	default:
		state, reason = "skipped", "UNSUPPORTED_TASK_TYPE:"+truncate(v.TaskType)
	}
	var expires *time.Time
	if !v.ExpiresAt.IsZero() {
		expires = &v.ExpiresAt
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE reward_tickets SET state = $2, reason = $3, task_id = $4, task_title = $5, task_type = $6,
			period_key = $7, expires_at = $8, attempts = attempts + 1, updated_at = now()
		WHERE id = $1 AND state = 'pending'`, t.id, state, reason, v.TaskID, truncate(v.TaskTitle), truncate(v.TaskType), truncate(v.PeriodKey), expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 && state == "active" && v.TaskType == "register" {
		if _, err := tx.Exec(ctx, `INSERT INTO reward_events (ticket_id, event_id, event_type) VALUES ($1, $2, 'register')
			ON CONFLICT DO NOTHING`, t.id, fmt.Sprintf("tm-reg-%d", t.id)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// enqueueRecharges 把绑定票据之后下单、票据过期之前支付且已到账的充值单入队（金额扣除已退部分）。
// 起点用订单创建时间而不是 paid_at：paid_at 取自支付渠道（只精确到秒，且是对方的时钟），
// 绑定后立刻下单付款时可能比绑定时间还早，会把本该计入的充值漏掉。
func (s *Service) enqueueRecharges(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO reward_events (ticket_id, event_id, event_type, recharge_order_id, amount)
		SELECT t.id, 'tm-rc-' || o.id || '-' || t.id, 'recharge', o.id, o.amount - o.refunded_amount
		FROM reward_tickets t
		JOIN recharge_orders o ON o.user_id = t.user_id
		WHERE t.state = 'active' AND t.task_type = 'recharge'
			AND o.status = 'credited' AND o.paid_at IS NOT NULL AND o.created_at >= t.created_at
			AND (t.expires_at IS NULL OR o.paid_at < t.expires_at)
			AND o.amount - o.refunded_amount > 0
			AND NOT EXISTS (SELECT 1 FROM reward_events e WHERE e.ticket_id = t.id AND e.recharge_order_id = o.id)
		ON CONFLICT DO NOTHING`)
	return err
}

type pendingEvent struct {
	id, ticketID, userID int64
	eventID, eventType   string
	ticket               string
	amount               *string
	orderID              *int64
	attempts             int
	expiresAt            *time.Time
	occurredAt           time.Time
}

func (s *Service) sendEvents(ctx context.Context, c *Client) error {
	rows, err := s.DB.Query(ctx, `SELECT e.id, e.ticket_id, t.user_id, e.event_id, e.event_type, t.ticket, e.amount::text,
			e.recharge_order_id, e.attempts, t.expires_at, COALESCE(o.paid_at, e.created_at)
		FROM reward_events e
		JOIN reward_tickets t ON t.id = e.ticket_id
		LEFT JOIN recharge_orders o ON o.id = e.recharge_order_id
		WHERE e.state = 'pending' AND e.next_attempt_at <= $1
		ORDER BY e.next_attempt_at, e.id LIMIT $2`, s.now(), batchSize)
	if err != nil {
		return err
	}
	events, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pendingEvent, error) {
		var e pendingEvent
		err := r.Scan(&e.id, &e.ticketID, &e.userID, &e.eventID, &e.eventType, &e.ticket, &e.amount,
			&e.orderID, &e.attempts, &e.expiresAt, &e.occurredAt)
		return e, err
	})
	if err != nil {
		return err
	}
	for _, e := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		in := EventInput{EventID: e.eventID, EventType: e.eventType, Ticket: e.ticket,
			ExternalUserID: externalUserID(e.userID), OccurredAt: e.occurredAt.Unix()}
		if e.amount != nil {
			in.Amount = *e.amount
		}
		if e.orderID != nil {
			in.OrderID = strconv.FormatInt(*e.orderID, 10)
		}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		res, raw, serr := c.SendEvent(callCtx, in)
		cancel()
		if serr != nil {
			err = s.eventFailed(ctx, e, serr)
		} else {
			err = s.eventSent(ctx, e, res, raw)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) eventSent(ctx context.Context, e pendingEvent, res *EventResult, raw json.RawMessage) error {
	state := res.Result
	if state != "completed" && state != "ignored" {
		state = "accepted"
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE reward_events SET state = $2, reason = '', rewarded = $3, response = $4, sent_at = now(),
			attempts = attempts + 1, updated_at = now() WHERE id = $1 AND state = 'pending'`,
		e.id, state, res.Rewarded, []byte(raw)); err != nil {
		return err
	}
	if res.Status == "completed" || state == "completed" {
		if _, err := tx.Exec(ctx, `UPDATE reward_tickets SET state = 'completed', reason = '', updated_at = now()
			WHERE id = $1 AND state <> 'completed'`, e.ticketID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type outcome int

const (
	retrySoon  outcome = iota // 网络 / 5xx / 凭证或开关问题：退避重试
	retryLater                // 名额满 / 任务暂不可用：每 6 小时试一次，直到票据过期
	alreadyDone
	rejected
)

func classify(err error) outcome {
	var ae *APIError
	if !errors.As(err, &ae) {
		return retrySoon
	}
	switch ae.Reason {
	case "REWARD_TASK_ALREADY_COMPLETED":
		return alreadyDone
	case "REWARD_TASK_QUOTA_EXHAUSTED", "REWARD_TASK_UNAVAILABLE":
		return retryLater
	case "", "REWARD_SIGNATURE_MISSING", "REWARD_SIGNATURE_INVALID", "REWARD_TIMESTAMP_EXPIRED",
		"REWARD_NONCE_REPLAYED", "REWARD_PARTNER_INVALID", "REWARD_TASKS_DISABLED":
		return retrySoon
	}
	if ae.Status >= 500 || ae.Status == http.StatusTooManyRequests {
		return retrySoon
	}
	return rejected
}

func (s *Service) eventFailed(ctx context.Context, e pendingEvent, serr error) error {
	reason, now := errReason(serr), s.now()
	attempts := e.attempts + 1
	state, next := "pending", now
	ticketState := ""
	switch classify(serr) {
	case retrySoon:
		next = now.Add(backoff(attempts))
		if attempts >= maxSendAttempts {
			state = "failed"
		}
		s.log().Warn("reward task: event send failed, will retry", "event_id", e.eventID, "attempts", attempts, "err", serr)
	case retryLater:
		next = now.Add(retryLaterDelay)
		if e.expiresAt != nil && !now.Before(*e.expiresAt) {
			state = "rejected"
		}
	case alreadyDone:
		state, ticketState = "ignored", "completed"
	case rejected:
		state = "rejected"
		if ticketReason(reasonOf(serr)) {
			ticketState = "invalid"
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE reward_events SET state = $2, reason = $3, attempts = $4, next_attempt_at = $5, updated_at = now()
		WHERE id = $1 AND state = 'pending'`, e.id, state, reason, attempts, next); err != nil {
		return err
	}
	if ticketState != "" {
		if _, err := tx.Exec(ctx, `UPDATE reward_tickets SET state = $2, reason = $3, updated_at = now()
			WHERE id = $1 AND state IN ('pending', 'active')`, e.ticketID, ticketState, reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
