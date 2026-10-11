// Package replica 读取 sub2api 只读库。所有 SQL 都在 contract/queries 中，
// 受 s2a-compat 校验；本包只负责执行与扫描，不拼接 SQL。
package replica

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DueGin/token-market/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier 是 pgxpool.Pool / pgx.Conn / pgx.Tx 的公共子集。
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

const (
	qAppliedMigrations = "applied_migrations"
	qUsageLogsAfter    = "usage_logs_after"
	qTMAccounts        = "tm_accounts"
	qTMAccountGroups   = "tm_account_groups"
	qTMGroups          = "tm_groups"
	qUserBalances      = "user_balances"
	qUserByEmail       = "user_by_email"
	qAPIKeyByKey       = "api_key_by_key"
	qGroupByName       = "group_by_name"
	qAccountByName     = "account_by_name"
	qUsageSumAfter     = "usage_sum_after"
	qAutoResetAudits   = "auto_reset_audits"
)

type Reader struct {
	db      Querier
	queries map[string]string
}

func New(db Querier) (*Reader, error) {
	qs, err := contract.Queries()
	if err != nil {
		return nil, err
	}
	for _, name := range []string{qAppliedMigrations, qUsageLogsAfter, qTMAccounts, qTMAccountGroups, qTMGroups, qUserBalances,
		qUserByEmail, qAPIKeyByKey, qGroupByName, qAccountByName, qUsageSumAfter, qAutoResetAudits} {
		if _, ok := qs[name]; !ok {
			return nil, fmt.Errorf("replica: contract query %s missing", name)
		}
	}
	return &Reader{db: db, queries: qs}, nil
}

type AppliedMigration struct {
	Filename string
	Checksum string
}

func (r *Reader) AppliedMigrations(ctx context.Context) ([]AppliedMigration, error) {
	rows, err := r.db.Query(ctx, r.queries[qAppliedMigrations])
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qAppliedMigrations, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AppliedMigration, error) {
		var m AppliedMigration
		err := row.Scan(&m.Filename, &m.Checksum)
		return m, err
	})
}

// AppliedMigrationNames 返回已执行迁移的文件名，供兼容自检计算指纹。
func (r *Reader) AppliedMigrationNames(ctx context.Context) ([]string, error) {
	ms, err := r.AppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Filename
	}
	return names, nil
}

// UsageLog 是 usage_logs 中本系统关心的列。可空列用指针。
type UsageLog struct {
	ID                    int64
	RequestID             *string
	CreatedAt             time.Time
	UserID                int64
	APIKeyID              int64
	AccountID             int64
	GroupID               *int64
	Model                 string
	InputTokens           int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	TotalCost             float64
	ActualCost            float64
	AccountRateMultiplier *float64
	FirstTokenMs          *int
	DurationMs            *int
	UpstreamResponseModel *string
	UpstreamModelMismatch *bool
}

// UsageLogsAfter 按 id 增量读取 created_at 早于 before 的用量记录。
func (r *Reader) UsageLogsAfter(ctx context.Context, afterID int64, before time.Time, limit int) ([]UsageLog, error) {
	rows, err := r.db.Query(ctx, r.queries[qUsageLogsAfter], afterID, before, limit)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qUsageLogsAfter, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageLog, error) {
		var u UsageLog
		err := row.Scan(&u.ID, &u.RequestID, &u.CreatedAt, &u.UserID, &u.APIKeyID, &u.AccountID, &u.GroupID, &u.Model,
			&u.InputTokens, &u.OutputTokens, &u.CacheCreationTokens, &u.CacheReadTokens,
			&u.TotalCost, &u.ActualCost, &u.AccountRateMultiplier, &u.FirstTokenMs, &u.DurationMs,
			&u.UpstreamResponseModel, &u.UpstreamModelMismatch)
		return u, err
	})
}

type Account struct {
	SessionWindowEnd        *time.Time
	ID                      int64
	Name                    string
	Platform                string
	Type                    string
	Status                  string
	ErrorMessage            *string
	Schedulable             bool
	Priority                int
	Concurrency             int
	LoadFactor              *int
	RateMultiplier          float64
	RateLimitResetAt        *time.Time
	OverloadUntil           *time.Time
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason *string
	Extra                   map[string]any
	DeletedAt               *time.Time
}

func (r *Reader) TMAccounts(ctx context.Context) ([]Account, error) {
	rows, err := r.db.Query(ctx, r.queries[qTMAccounts])
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qTMAccounts, err)
	}
	return pgx.CollectRows(rows, scanAccount)
}

// AccountByName 按名称查找账号，不存在时返回 nil。
func (r *Reader) AccountByName(ctx context.Context, name string) (*Account, error) {
	rows, err := r.db.Query(ctx, r.queries[qAccountByName], name)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qAccountByName, err)
	}
	accounts, err := pgx.CollectRows(rows, scanAccount)
	if err != nil || len(accounts) == 0 {
		return nil, err
	}
	return &accounts[0], nil
}

func scanAccount(row pgx.CollectableRow) (Account, error) {
	var a Account
	var extra []byte
	err := row.Scan(&a.ID, &a.Name, &a.Platform, &a.Type, &a.Status, &a.ErrorMessage, &a.Schedulable, &a.Priority,
		&a.Concurrency, &a.LoadFactor, &a.RateMultiplier, &a.RateLimitResetAt, &a.OverloadUntil,
		&a.TempUnschedulableUntil, &a.TempUnschedulableReason, &extra, &a.DeletedAt, &a.SessionWindowEnd)
	if err != nil {
		return a, err
	}
	if len(extra) > 0 {
		if err := json.Unmarshal(extra, &a.Extra); err != nil {
			return a, fmt.Errorf("account %d extra: %w", a.ID, err)
		}
	}
	return a, nil
}

