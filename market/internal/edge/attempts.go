package edge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Attempt 是一跳的尝试记录，用于与 sub2api 的 usage_logs 逐条对账。
type Attempt struct {
	EdgeRequestID    string    `json:"edge_request_id"`
	BuyerKeyID       int64     `json:"buyer_key_id"`
	Hop              int       `json:"hop"`
	ListingID        int64     `json:"listing_id,omitempty"`
	PoolID           int64     `json:"pool_id,omitempty"`
	Sub2APIRequestID string    `json:"sub2api_request_id,omitempty"`
	HTTPStatus       int       `json:"http_status,omitempty"`
	ErrorClass       string    `json:"error_class"`
	Committed        bool      `json:"committed"`
	Billable         bool      `json:"billable"`
	EndReason        string    `json:"end_reason"` // completed / client_cancel / upstream_error / timeout / skipped / retry
	TTFBMs           int       `json:"ttfb_ms,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
}

// AttemptLogger 异步批量写入尝试日志；写库失败时追加到本地暂存文件，稍后重放，不阻塞请求。
type AttemptLogger struct {
	db       *pgxpool.Pool
	spoolDir string
	log      *slog.Logger
	ch       chan Attempt
	done     chan struct{}
}

func NewAttemptLogger(db *pgxpool.Pool, spoolDir string, log *slog.Logger) *AttemptLogger {
	return &AttemptLogger{db: db, spoolDir: spoolDir, log: log, ch: make(chan Attempt, 10000), done: make(chan struct{})}
}

// Record 记录一次尝试。队列满时直接写暂存文件，绝不阻塞请求。
func (l *AttemptLogger) Record(a Attempt) {
	select {
	case l.ch <- a:
	default:
		l.spool([]Attempt{a})
	}
}

// Run 持续写库，直到 ctx 结束并把队列中剩余的记录写完。
func (l *AttemptLogger) Run(ctx context.Context) {
	defer close(l.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	replay := time.NewTicker(30 * time.Second)
	defer replay.Stop()
	var batch []Attempt
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := l.insert(context.WithoutCancel(ctx), batch); err != nil {
			l.log.Warn("write edge attempts failed, spooling", "count", len(batch), "err", err)
			l.spool(batch)
		}
		batch = batch[:0]
	}
	for {
		select {
		case a := <-l.ch:
			batch = append(batch, a)
			if len(batch) >= 500 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-replay.C:
			l.replaySpool(ctx)
		case <-ctx.Done():
			for {
				select {
				case a := <-l.ch:
					batch = append(batch, a)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Wait 等待 Run 退出。
func (l *AttemptLogger) Wait() { <-l.done }

func (l *AttemptLogger) insert(ctx context.Context, batch []Attempt) error {
	if l.db == nil {
		return fmt.Errorf("no database configured")
	}
	rows := make([][]any, len(batch))
	for i, a := range batch {
		var lid, pid *int64
		if a.ListingID > 0 {
			lid = &a.ListingID
		}
		if a.PoolID > 0 {
			pid = &a.PoolID
		}
		var reqID *string
		if a.Sub2APIRequestID != "" {
			reqID = &a.Sub2APIRequestID
		}
		var status, ttfb *int
		if a.HTTPStatus > 0 {
			status = &a.HTTPStatus
		}
		if a.TTFBMs > 0 {
			ttfb = &a.TTFBMs
		}
		rows[i] = []any{a.EdgeRequestID, a.BuyerKeyID, a.Hop, lid, pid, reqID, status, a.Committed, a.Billable, a.EndReason, ttfb, a.StartedAt, a.EndedAt, a.ErrorClass}
	}
	_, err := l.db.CopyFrom(ctx, pgx.Identifier{"edge_attempts"},
		[]string{"edge_request_id", "buyer_key_id", "hop", "listing_id", "pool_id", "sub2api_request_id", "http_status",
			"committed", "billable", "end_reason", "ttfb_ms", "started_at", "ended_at", "error_class"},
		pgx.CopyFromRows(rows))
	return err
}

func (l *AttemptLogger) spool(batch []Attempt) {
	if err := os.MkdirAll(l.spoolDir, 0o755); err != nil {
		l.log.Error("create spool dir", "err", err)
		return
	}
	name := filepath.Join(l.spoolDir, fmt.Sprintf("attempts-%d.jsonl", time.Now().UnixNano()))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		l.log.Error("open spool file", "err", err)
		return
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, a := range batch {
		if err := enc.Encode(a); err != nil {
			l.log.Error("write spool file", "err", err)
			return
		}
	}
}

func (l *AttemptLogger) replaySpool(ctx context.Context) {
	files, err := filepath.Glob(filepath.Join(l.spoolDir, "attempts-*.jsonl"))
	if err != nil || len(files) == 0 {
		return
	}
	sort.Strings(files)
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		var batch []Attempt
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var a Attempt
			if json.Unmarshal(sc.Bytes(), &a) == nil {
				batch = append(batch, a)
			}
		}
		_ = f.Close()
		if err := l.insert(ctx, batch); err != nil {
			return // 数据库仍不可用，下次再试
		}
		_ = os.Remove(name)
	}
}
