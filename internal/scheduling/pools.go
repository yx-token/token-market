// Package scheduling 计算每个上游在 sub2api 中的期望状态（分组成员、优先级段、并发、倍率、限流参数），
// 并通过出站表下发，使 sub2api 的实际状态收敛到期望状态。
package scheduling

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/provision"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Pool struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Platform          string    `json:"platform"`
	Sub2APIGroupID    *int64    `json:"sub2api_group_id,omitempty"`
	SellMultiplier    float64   `json:"sell_multiplier"`
	MinMargin         float64   `json:"min_margin"`
	SafetyBuffer      float64   `json:"safety_buffer"`
	Enabled           bool      `json:"enabled"`
	MarketPublished   bool      `json:"market_published"`
	MarketTitle       string    `json:"market_title"`
	MarketDescription string    `json:"market_description"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Cap 返回进入该默认分组的倍率上限 D × (1 − 最低毛利 − 安全缓冲)，与 sub2api 利润控制同一公式。
func (p Pool) Cap() float64 {
	return p.SellMultiplier * (1 - p.MinMargin - p.SafetyBuffer)
}

// Accepts 判断某平台的上游能否进入该默认分组。
func (p Pool) Accepts(platform string) bool {
	return p.Enabled && (p.Platform == platform || p.Platform == "composite")
}

const poolColumns = `id, name, platform, sub2api_group_id, sell_multiplier, min_margin, safety_buffer, enabled, market_published, market_title, market_description, created_at, updated_at`

func scanPool(row pgx.Row) (*Pool, error) {
	var p Pool
	err := row.Scan(&p.ID, &p.Name, &p.Platform, &p.Sub2APIGroupID, &p.SellMultiplier, &p.MinMargin, &p.SafetyBuffer, &p.Enabled, &p.MarketPublished, &p.MarketTitle, &p.MarketDescription, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func ListPools(ctx context.Context, db DB, onlyEnabled bool) ([]Pool, error) {
	rows, err := db.Query(ctx, "SELECT "+poolColumns+" FROM pools WHERE NOT $1 OR enabled ORDER BY id", onlyEnabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pool
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func GetPool(ctx context.Context, db DB, id int64) (*Pool, error) {
	p, err := scanPool(db.QueryRow(ctx, "SELECT "+poolColumns+" FROM pools WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("POOL_NOT_FOUND", "pool not found")
	}
	return p, err
}

// GetPoolByName 按名称查找默认分组。
func GetPoolByName(ctx context.Context, db DB, name string) (*Pool, error) {
	p, err := scanPool(db.QueryRow(ctx, "SELECT "+poolColumns+" FROM pools WHERE name = $1", name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("POOL_NOT_FOUND", "pool not found")
	}
	return p, err
}

var poolPlatforms = map[string]bool{
	"anthropic": true, "openai": true, "gemini": true, "antigravity": true, "grok": true,
	"kimi": true, "zhipu": true, "minimax": true, "opencode_go": true, "composite": true,
}

// defaultMarketTitle 从上架时的分组名生成市场展示标题。
func defaultMarketTitle(name, platform string) string {
	title := strings.TrimPrefix(name, "tm-pool-")
	if title == "" || title == name {
		title = name
	}
	if platform == "composite" {
		return title + " · 平台保障池"
	}
	return title + " · 平台保障"
}

type PoolInput struct {
	Name              string   `json:"name"`
	Platform          string   `json:"platform"`
	SellMultiplier    *float64 `json:"sell_multiplier"`
	MinMargin         *float64 `json:"min_margin"`
	SafetyBuffer      *float64 `json:"safety_buffer"`
	Enabled           *bool    `json:"enabled"`
	MarketPublished   *bool    `json:"market_published"`
	MarketTitle       *string  `json:"market_title"`
	MarketDescription *string  `json:"market_description"`
}

type PoolService struct {
	DB   DB
	Prov *provision.Provisioner
}

// Create 创建默认分组，并在 sub2api 中创建开启利润控制的同名分组。
func (s *PoolService) Create(ctx context.Context, in PoolInput) (*Pool, error) {
	if !strings.HasPrefix(in.Name, "tm-pool-") {
		return nil, httpx.BadRequest("INVALID_NAME", "pool name must start with tm-pool-")
	}
	if !poolPlatforms[in.Platform] {
		return nil, httpx.BadRequest("INVALID_PLATFORM", "unsupported platform")
	}
	if in.SellMultiplier == nil || *in.SellMultiplier <= 0 {
		return nil, httpx.BadRequest("INVALID_PRICE", "sell_multiplier must be > 0")
	}
	margin, buffer := 0.10, 0.02
	if in.MinMargin != nil {
		margin = *in.MinMargin
	}
	if in.SafetyBuffer != nil {
		buffer = *in.SafetyBuffer
	}
	if margin < 0 || buffer < 0 || margin+buffer >= 1 {
		return nil, httpx.BadRequest("INVALID_MARGIN", "min_margin and safety_buffer must be >= 0 and sum to less than 1")
	}
	var id int64
	err := s.DB.QueryRow(ctx, `INSERT INTO pools (name, platform, sell_multiplier, min_margin, safety_buffer) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.Name, in.Platform, *in.SellMultiplier, margin, buffer).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, httpx.Conflict("POOL_EXISTS", "pool name already exists")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.ensureGroup(ctx, id); err != nil {
		return nil, fmt.Errorf("pool saved but sub2api group creation failed (retry with sync): %w", err)
	}
	return GetPool(ctx, s.DB, id)
}

