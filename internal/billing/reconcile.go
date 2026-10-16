package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Reconciler 做计费对账：Edge 尝试日志与 usage_logs 逐条核对，以及买家余额恒等式核对。
// 计费以 sub2api 实际产生的费用为准（决策 D17）：不一致只记录、告警，由人处理，不自动调账。
type Reconciler struct {
	DB       *pgxpool.Pool
	Replica  *replica.Reader
	Shadow   *buyer.ShadowService
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
	Now      func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

type ReconcileResult struct {
	R1, R2, R3, R4, BalanceMismatch int64
	FrozenKeys                      int
}

// RunAttempts 执行 R1–R4。只处理最近两天的数据，并给尝试日志投递留出时间。
func (r *Reconciler) RunAttempts(ctx context.Context) (*ReconcileResult, error) {
	res := &ReconcileResult{}
	now := r.now()
	since := now.Add(-48 * time.Hour)

	// R1：Edge 记为失败，但 sub2api 产生了费用 → 按 sub2api 计费，记录待解释（只看买家渠道；第三方检测经 Edge 的探测流量由平台承担）
	tag, err := r.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, sub2api_usage_id, edge_attempt_id, buyer_user_id, amount, detail)
		SELECT 'R1_failed_with_charge', 'R1:' || ur.sub2api_usage_id, ur.sub2api_usage_id, a.id, ur.buyer_user_id, ur.actual_cost,
			jsonb_build_object('end_reason', a.end_reason, 'http_status', a.http_status, 'committed', a.committed)
		FROM edge_attempts a JOIN usage_records ur ON ur.sub2api_request_id = 'client:' || a.sub2api_request_id
		WHERE a.started_at > $1 AND a.sub2api_request_id IS NOT NULL AND ur.channel IN ('pool', 'direct')
			AND (a.end_reason IN ('retry', 'upstream_error', 'timeout') OR (a.http_status >= 400))
		ON CONFLICT (dedupe_key) DO NOTHING`, since)
	if err != nil {
		return nil, fmt.Errorf("R1: %w", err)
	}
	res.R1 = tag.RowsAffected()

	// R2：Edge 记为成功完成的计费接口，10 分钟后仍无费用记录 → 可能漏记用量
	tag, err = r.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, edge_attempt_id, detail)
		SELECT 'R2_success_without_charge', 'R2:' || a.id, a.id,
			jsonb_build_object('http_status', a.http_status, 'sub2api_request_id', a.sub2api_request_id, 'buyer_key_id', a.buyer_key_id)
		FROM edge_attempts a
		WHERE a.started_at > $1 AND a.started_at < $2 AND a.end_reason = 'completed' AND a.billable
			AND a.http_status BETWEEN 200 AND 299 AND a.sub2api_request_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM usage_records ur WHERE ur.sub2api_request_id = 'client:' || a.sub2api_request_id)
		ON CONFLICT (dedupe_key) DO NOTHING`, since, now.Add(-10*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("R2: %w", err)
	}
	res.R2 = tag.RowsAffected()

	// R3：有买家渠道的费用，但找不到 Edge 尝试 → 可能有人绕过 Edge 直接使用内部 key
	tag, err = r.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, sub2api_usage_id, buyer_user_id, amount, detail)
		SELECT 'R3_orphan_charge', 'R3:' || ur.sub2api_usage_id, ur.sub2api_usage_id, ur.buyer_user_id, ur.actual_cost,
			jsonb_build_object('sub2api_api_key_id', ur.sub2api_api_key_id, 'request_id', ur.sub2api_request_id)
		FROM usage_records ur
		WHERE ur.created_at > $1 AND ur.created_at < $2 AND ur.channel IN ('pool', 'direct')
			AND ur.sub2api_request_id LIKE 'client:%'
			AND NOT EXISTS (SELECT 1 FROM edge_attempts a WHERE 'client:' || a.sub2api_request_id = ur.sub2api_request_id)
		ON CONFLICT (dedupe_key) DO NOTHING`, since, now.Add(-30*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("R3: %w", err)
	}
	res.R3 = tag.RowsAffected()

	// R4：同一 sub2api 请求 ID 有多条费用记录（sub2api 本身按请求去重扣费，正常不应出现）
	tag, err = r.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, sub2api_usage_id, buyer_user_id, amount, detail)
		SELECT 'R4_duplicate', 'R4:' || ur.sub2api_usage_id, ur.sub2api_usage_id, ur.buyer_user_id, ur.actual_cost,
			jsonb_build_object('request_id', ur.sub2api_request_id)
		FROM usage_records ur
		WHERE ur.created_at > $1 AND ur.sub2api_request_id IS NOT NULL AND ur.sub2api_request_id IN (
			SELECT sub2api_request_id FROM usage_records WHERE created_at > $1 AND sub2api_request_id IS NOT NULL
			GROUP BY sub2api_request_id, sub2api_api_key_id HAVING count(*) > 1)
		ON CONFLICT (dedupe_key) DO NOTHING`, since)
	if err != nil {
		return nil, fmt.Errorf("R4: %w", err)
	}
	res.R4 = tag.RowsAffected()

	frozen, err := r.freezeOrphanKeys(ctx, since)
	if err != nil {
		return nil, err
	}
	creditFrozen, err := r.freezeOrphanCreditKeys(ctx, since)
	if err != nil {
		return nil, err
	}
	frozen += creditFrozen
	res.FrozenKeys = frozen
	if res.R1+res.R2+res.R3+res.R4 > 0 {
		r.Log.Warn("billing anomalies found", "R1", res.R1, "R2", res.R2, "R3", res.R3, "R4", res.R4, "frozen_keys", frozen)
	}
	return res, nil
}