// AutoResetAudit 是 sub2api 自动使用 Codex 重置卡的一条审计记录。Extra 含 account_id、result_code
// （success / no_credit / failed / recovery_failed）、trigger_window、windows_reset、error_code 等。
type AutoResetAudit struct {
	ID        int64
	CreatedAt time.Time
	Extra     map[string]any
}

// AutoResetAudits 返回 since 之后最近的自动用卡审计记录（按 ID 降序，最多 500 条）。
func (r *Reader) AutoResetAudits(ctx context.Context, since time.Time) ([]AutoResetAudit, error) {
	rows, err := r.db.Query(ctx, r.queries[qAutoResetAudits], since)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qAutoResetAudits, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AutoResetAudit, error) {
		var a AutoResetAudit
		var extra []byte
		if err := row.Scan(&a.ID, &a.CreatedAt, &extra); err != nil {
			return a, err
		}
		if len(extra) > 0 {
			if err := json.Unmarshal(extra, &a.Extra); err != nil {
				return a, fmt.Errorf("audit %d extra: %w", a.ID, err)
			}
		}
		return a, nil
	})
}

type AccountGroup struct {
	AccountID int64
	GroupID   int64
}

func (r *Reader) TMAccountGroups(ctx context.Context) ([]AccountGroup, error) {
	rows, err := r.db.Query(ctx, r.queries[qTMAccountGroups])
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qTMAccountGroups, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AccountGroup, error) {
		var ag AccountGroup
		err := row.Scan(&ag.AccountID, &ag.GroupID)
		return ag, err
	})
}

type Group struct {
	ID                   int64
	Name                 string
	Platform             string
	RateMultiplier       float64
	IsExclusive          bool
	ProfitControlEnabled bool
	DeletedAt            *time.Time
}

func (r *Reader) TMGroups(ctx context.Context) ([]Group, error) {
	rows, err := r.db.Query(ctx, r.queries[qTMGroups])
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qTMGroups, err)
	}
	return pgx.CollectRows(rows, scanGroup)
}

// GroupByName 按名称查找分组，不存在时返回 nil。
func (r *Reader) GroupByName(ctx context.Context, name string) (*Group, error) {
	rows, err := r.db.Query(ctx, r.queries[qGroupByName], name)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qGroupByName, err)
	}
	groups, err := pgx.CollectRows(rows, scanGroup)
	if err != nil || len(groups) == 0 {
		return nil, err
	}
	return &groups[0], nil
}

func scanGroup(row pgx.CollectableRow) (Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Platform, &g.RateMultiplier, &g.IsExclusive, &g.ProfitControlEnabled, &g.DeletedAt)
	return g, err
}

// UserByEmail 按邮箱查找用户，不存在时返回 nil。
func (r *Reader) UserByEmail(ctx context.Context, email string) (*UserBalance, error) {
	rows, err := r.db.Query(ctx, r.queries[qUserByEmail], email)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qUserByEmail, err)
	}
	users, err := pgx.CollectRows(rows, scanUserBalance)
	if err != nil || len(users) == 0 {
		return nil, err
	}
	return &users[0], nil
}

type APIKeyRow struct {
	ID      int64
	UserID  int64
	GroupID *int64
	Status  string
}

// APIKeyByKey 按 key 值查找，不存在时返回 nil。
func (r *Reader) APIKeyByKey(ctx context.Context, key string) (*APIKeyRow, error) {
	rows, err := r.db.Query(ctx, r.queries[qAPIKeyByKey], key)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qAPIKeyByKey, err)
	}
	keys, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (APIKeyRow, error) {
		var k APIKeyRow
		err := row.Scan(&k.ID, &k.UserID, &k.GroupID, &k.Status)
		return k, err
	})
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	return &keys[0], nil
}

type UserBalance struct {
	ID      int64
	Balance float64
	Status  string
}

func (r *Reader) UserBalances(ctx context.Context, ids []int64) ([]UserBalance, error) {
	rows, err := r.db.Query(ctx, r.queries[qUserBalances], ids)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qUserBalances, err)
	}
	return pgx.CollectRows(rows, scanUserBalance)
}

func scanUserBalance(row pgx.CollectableRow) (UserBalance, error) {
	var u UserBalance
	err := row.Scan(&u.ID, &u.Balance, &u.Status)
	return u, err
}

// UsageSumAfter 返回游标之后各用户的消费合计（用于余额对账时扣除尚未入账的部分）。
func (r *Reader) UsageSumAfter(ctx context.Context, afterID int64, userIDs []int64) (map[int64]float64, error) {
	rows, err := r.db.Query(ctx, r.queries[qUsageSumAfter], afterID, userIDs)
	if err != nil {
		return nil, fmt.Errorf("replica %s: %w", qUsageSumAfter, err)
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var id int64
		var sum float64
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, err
		}
		out[id] = sum
	}
	return out, rows.Err()
}

// ExplainAll 对每条契约查询执行 EXPLAIN（参数用 NULL 占位），用于在目标版本的表结构上
// 验证查询仍然有效。s2a-compat 的结构比对阶段会调用它。
func ExplainAll(ctx context.Context, db Querier) map[string]error {
	qs, err := contract.Queries()
	if err != nil {
		return map[string]error{"*": err}
	}
	out := make(map[string]error, len(qs))
	for name, sql := range qs {
		args := make([]any, countParams(sql))
		_, err := db.Exec(ctx, "EXPLAIN "+sql, args...)
		out[name] = err
	}
	return out
}

// countParams 返回 SQL 中最大的 $N 编号。
func countParams(sql string) int {
	max := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		n, j := 0, i+1
		for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
			n = n*10 + int(sql[j]-'0')
			j++
		}
		if n > max {
			max = n
		}
	}
	return max
}
