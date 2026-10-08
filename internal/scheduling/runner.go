package scheduling

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const kickChannel = "tm:scheduler:kick"

// RedisKicker 通过 Redis 通知 worker 中的调度器立即运行一轮（API 与 worker 是不同进程）。
type RedisKicker struct {
	Redis *redis.Client
}

func (k RedisKicker) Kick(ctx context.Context) {
	if k.Redis != nil {
		_ = k.Redis.Publish(ctx, kickChannel, "1").Err()
	}
}

// removalGrace 是删除上游时，从停止调度到删除 sub2api 账号之间等待进行中请求结束的时间。
const removalGrace = 5 * time.Minute

type Runner struct {
	DB       *pgxpool.Pool
	Replica  *replica.Reader
	Redis    *redis.Client
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
	Now      func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run 周期运行，并在收到 kick 时立即运行（合并 1 秒内的多次通知）。
func (r *Runner) Run(ctx context.Context, interval time.Duration) {
	var kicks <-chan *redis.Message
	if r.Redis != nil {
		sub := r.Redis.Subscribe(ctx, kickChannel)
		defer func() { _ = sub.Close() }()
		kicks = sub.Channel()
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-kicks:
			time.Sleep(time.Second)
			drain(kicks)
		}
		if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.Log.Error("scheduler cycle", "err", err)
		}
		timer.Reset(interval)
	}
}

func drain(ch <-chan *redis.Message) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// CycleResult 汇总一轮调度的结果。
type CycleResult struct {
	Upstreams int
	Enqueued  int
	Missing   int
	Removed   int
}

// RunOnce 运行一轮：规划 → 保存期望状态 → 读取实际状态 → 比对下发 → 同步健康状态 → 处理删除。
func (r *Runner) RunOnce(ctx context.Context) error {
	_, err := r.Cycle(ctx)
	return err
}

func (r *Runner) Cycle(ctx context.Context) (*CycleResult, error) {
	st, err := r.Settings(ctx)
	if err != nil {
		return nil, err
	}
	pc, ups, err := LoadPlanInputs(ctx, r.DB, st)
	if err != nil {
		return nil, fmt.Errorf("load plan inputs: %w", err)
	}
	desired := Plan(pc, ups)
	if err := SaveDesired(ctx, r.DB, desired); err != nil {
		return nil, fmt.Errorf("save desired: %w", err)
	}

	actual, err := r.loadActual(ctx)
	if err != nil {
		return nil, fmt.Errorf("load actual: %w", err)
	}
	targets := make([]string, 0, len(desired)*2)
	for _, d := range desired {
		targets = append(targets, fmt.Sprintf("account:%d", d.AccountID))
		if d.ListingGroupID != nil {
			targets = append(targets, fmt.Sprintf("group:%d", *d.ListingGroupID))
		}
	}
	pending, err := ops.PendingTargets(ctx, r.DB, targets)
	if err != nil {
		return nil, err
	}
	applied, err := r.appliedThresholds(ctx)
	if err != nil {
		return nil, err
	}
	planned, missing := Diff(DiffInput{
		Desired: desired, Actual: actual, Pending: pending, AppliedThreshold: applied, Budget: st.SchedulerChangeBudget,
	})
	stamp := r.now().UnixNano()
	for i, p := range planned {
		prio := ops.PriorityNormal
		if p.Urgent {
			prio = ops.PriorityUrgent
		}
		if _, err := ops.Enqueue(ctx, r.DB, ops.NewOp{
			Type: p.Type, Target: p.Target, Payload: p.Payload, Priority: prio,
			IdempotencyKey: fmt.Sprintf("sched-%s-%d-%d", p.Target, stamp, i),
		}); err != nil {
			return nil, err
		}
		if p.AppliedThreshold != nil {
			if _, err := r.DB.Exec(ctx, `UPDATE upstream_runtime_params SET applied_window_threshold = $2, applied_at = now() WHERE upstream_id = $1`,
				p.Upstream, *p.AppliedThreshold); err != nil {
				return nil, err
			}
		}
	}
	for _, id := range missing {
		if _, err := listing.SetHealth(ctx, r.DB, id, listing.HealthUnavailable, "sub2api: account missing"); err != nil {
			return nil, err
		}
	}
	if err := r.syncAccountHealth(ctx, ups, actual); err != nil {
		return nil, err
	}
	removed, removalOps, err := r.processRemovals(ctx, actual, pending)
	if err != nil {
		return nil, err
	}
	r.reportOrphans(ctx, actual)
	return &CycleResult{Upstreams: len(ups), Enqueued: len(planned) + removalOps, Missing: len(missing), Removed: removed}, nil
}

func (r *Runner) loadActual(ctx context.Context) (Actual, error) {
	act := Actual{Accounts: map[int64]replica.Account{}, Groups: map[int64][]int64{}, GroupRow: map[int64]replica.Group{}}
	accounts, err := r.Replica.TMAccounts(ctx)
	if err != nil {
		return act, err
	}
	for _, a := range accounts {
		if a.DeletedAt == nil {
			act.Accounts[a.ID] = a
		}
	}
	members, err := r.Replica.TMAccountGroups(ctx)
	if err != nil {
		return act, err
	}
	for _, m := range members {
		act.Groups[m.AccountID] = append(act.Groups[m.AccountID], m.GroupID)
	}
	for id, gs := range act.Groups {
		act.Groups[id] = uniqueSorted(gs)
	}
	groups, err := r.Replica.TMGroups(ctx)
	if err != nil {
		return act, err
	}
	for _, g := range groups {
		if g.DeletedAt == nil {
			act.GroupRow[g.ID] = g
		}
	}
	return act, nil
}