// freezeOrphanKeys 对同一内部 key 在窗口内出现 3 次及以上"无来源费用"的，停用该 key 待排查。
// 设置门槛是为了避免个别尝试日志丢失造成误冻结。
func (r *Reconciler) freezeOrphanKeys(ctx context.Context, since time.Time) (int, error) {
	rows, err := r.DB.Query(ctx, `SELECT ik.id, ik.sub2api_key_id, ik.buyer_user_id, ik.buyer_key_id
		FROM billing_anomalies ba
		JOIN usage_records ur ON ur.sub2api_usage_id = ba.sub2api_usage_id
		JOIN internal_keys ik ON ik.sub2api_key_id = ur.sub2api_api_key_id
		WHERE ba.rule = 'R3_orphan_charge' AND ba.status = 'open' AND ba.created_at > $1 AND ik.status = 'active'
		GROUP BY ik.id, ik.sub2api_key_id, ik.buyer_user_id, ik.buyer_key_id HAVING count(*) >= 3`, since)
	if err != nil {
		return 0, err
	}
	type item struct{ id, subKey, userID, keyID int64 }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.subKey, &it.userID, &it.keyID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	n := 0
	for _, it := range items {
		ident, err := r.Shadow.Get(ctx, buyer.KindBuyer, it.userID)
		if err != nil || ident == nil {
			continue
		}
		if err := r.Shadow.SetKeyStatus(ctx, ident, it.subKey, false); err != nil {
			r.Log.Error("freeze internal key", "internal_key_id", it.id, "err", err)
			continue
		}
		if _, err := r.DB.Exec(ctx, "UPDATE internal_keys SET status = 'frozen' WHERE id = $1", it.id); err != nil {
			return n, err
		}
		r.Log.Error("internal key frozen after orphan charges", "internal_key_id", it.id, "buyer_key_id", it.keyID)
		n++
	}
	return n, nil
}

