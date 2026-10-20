//go:build integration

package ops_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/db"
	"github.com/yx-token/idlicat/market/internal/sub2api/ops"
	"github.com/yx-token/idlicat/market/internal/testutil"
	"github.com/yx-token/idlicat/market/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return pool
}

func status(t *testing.T, pool *pgxpool.Pool, id int64) (string, int) {
	t.Helper()
	var s string
	var attempts int
	if err := pool.QueryRow(context.Background(), "SELECT status, attempts FROM sub2api_ops WHERE id = $1", id).Scan(&s, &attempts); err != nil {
		t.Fatal(err)
	}
	return s, attempts
}

func TestEnqueueIsIdempotent(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	op := ops.NewOp{Type: "t", Target: "account:1", Payload: map[string]int{"a": 1}, IdempotencyKey: "k1"}
	id1, err := ops.Enqueue(ctx, pool, op)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := ops.Enqueue(ctx, pool, op)
	if err != nil || id1 != id2 {
		t.Fatalf("duplicate enqueue should return same id: %d %d %v", id1, id2, err)
	}
}

func TestSenderPreservesPerTargetOrder(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	for _, k := range []string{"a1", "a2", "a3"} {
		if _, err := ops.Enqueue(ctx, pool, ops.NewOp{Type: "rec", Target: "account:1", Payload: k, IdempotencyKey: k}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ops.Enqueue(ctx, pool, ops.NewOp{Type: "rec", Target: "account:2", Payload: "b1", IdempotencyKey: "b1"}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var seen []string
	s := ops.NewSender(pool, ops.SenderConfig{BatchSize: 10})
	s.Register("rec", func(_ context.Context, op ops.Op) (any, error) {
		mu.Lock()
		seen = append(seen, op.IdempotencyKey)
		mu.Unlock()
		return nil, nil
	})

	// 每轮同一 target 只能领取一个，所以 account:1 需要三轮
	for i := 0; i < 5; i++ {
		if _, err := s.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var order []string
	for _, k := range seen {
		if k[0] == 'a' {
			order = append(order, k)
		}
	}
	if len(order) != 3 || order[0] != "a1" || order[1] != "a2" || order[2] != "a3" {
		t.Fatalf("account:1 ops out of order: %v (all: %v)", order, seen)
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 executions, got %v", seen)
	}
}

var errTransient = errors.New("transient")
var errFatal = errors.New("fatal")

func TestSenderRetriesThenDies(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	retryID, _ := ops.Enqueue(ctx, pool, ops.NewOp{Type: "flaky", Target: "x:1", Payload: 1, IdempotencyKey: "r", MaxAttempts: 2})
	fatalID, _ := ops.Enqueue(ctx, pool, ops.NewOp{Type: "broken", Target: "x:2", Payload: 1, IdempotencyKey: "f"})
	unknownID, _ := ops.Enqueue(ctx, pool, ops.NewOp{Type: "nobody", Target: "x:3", Payload: 1, IdempotencyKey: "u"})

	s := ops.NewSender(pool, ops.SenderConfig{Retryable: func(err error) bool { return errors.Is(err, errTransient) }})
	s.Register("flaky", func(context.Context, ops.Op) (any, error) { return nil, errTransient })
	s.Register("broken", func(context.Context, ops.Op) (any, error) { return nil, errFatal })

	if _, err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if st, n := status(t, pool, retryID); st != ops.StatusFailed || n != 1 {
		t.Fatalf("transient error should be failed/1, got %s/%d", st, n)
	}
	if st, _ := status(t, pool, fatalID); st != ops.StatusDead {
		t.Fatalf("non-retryable error should be dead, got %s", st)
	}
	if st, _ := status(t, pool, unknownID); st != ops.StatusDead {
		t.Fatalf("missing handler should be dead, got %s", st)
	}

	// 跳过退避等待，第二次失败达到 max_attempts 后进入 dead
	if _, err := pool.Exec(ctx, "UPDATE sub2api_ops SET next_attempt_at = now() WHERE id = $1", retryID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if st, n := status(t, pool, retryID); st != ops.StatusDead || n != 2 {
		t.Fatalf("exhausted retries should be dead/2, got %s/%d", st, n)
	}
}

func TestSenderGateBlocksWrites(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	id, _ := ops.Enqueue(ctx, pool, ops.NewOp{Type: "rec", Target: "x:1", Payload: 1, IdempotencyKey: "g"})
	allowed := false
	s := ops.NewSender(pool, ops.SenderConfig{Gate: func(context.Context) (bool, string) { return allowed, "safe mode" }})
	s.Register("rec", func(context.Context, ops.Op) (any, error) { return "ok", nil })

	if n, _ := s.RunOnce(ctx); n != 0 {
		t.Fatal("gate closed: nothing should run")
	}
	if st, _ := status(t, pool, id); st != ops.StatusPending {
		t.Fatalf("op should stay pending, got %s", st)
	}
	allowed = true
	if n, _ := s.RunOnce(ctx); n != 1 {
		t.Fatal("gate open: op should run")
	}
	if st, _ := status(t, pool, id); st != ops.StatusDone {
		t.Fatalf("op should be done, got %s", st)
	}
}

func TestSenderReclaimsExpiredLease(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	id, _ := ops.Enqueue(ctx, pool, ops.NewOp{Type: "rec", Target: "x:1", Payload: 1, IdempotencyKey: "l"})
	// 模拟进程在发送途中崩溃：状态停在 sending 且租约已过期
	if _, err := pool.Exec(ctx, "UPDATE sub2api_ops SET status = 'sending', attempts = 1, locked_until = now() - interval '1 minute' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	s := ops.NewSender(pool, ops.SenderConfig{Lease: time.Minute})
	s.Register("rec", func(context.Context, ops.Op) (any, error) { return nil, nil })
	if n, err := s.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("expired lease should be reclaimed: n=%d err=%v", n, err)
	}
	if st, n := status(t, pool, id); st != ops.StatusDone || n != 2 {
		t.Fatalf("got %s/%d", st, n)
	}
}
