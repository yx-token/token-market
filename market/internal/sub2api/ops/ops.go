// Package ops 实现 sub2api 写操作的出站表（transactional outbox）。
//
// 业务代码在自己的数据库事务里调用 Enqueue 记录"要对 sub2api 做什么"，由 Sender 异步按序发送，
// 失败按退避重试，超过次数或遇到不可重试错误进入 dead 状态等待人工处理。
// 同一 target 的操作严格按入队顺序执行（例如先建账号、再改分组）。
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	StatusPending = "pending"
	StatusSending = "sending"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusDead    = "dead"

	PriorityUrgent int16 = 10  // 不调度等安全相关操作
	PriorityNormal int16 = 100 // 普通参数调整
)

// DB 是 pgxpool.Pool / pgx.Tx 的公共子集。
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type NewOp struct {
	Type           string
	Target         string // 如 account:123、user:45，同一 target 按序执行
	Payload        any
	IdempotencyKey string // 全局唯一；同时作为调用 sub2api 时的 Idempotency-Key
	Priority       int16
	MaxAttempts    int
	// NotBefore 非零时到该时间后才发送。延时操作会阻塞同一 target 的后续操作，应使用独立的 target。
	NotBefore time.Time
}

// Enqueue 记录一个待发送的操作。幂等键已存在时不重复插入，返回已有记录的 ID。
func Enqueue(ctx context.Context, db DB, op NewOp) (int64, error) {
	if op.Type == "" || op.Target == "" || op.IdempotencyKey == "" {
		return 0, errors.New("ops: type, target and idempotency key are required")
	}
	payload, err := json.Marshal(op.Payload)
	if err != nil {
		return 0, fmt.Errorf("ops: marshal payload: %w", err)
	}
	if op.Priority == 0 {
		op.Priority = PriorityNormal
	}
	if op.MaxAttempts <= 0 {
		op.MaxAttempts = 10
	}
	var notBefore *time.Time
	if !op.NotBefore.IsZero() {
		notBefore = &op.NotBefore
	}
	var id int64
	err = db.QueryRow(ctx, `
		INSERT INTO sub2api_ops (op_type, target, payload, idempotency_key, priority, max_attempts, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, now()))
		ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id`,
		op.Type, op.Target, payload, op.IdempotencyKey, op.Priority, op.MaxAttempts, notBefore).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ops: enqueue: %w", err)
	}
	return id, nil
}

type Op struct {
	ID             int64
	Type           string
	Target         string
	Payload        json.RawMessage
	IdempotencyKey string
	Attempts       int
	MaxAttempts    int
}

// Handler 执行一个操作。返回的错误由 Retryable 判断是否重试。
type Handler func(ctx context.Context, op Op) (result any, err error)

// Gate 在每批发送前调用；返回 false 时本轮不发送（例如兼容自检处于安全模式）。
type Gate func(ctx context.Context) (allowed bool, reason string)

type Sender struct {
	db        DB
	handlers  map[string]Handler
	retryable func(error) bool
	gate      Gate
	batch     int
	lease     time.Duration
	opTimeout time.Duration
	backoff   func(attempt int) time.Duration
	log       *slog.Logger
	// pausedReason 记录上一轮被 Gate 拦截的原因，只在状态变化时打日志，避免每秒刷屏。
	// RunOnce 不应并发调用（Run 是单协程循环）。
	pausedReason string
}

type SenderConfig struct {
	BatchSize int
	Lease     time.Duration // sending 状态的租约；进程崩溃后超过租约的操作会被重新领取
	OpTimeout time.Duration
	Retryable func(error) bool
	Gate      Gate
	Logger    *slog.Logger
}