// RunBalances 核对买家余额恒等式：sub2api 余额 = 充值 − 退款 − sub2api 消费 ± 人工调整。
// 账本余额扣除游标之后尚未入账的消费、加上仍在出站队列中的余额操作后应与 sub2api 余额一致，
// 差异超过容差记为 balance_mismatch。
func (r *Reconciler) RunBalances(ctx context.Context) (int64, error) {
	st, err := r.Settings(ctx)
	if err != nil {
		return 0, err
	}
	// 预期余额 = 账面现金 + 未用额度包 remaining（预付未扣 sub2api，仍可调用）
	rows, err := r.DB.Query(ctx, `SELECT si.owner_id, si.sub2api_user_id,
			COALESCE((SELECT -sum(e.amount) FROM ledger_accounts a JOIN ledger_entries e ON e.account_id = a.id
				WHERE a.owner_type = 'buyer' AND a.owner_id = si.owner_id AND a.kind = 'buyer_cash'), 0)::float8
			+ COALESCE((SELECT sum(remaining) FROM quota_packs WHERE buyer_id = si.owner_id AND status = 'active'), 0)::float8
		FROM shadow_identities si WHERE si.kind = 'buyer' AND si.sub2api_user_id IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	type item struct {
		userID, subUserID int64
		expected          float64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.userID, &it.subUserID, &it.expected); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if len(items) == 0 {
		return r.runStoreCreditBalances(ctx, st.BalanceMismatchTolerance)
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.subUserID
	}
	balances, err := r.Replica.UserBalances(ctx, ids)
	if err != nil {
		return 0, err
	}
	actual := map[int64]float64{}
	for _, b := range balances {
		actual[b.ID] = b.Balance
	}
	// 游标之后尚未入账的消费：sub2api 余额已扣、账本尚未扣
	var cursor int64
	if err := r.DB.QueryRow(ctx, "SELECT last_id FROM ingest_cursors WHERE source = 'usage_logs'").Scan(&cursor); err != nil {
		return 0, err
	}
	uningested, err := r.Replica.UsageSumAfter(ctx, cursor, ids)
	if err != nil {
		return 0, err
	}
	var found int64
	day := r.now().Format("2006-01-02")
	for _, it := range items {
		// 充值与人工调整先记账再下发，下发前账面与 sub2api 的差额属于正常差异；
		// 退款是 sub2api 扣减成功后才记账，不计入
		var inflight float64
		if err := r.DB.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN op_type = 'user.balance_add' THEN (payload->>'amount')::float8
				ELSE -(payload->>'amount')::float8 END), 0)
			FROM sub2api_ops WHERE target = $1 AND status IN ('pending', 'sending', 'failed')
				AND op_type IN ('user.balance_add', 'user.balance_subtract')
				AND idempotency_key NOT LIKE 'tm-refund-%'`, fmt.Sprintf("user:%d", it.subUserID)).Scan(&inflight); err != nil {
			return found, err
		}
		expected := it.expected - uningested[it.subUserID]
		diff := actual[it.subUserID] + inflight - expected
		if abs(diff) <= st.BalanceMismatchTolerance {
			continue
		}
		detail, _ := json.Marshal(map[string]any{"sub2api_balance": actual[it.subUserID], "ledger_balance": it.expected,
			"uningested_usage": uningested[it.subUserID], "inflight_ops": inflight, "diff": diff})
		tag, err := r.DB.Exec(ctx, `INSERT INTO billing_anomalies (rule, dedupe_key, buyer_user_id, amount, detail)
			VALUES ('balance_mismatch', $1, $2, $3, $4) ON CONFLICT (dedupe_key) DO NOTHING`,
			fmt.Sprintf("balance:%d:%s", it.userID, day), it.userID, diff, detail)
		if err != nil {
			return found, err
		}
		found += tag.RowsAffected()
	}
	creditFound, err := r.runStoreCreditBalances(ctx, st.BalanceMismatchTolerance)
	return found + creditFound, err
}

type Anomaly struct {
	ID            int64           `json:"id"`
	Rule          string          `json:"rule"`
	SubAPIUsageID *int64          `json:"sub2api_usage_id,omitempty"`
	EdgeAttemptID *int64          `json:"edge_attempt_id,omitempty"`
	BuyerUserID   *int64          `json:"buyer_user_id,omitempty"`
	Amount        *float64        `json:"amount,omitempty"`
	Detail        json.RawMessage `json:"detail,omitempty"`
	Status        string          `json:"status"`
	Note          *string         `json:"note,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func ListAnomalies(ctx context.Context, db DB, status string, limit, offset int) ([]Anomaly, error) {
	rows, err := db.Query(ctx, `SELECT id, rule, sub2api_usage_id, edge_attempt_id, buyer_user_id, amount::float8, detail, status, note, created_at
		FROM billing_anomalies WHERE $1 = '' OR status = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Anomaly, error) {
		var a Anomaly
		err := row.Scan(&a.ID, &a.Rule, &a.SubAPIUsageID, &a.EdgeAttemptID, &a.BuyerUserID, &a.Amount, &a.Detail, &a.Status, &a.Note, &a.CreatedAt)
		return a, err
	})
}