// Sync 确保默认分组在 sub2api 中存在（创建失败后重试）。
func (s *PoolService) Sync(ctx context.Context, id int64) (*Pool, error) {
	if _, err := s.ensureGroup(ctx, id); err != nil {
		return nil, err
	}
	return GetPool(ctx, s.DB, id)
}

func (s *PoolService) ensureGroup(ctx context.Context, id int64) (int64, error) {
	p, err := GetPool(ctx, s.DB, id)
	if err != nil {
		return 0, err
	}
	if p.Sub2APIGroupID != nil {
		return *p.Sub2APIGroupID, nil
	}
	gid, err := s.Prov.EnsureGroup(ctx, provision.GroupSpec{
		Name:               p.Name,
		Platform:           p.Platform,
		RateMultiplier:     p.SellMultiplier,
		Exclusive:          true, // 只有被授权的影子用户才能绑定
		ProfitControl:      true,
		ProfitMinMargin:    p.MinMargin,
		ProfitSafetyBuffer: p.SafetyBuffer,
		Description:        "token-market default pool",
	})
	if err != nil {
		return 0, err
	}
	_, err = s.DB.Exec(ctx, "UPDATE pools SET sub2api_group_id = $2, updated_at = now() WHERE id = $1", id, gid)
	return gid, err
}

// Update 修改默认分组售价、毛利、启用状态，同步到 sub2api 分组。
func (s *PoolService) Update(ctx context.Context, id int64, in PoolInput) (*Pool, error) {
	p, err := GetPool(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if in.SellMultiplier != nil {
		if *in.SellMultiplier <= 0 {
			return nil, httpx.BadRequest("INVALID_PRICE", "sell_multiplier must be > 0")
		}
		p.SellMultiplier = *in.SellMultiplier
	}
	if in.MinMargin != nil {
		p.MinMargin = *in.MinMargin
	}
	if in.SafetyBuffer != nil {
		p.SafetyBuffer = *in.SafetyBuffer
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.MarketPublished != nil {
		p.MarketPublished = *in.MarketPublished
	}
	if in.MarketTitle != nil {
		p.MarketTitle = strings.TrimSpace(*in.MarketTitle)
	}
	if in.MarketDescription != nil {
		p.MarketDescription = strings.TrimSpace(*in.MarketDescription)
	}
	if !p.Enabled {
		p.MarketPublished = false
	}
	if p.MarketPublished {
		if p.Sub2APIGroupID == nil {
			return nil, httpx.BadRequest("POOL_NOT_SYNCED", "sync pool to sub2api before publishing to market")
		}
		if p.MarketTitle == "" {
			p.MarketTitle = defaultMarketTitle(p.Name, p.Platform)
		}
	}
	if p.MinMargin < 0 || p.SafetyBuffer < 0 || p.MinMargin+p.SafetyBuffer >= 1 {
		return nil, httpx.BadRequest("INVALID_MARGIN", "min_margin and safety_buffer must be >= 0 and sum to less than 1")
	}
	if _, err := s.DB.Exec(ctx, `UPDATE pools SET sell_multiplier = $2, min_margin = $3, safety_buffer = $4, enabled = $5, market_published = $6, market_title = $7, market_description = $8, updated_at = now() WHERE id = $1`,
		p.ID, p.SellMultiplier, p.MinMargin, p.SafetyBuffer, p.Enabled, p.MarketPublished, p.MarketTitle, p.MarketDescription); err != nil {
		return nil, err
	}
	if p.Sub2APIGroupID != nil {
		rate, margin, buffer, on := p.SellMultiplier, p.MinMargin, p.SafetyBuffer, true
		if _, err := ops.Enqueue(ctx, s.DB, ops.NewOp{
			Type:   opshandlers.TypeGroupUpdate,
			Target: fmt.Sprintf("group:%d", *p.Sub2APIGroupID),
			Payload: opshandlers.GroupUpdatePayload{GroupID: *p.Sub2APIGroupID, Input: adminclient.UpdateGroupInput{
				RateMultiplier: &rate, ProfitControlEnabled: &on, ProfitMinMargin: &margin, ProfitSafetyBuffer: &buffer,
			}},
			IdempotencyKey: fmt.Sprintf("pool-update-%d-%d", p.ID, time.Now().UnixNano()),
		}); err != nil {
			return nil, err
		}
	}
	return GetPool(ctx, s.DB, id)
}