func NewSender(db DB, cfg SenderConfig) *Sender {
	s := &Sender{
		db:        db,
		handlers:  map[string]Handler{},
		retryable: cfg.Retryable,
		gate:      cfg.Gate,
		batch:     cfg.BatchSize,
		lease:     cfg.Lease,
		opTimeout: cfg.OpTimeout,
		backoff:   DefaultBackoff,
		log:       cfg.Logger,
	}
	if s.batch <= 0 {
		s.batch = 20
	}
	if s.lease <= 0 {
		s.lease = 2 * time.Minute
	}
	if s.opTimeout <= 0 {
		s.opTimeout = 60 * time.Second
	}
	if s.retryable == nil {
		s.retryable = func(error) bool { return true }
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

func (s *Sender) Register(opType string, h Handler) {
	s.handlers[opType] = h
}

// DefaultBackoff：2^attempt 秒，上限 10 分钟，加 ±20% 抖动。
func DefaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Second << min(attempt, 10)
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	jitter := 0.8 + 0.4*rand.Float64() //nolint:gosec // 退避抖动不需要密码学随机
	return time.Duration(float64(d) * jitter)
}

// claim 领取一批可执行的操作。同一 target 只领取最早的未完成操作，保证顺序；
// dead 的操作不阻塞后续操作（由告警提示人工处理）。
const claimSQL = `
UPDATE sub2api_ops o
SET status = 'sending', attempts = o.attempts + 1, locked_until = now() + make_interval(secs => $2), updated_at = now()
WHERE o.id IN (
    SELECT c.id FROM sub2api_ops c
    WHERE ((c.status IN ('pending', 'failed') AND c.next_attempt_at <= now())
           OR (c.status = 'sending' AND c.locked_until < now()))
      AND NOT EXISTS (
          SELECT 1 FROM sub2api_ops p
          WHERE p.target = c.target AND p.id < c.id AND p.status IN ('pending', 'sending', 'failed'))
    ORDER BY c.priority, c.next_attempt_at, c.id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING o.id, o.op_type, o.target, o.payload, o.idempotency_key, o.attempts, o.max_attempts`

// RunOnce 领取并执行一批操作，返回执行的数量。
func (s *Sender) RunOnce(ctx context.Context) (int, error) {
	if s.gate != nil {
		ok, reason := s.gate(ctx)
		if !ok {
			if reason != s.pausedReason {
				s.log.Warn("sub2api ops paused", "reason", reason)
				s.pausedReason = reason
			}
			return 0, nil
		}
		if s.pausedReason != "" {
			s.log.Info("sub2api ops resumed")
			s.pausedReason = ""
		}
	}
	rows, err := s.db.Query(ctx, claimSQL, s.batch, s.lease.Seconds())
	if err != nil {
		return 0, fmt.Errorf("ops: claim: %w", err)
	}
	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Op, error) {
		var op Op
		err := row.Scan(&op.ID, &op.Type, &op.Target, &op.Payload, &op.IdempotencyKey, &op.Attempts, &op.MaxAttempts)
		return op, err
	})
	if err != nil {
		return 0, fmt.Errorf("ops: claim scan: %w", err)
	}
	for _, op := range claimed {
		s.execute(ctx, op)
	}
	return len(claimed), nil
}

func (s *Sender) execute(ctx context.Context, op Op) {
	h, ok := s.handlers[op.Type]
	if !ok {
		s.finish(ctx, op, StatusDead, nil, fmt.Errorf("no handler for op type %q", op.Type))
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, s.opTimeout)
	result, err := h(opCtx, op)
	cancel()
	switch {
	case err == nil:
		s.finish(ctx, op, StatusDone, result, nil)
	case !s.retryable(err) || op.Attempts >= op.MaxAttempts:
		s.finish(ctx, op, StatusDead, nil, err)
	default:
		s.finish(ctx, op, StatusFailed, nil, err)
	}
}

func (s *Sender) finish(ctx context.Context, op Op, status string, result any, opErr error) {
	// 进程收到退出信号时也要把结果写回，否则操作会一直停在 sending 直到租约过期
	ctx = context.WithoutCancel(ctx)
	var resultJSON []byte
	if result != nil {
		var err error
		if resultJSON, err = json.Marshal(result); err != nil {
			s.log.Error("marshal op result", "op_id", op.ID, "err", err)
		}
	}
	var errText *string
	if opErr != nil {
		msg := opErr.Error()
		errText = &msg
	}
	next := time.Now()
	if status == StatusFailed {
		next = next.Add(s.backoff(op.Attempts))
	}
	_, err := s.db.Exec(ctx, `
		UPDATE sub2api_ops
		SET status = $2, result = COALESCE($3, result), last_error = $4, next_attempt_at = $5,
		    locked_until = NULL, updated_at = now(),
		    done_at = CASE WHEN $2 = 'done' THEN now() ELSE done_at END
		WHERE id = $1`,
		op.ID, status, resultJSON, errText, next)
	if err != nil {
		s.log.Error("update op status", "op_id", op.ID, "status", status, "err", err)
		return
	}
	switch status {
	case StatusDead:
		s.log.Error("sub2api op dead", "op_id", op.ID, "type", op.Type, "target", op.Target, "attempts", op.Attempts, "err", opErr)
	case StatusFailed:
		s.log.Warn("sub2api op failed, will retry", "op_id", op.ID, "type", op.Type, "attempts", op.Attempts, "err", opErr)
	}
}

// Run 循环发送直到 ctx 结束。一批取满时立即继续，否则等待 interval。
func (s *Sender) Run(ctx context.Context, interval time.Duration) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		n, err := s.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Error("sub2api ops run", "err", err)
		}
		if n >= s.batch {
			timer.Reset(0)
		} else {
			timer.Reset(interval)
		}
	}
}

// PendingTargets 返回给定 target 中仍有未完成操作（pending / sending / failed）的集合，
// 调度器据此避免在上一批操作生效前重复下发。
func PendingTargets(ctx context.Context, db DB, targets []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(targets) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT DISTINCT target FROM sub2api_ops
		WHERE target = ANY($1) AND status IN ('pending', 'sending', 'failed')`, targets)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

// Status 返回操作的当前状态，用于等待某个操作完成（例如充值到账）。
func Status(ctx context.Context, db DB, id int64) (string, error) {
	var s string
	err := db.QueryRow(ctx, "SELECT status FROM sub2api_ops WHERE id = $1", id).Scan(&s)
	return s, err
}
