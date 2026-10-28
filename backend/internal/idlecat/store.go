// Package idlecat 是闲猫市场在 sub2api 上的增量业务（直连市场库 + 本机引擎账号/分组）。
package idlecat

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Store 读市场库；Engine 可选（真实 sub2api 库，用于供给融合）。
type Store struct {
	Market *sql.DB
	Engine *sql.DB
}

// OpenFromEnv 读取 MARKET_DATABASE_DSN；可选 MARKET_ENGINE_DSN（默认同进程可另注入 Engine）。
func OpenFromEnv() (*Store, error) {
	marketDSN := strings.TrimSpace(os.Getenv("MARKET_DATABASE_DSN"))
	if marketDSN == "" {
		return nil, fmt.Errorf("MARKET_DATABASE_DSN empty")
	}
	s := &Store{}
	db, err := sql.Open("postgres", marketDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	// 市场库可能晚于引擎启动（单机部署两套 compose 开机顺序不定）。连接按需建立，
	// 不可达时只记录日志，路由照常挂载，库恢复后请求自动成功。
	if err := db.Ping(); err != nil {
		log.Printf("idlecat: market database not reachable yet: %v", err)
	}
	s.Market = db
	engineDSN := strings.TrimSpace(os.Getenv("MARKET_ENGINE_DSN"))
	if engineDSN != "" {
		edb, err := sql.Open("postgres", engineDSN)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		edb.SetMaxOpenConns(8)
		if err := edb.Ping(); err != nil {
			log.Printf("idlecat: engine database not reachable yet: %v", err)
		}
		s.Engine = edb
	}
	return s, nil
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.Market != nil {
		_ = s.Market.Close()
	}
	if s.Engine != nil {
		_ = s.Engine.Close()
	}
}

// Status 是市场对引擎兼容状态（compat_state）。
type Status struct {
	Sub2APIVersion       string         `json:"sub2api_version"`
	MigrationFingerprint string         `json:"migration_fingerprint"`
	GuardState           string         `json:"guard_state"`
	Reason               *string        `json:"reason"`
	CheckedAt            *time.Time     `json:"checked_at"`
	OpsByStatus          map[string]int `json:"ops_by_status"`
	IngestCursor         int64          `json:"ingest_cursor"`
	Host                 string         `json:"host"`
	Mode                 string         `json:"mode"`
}

func (s *Store) Status(ctx context.Context) (*Status, error) {
	st := &Status{Host: "sub2api", Mode: "native", OpsByStatus: map[string]int{}}
	err := s.Market.QueryRowContext(ctx, `
		SELECT sub2api_version, migration_fingerprint, guard_state, reason, checked_at
		FROM compat_state WHERE id = 1`).
		Scan(&st.Sub2APIVersion, &st.MigrationFingerprint, &st.GuardState, &st.Reason, &st.CheckedAt)
	if err != nil {
		return nil, err
	}
	_ = s.Market.QueryRowContext(ctx, `SELECT last_id FROM ingest_cursors WHERE source = 'usage_logs'`).Scan(&st.IngestCursor)
	rows, err := s.Market.QueryContext(ctx, `SELECT status, count(*) FROM sub2api_ops WHERE status <> 'done' GROUP BY status`)
	if err != nil {
		return st, nil
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		st.OpsByStatus[status] = n
	}
	return st, rows.Err()
}

// Vendor 是厂商配置摘要（管理端表格用）。
type Vendor struct {
	Platform           string  `json:"platform"`
	Enabled            bool    `json:"enabled"`
	ConsignmentMin     float64 `json:"consignment_min"`
	ConsignmentMax     float64 `json:"consignment_max"`
	DefaultConcurrency int     `json:"default_concurrency"`
}

func (s *Store) ListVendors(ctx context.Context) ([]Vendor, error) {
	rows, err := s.Market.QueryContext(ctx, `
		SELECT platform, enabled, consignment_min, consignment_max, default_concurrency
		FROM vendor_configs ORDER BY platform`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Vendor
	for rows.Next() {
		var v Vendor
		if err := rows.Scan(&v.Platform, &v.Enabled, &v.ConsignmentMin, &v.ConsignmentMax, &v.DefaultConcurrency); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// FusedAccount 闲猫上游 × 引擎账号。
type FusedAccount struct {
	UpstreamID      int64    `json:"upstream_id,omitempty"`
	UpstreamCode    string   `json:"upstream_code,omitempty"`
	UpstreamName    string   `json:"upstream_name,omitempty"`
	AccountEmail    string   `json:"account_email,omitempty"`
	SellerID        int64    `json:"seller_id,omitempty"`
	SellerEmail     string   `json:"seller_email,omitempty"`
	Platform        string   `json:"platform"`
	MarketStatus    string   `json:"market_status"`
	Health          string   `json:"health,omitempty"`
	Grade           *string  `json:"grade,omitempty"`
	EngineAccountID *int64   `json:"engine_account_id,omitempty"`
	EngineName      string   `json:"engine_name,omitempty"`
	EngineStatus    string   `json:"engine_status,omitempty"`
	Schedulable     *bool    `json:"schedulable,omitempty"`
	RateMultiplier  *float64 `json:"rate_multiplier,omitempty"`
	Concurrency     *int     `json:"concurrency,omitempty"`
	Linked          bool     `json:"linked"`
	LinkState       string   `json:"link_state,omitempty"`
}

type engineAccount struct {
	ID             int64
	Name           string
	Platform       string
	Status         string
	Schedulable    bool
	RateMultiplier float64
	Concurrency    int
}

func (s *Store) ListSupplyAccounts(ctx context.Context) ([]FusedAccount, error) {
	rows, err := s.Market.QueryContext(ctx, `
		SELECT u.id, COALESCE(u.code, ''), COALESCE(u.name, ''), COALESCE(u.account_email, ''),
			u.seller_id, COALESCE(usr.email, ''), u.platform, u.status, u.health, u.grade, u.sub2api_account_id
		FROM upstreams u
		LEFT JOIN users usr ON usr.id = u.seller_id
		WHERE u.status <> 'removed' ORDER BY u.id DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type up struct {
		ID, SellerID                          int64
		Code, Name, AccountEmail, SellerEmail string
		Platform, Status, Health              string
		Grade                                 sql.NullString
		EngineID                              sql.NullInt64
	}
	var ups []up
	for rows.Next() {
		var u up
		if err := rows.Scan(&u.ID, &u.Code, &u.Name, &u.AccountEmail, &u.SellerID, &u.SellerEmail, &u.Platform, &u.Status, &u.Health, &u.Grade, &u.EngineID); err != nil {
			return nil, err
		}
		ups = append(ups, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	engines, err := s.listEngineAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]engineAccount, len(engines))
	for _, a := range engines {
		byID[a.ID] = a
	}
	used := map[int64]bool{}
	out := make([]FusedAccount, 0, len(ups)+len(engines))
	for _, u := range ups {
		row := FusedAccount{
			UpstreamID: u.ID, UpstreamCode: u.Code, UpstreamName: u.Name, AccountEmail: u.AccountEmail,
			SellerID: u.SellerID, SellerEmail: u.SellerEmail, Platform: u.Platform,
			MarketStatus: u.Status, Health: u.Health, LinkState: "unbound",
		}
		if u.Grade.Valid {
			g := u.Grade.String
			row.Grade = &g
		}
		if u.EngineID.Valid {
			id := u.EngineID.Int64
			row.EngineAccountID = &id
			if a, ok := byID[id]; ok {
				fillAccount(&row, a)
				used[id] = true
				row.Linked = true
				row.LinkState = "linked"
			} else {
				row.EngineStatus = "未同步"
				row.LinkState = "orphan"
			}
		}
		out = append(out, row)
	}
	for _, a := range engines {
		if used[a.ID] {
			continue
		}
		row := FusedAccount{Platform: a.Platform, MarketStatus: "仅引擎", Linked: false, LinkState: "engine_only"}
		fillAccount(&row, a)
		out = append(out, row)
	}
	return out, nil
}

func fillAccount(row *FusedAccount, a engineAccount) {
	id := a.ID
	sched := a.Schedulable
	rate := a.RateMultiplier
	conc := a.Concurrency
	row.EngineAccountID = &id
	row.EngineName = a.Name
	row.EngineStatus = a.Status
	row.Schedulable = &sched
	row.RateMultiplier = &rate
	row.Concurrency = &conc
}

func (s *Store) listEngineAccounts(ctx context.Context) ([]engineAccount, error) {
	if s.Engine == nil {
		return nil, nil
	}
	rows, err := s.Engine.QueryContext(ctx, `
		SELECT id, name, platform, status, schedulable, rate_multiplier, concurrency
		FROM accounts WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []engineAccount
	for rows.Next() {
		var a engineAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.Platform, &a.Status, &a.Schedulable, &a.RateMultiplier, &a.Concurrency); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FusedGroup 闲猫默认分组 × 引擎分组。
type FusedGroup struct {
	PoolID         int64    `json:"pool_id,omitempty"`
	Name           string   `json:"name"`
	Platform       string   `json:"platform"`
	Enabled        *bool    `json:"enabled,omitempty"`
	SellMultiplier *float64 `json:"sell_multiplier,omitempty"`
	EngineGroupID  *int64   `json:"engine_group_id,omitempty"`
	EngineName     string   `json:"engine_name,omitempty"`
	EngineRate     *float64 `json:"engine_rate,omitempty"`
	MarketStatus   string   `json:"market_status"`
	Linked         bool     `json:"linked"`
}

type engineGroup struct {
	ID             int64
	Name           string
	Platform       string
	RateMultiplier float64
}

func (s *Store) ListSupplyGroups(ctx context.Context) ([]FusedGroup, error) {
	rows, err := s.Market.QueryContext(ctx, `
		SELECT id, name, platform, enabled, sell_multiplier, sub2api_group_id
		FROM pools ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type pool struct {
		ID             int64
		Name, Platform string
		Enabled        bool
		Sell           float64
		EngineID       sql.NullInt64
	}
	var pools []pool
	for rows.Next() {
		var p pool
		if err := rows.Scan(&p.ID, &p.Name, &p.Platform, &p.Enabled, &p.Sell, &p.EngineID); err != nil {
			return nil, err
		}
		pools = append(pools, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	groups, err := s.listEngineGroups(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]engineGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	used := map[int64]bool{}
	out := make([]FusedGroup, 0, len(pools)+len(groups))
	for _, p := range pools {
		en, sell := p.Enabled, p.Sell
		row := FusedGroup{
			PoolID: p.ID, Name: p.Name, Platform: p.Platform,
			Enabled: &en, SellMultiplier: &sell, MarketStatus: "市场", Linked: true,
		}
		if p.EngineID.Valid {
			id := p.EngineID.Int64
			row.EngineGroupID = &id
			if g, ok := byID[id]; ok {
				row.EngineName = g.Name
				rate := g.RateMultiplier
				row.EngineRate = &rate
				used[id] = true
			} else {
				row.MarketStatus = "未同步"
			}
		}
		out = append(out, row)
	}
	for _, g := range groups {
		if used[g.ID] {
			continue
		}
		id := g.ID
		rate := g.RateMultiplier
		out = append(out, FusedGroup{
			Name: g.Name, Platform: g.Platform, EngineGroupID: &id,
			EngineName: g.Name, EngineRate: &rate, MarketStatus: "仅引擎", Linked: false,
		})
	}
	return out, nil
}

func (s *Store) listEngineGroups(ctx context.Context) ([]engineGroup, error) {
	if s.Engine == nil {
		return nil, nil
	}
	rows, err := s.Engine.QueryContext(ctx, `
		SELECT id, name, platform, rate_multiplier FROM groups WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []engineGroup
	for rows.Next() {
		var g engineGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Platform, &g.RateMultiplier); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