// ResolveAnomaly 标记为已解释或忽略（人工调账走 Funds.Adjust）。
func ResolveAnomaly(ctx context.Context, db DB, id int64, status, note string) error {
	if status != "explained" && status != "ignored" {
		return httpx.BadRequest("INVALID_STATUS", "status must be explained or ignored")
	}
	tag, err := db.Exec(ctx, `UPDATE billing_anomalies SET status = $2, note = $3, resolved_at = now() WHERE id = $1 AND status = 'open'`, id, status, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ANOMALY_NOT_OPEN", "anomaly not found or already resolved")
	}
	return nil
}

// ---- 买家 Key 花费限额 ----

type LimitEnforcer struct {
	DB   *pgxpool.Pool
	Keys *buyer.KeyService
	Log  *slog.Logger
	Now  func() time.Time
}

// RunOnce 按用量统计每个设置了花费限额的 Key，超限时封禁（推送 Edge），恢复后解除。
func (e *LimitEnforcer) RunOnce(ctx context.Context) error {
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	rows, err := e.DB.Query(ctx, `SELECT id, limits, limit_blocked_until FROM buyer_keys
		WHERE status = 'active' AND (limits ? 'quota_usd' OR limits ? 'rate_limit_5h_usd' OR limits ? 'rate_limit_1d_usd'
			OR limits ? 'rate_limit_7d_usd' OR limit_blocked_until IS NOT NULL)`)
	if err != nil {
		return err
	}
	type item struct {
		id      int64
		limits  buyer.Limits
		blocked *time.Time
	}
	var items []item
	for rows.Next() {
		var it item
		var raw []byte
		if err := rows.Scan(&it.id, &raw, &it.blocked); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &it.limits); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		var total, h5, d1, d7 float64
		if err := e.DB.QueryRow(ctx, `SELECT COALESCE(sum(actual_cost), 0)::float8,
				COALESCE(sum(actual_cost) FILTER (WHERE created_at > $2::timestamptz - interval '5 hours'), 0)::float8,
				COALESCE(sum(actual_cost) FILTER (WHERE created_at > $2::timestamptz - interval '1 day'), 0)::float8,
				COALESCE(sum(actual_cost) FILTER (WHERE created_at > $2::timestamptz - interval '7 days'), 0)::float8
			FROM usage_records WHERE buyer_key_id = $1`, it.id, now).Scan(&total, &h5, &d1, &d7); err != nil {
			return err
		}
		var until *time.Time
		reason := ""
		block := func(limit *float64, used float64, window time.Duration, name string) {
			if limit == nil || used < *limit {
				return
			}
			t := now.Add(window)
			if until == nil || t.After(*until) {
				until, reason = &t, name
			}
		}
		block(it.limits.QuotaUSD, total, 100*365*24*time.Hour, "quota exhausted")
		block(it.limits.RateLimit7d, d7, time.Hour, "7-day limit reached")
		block(it.limits.RateLimit1d, d1, 30*time.Minute, "daily limit reached")
		block(it.limits.RateLimit5h, h5, 10*time.Minute, "5-hour limit reached")

		switch {
		case until != nil:
			if _, err := e.DB.Exec(ctx, `UPDATE buyer_keys SET limit_blocked_until = $2, limit_block_reason = $3, config_version = config_version + 1 WHERE id = $1`,
				it.id, *until, reason); err != nil {
				return err
			}
			if err := e.Keys.Publish(ctx, it.id); err != nil {
				return err
			}
		case it.blocked != nil:
			if _, err := e.DB.Exec(ctx, `UPDATE buyer_keys SET limit_blocked_until = NULL, limit_block_reason = NULL, config_version = config_version + 1 WHERE id = $1`, it.id); err != nil {
				return err
			}
			if err := e.Keys.Publish(ctx, it.id); err != nil {
				return err
			}
		}
	}
	return nil
}