func (r *Runner) appliedThresholds(ctx context.Context) (map[int64]*int, error) {
	rows, err := r.DB.Query(ctx, `SELECT upstream_id, applied_window_threshold FROM upstream_runtime_params`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*int{}
	for rows.Next() {
		var id int64
		var thr *int16
		if err := rows.Scan(&id, &thr); err != nil {
			return nil, err
		}
		if thr != nil {
			v := int(*thr)
			out[id] = &v
		}
	}
	return out, rows.Err()
}

// syncAccountHealth 根据 sub2api 账号状态维护"不可用"：账号被标为 error（授权失效、封禁等）时不可用，恢复后解除。
func (r *Runner) syncAccountHealth(ctx context.Context, ups []UpstreamInput, actual Actual) error {
	for _, u := range ups {
		a, ok := actual.Accounts[u.AccountID]
		if !ok {
			continue
		}
		var reason string
		if a.ErrorMessage != nil {
			reason = *a.ErrorMessage
		}
		switch {
		case (a.Status == "error" || a.Status == "disabled") && u.Health != listing.HealthDegraded:
			// 已判定降智的保持降智（同样不调度），恢复复测的排期不受影响
			if _, err := listing.SetHealth(ctx, r.DB, u.ID, listing.HealthUnavailable, truncate("sub2api: "+a.Status+" "+reason, 300)); err != nil {
				return err
			}
		case u.Health == listing.HealthUnavailable:
			var current *string
			if err := r.DB.QueryRow(ctx, "SELECT health_reason FROM upstreams WHERE id = $1", u.ID).Scan(&current); err != nil {
				return err
			}
			if current != nil && strings.HasPrefix(*current, "sub2api:") {
				// 已通过上架检测的上游恢复为正常；仍在检测中的回到未知，由检测结果决定
				next := listing.HealthHealthy
				if u.Status == listing.StatusVerifying || u.Status == listing.StatusVerifyFailed {
					next = listing.HealthUnknown
				}
				if _, err := listing.SetHealth(ctx, r.DB, u.ID, next, ""); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// processRemovals 推进删除中的上游：先停止调度，等待进行中的请求结束后删除 sub2api 账号，最后释放代理。
func (r *Runner) processRemovals(ctx context.Context, actual Actual, pending map[string]bool) (removed, enqueued int, err error) {
	rows, err := r.DB.Query(ctx, `SELECT id, sub2api_account_id, probe_group_id, updated_at FROM upstreams WHERE status = 'removing'`)
	if err != nil {
		return 0, 0, err
	}
	type item struct {
		id         int64
		accountID  *int64
		probeGroup *int64
		since      time.Time
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.accountID, &it.probeGroup, &it.since); err != nil {
			rows.Close()
			return 0, 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, it := range items {
		var acc *replica.Account
		if it.accountID != nil {
			if a, ok := actual.Accounts[*it.accountID]; ok {
				acc = &a
			}
		}
		if acc == nil {
			if _, err := r.DB.Exec(ctx, `UPDATE proxies SET status = 'available', assigned_upstream_id = NULL WHERE assigned_upstream_id = $1`, it.id); err != nil {
				return removed, enqueued, err
			}
			if _, err := r.DB.Exec(ctx, `UPDATE upstreams SET status = 'removed', removed_at = now(), updated_at = now() WHERE id = $1`, it.id); err != nil {
				return removed, enqueued, err
			}
			// 账号已删除，探测分组不再有用
			if it.probeGroup != nil {
				if _, err := ops.Enqueue(ctx, r.DB, ops.NewOp{Type: opshandlers.TypeGroupDelete, Target: fmt.Sprintf("group:%d", *it.probeGroup),
					Payload: opshandlers.GroupDeletePayload{GroupID: *it.probeGroup}, IdempotencyKey: fmt.Sprintf("remove-probe-group-%d", *it.probeGroup)}); err != nil {
					return removed, enqueued, err
				}
				enqueued++
			}
			removed++
			continue
		}
		target := fmt.Sprintf("account:%d", acc.ID)
		if pending[target] {
			continue
		}
		var op ops.NewOp
		switch {
		case acc.Schedulable:
			op = ops.NewOp{Type: opshandlers.TypeAccountSetSchedulable, Target: target, Priority: ops.PriorityUrgent,
				Payload: opshandlers.AccountSchedulablePayload{AccountID: acc.ID, Schedulable: false}}
		case r.now().Sub(it.since) >= removalGrace:
			op = ops.NewOp{Type: opshandlers.TypeAccountDelete, Target: target,
				Payload: opshandlers.AccountDeletePayload{AccountID: acc.ID}}
		default:
			continue
		}
		op.IdempotencyKey = fmt.Sprintf("remove-%s-%s-%d", target, op.Type, r.now().UnixNano())
		if _, err := ops.Enqueue(ctx, r.DB, op); err != nil {
			return removed, enqueued, err
		}
		enqueued++
	}
	return removed, enqueued, nil
}

// reportOrphans 记录 sub2api 中存在、但本系统没有记录的 tm- 账号，不自动删除。
func (r *Runner) reportOrphans(ctx context.Context, actual Actual) {
	ids := make([]int64, 0, len(actual.Accounts))
	for id := range actual.Accounts {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	rows, err := r.DB.Query(ctx, `SELECT sub2api_account_id FROM upstreams WHERE sub2api_account_id = ANY($1)`, ids)
	if err != nil {
		return
	}
	known := map[int64]bool{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			known[id] = true
		}
	}
	rows.Close()
	for id, a := range actual.Accounts {
		if !known[id] && strings.HasPrefix(a.Name, "tm-u-") {
			r.Log.Warn("orphan tm- account in sub2api", "account_id", id, "name", a.Name)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
