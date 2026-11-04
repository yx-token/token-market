// Package scheduling 计算每个上游在 sub2api 中的期望状态（分组成员、优先级段、并发、倍率、限流参数），
// 并通过出站表下发，使 sub2api 的实际状态收敛到期望状态。
package scheduling

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
	"github.com/yx-token/token-market/market/internal/sub2api/provision"
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

// ListPools 列出未删除的默认分组；onlyEnabled 时只返回启用的。
func ListPools(ctx context.Context, db DB, onlyEnabled bool) ([]Pool, error) {
	rows, err := db.Query(ctx, "SELECT "+poolColumns+" FROM pools WHERE deleted_at IS NULL AND (NOT $1 OR enabled) ORDER BY id", onlyEnabled)
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

// GetPool 按 ID 查找未删除的默认分组。
func GetPool(ctx context.Context, db DB, id int64) (*Pool, error) {
	p, err := scanPool(db.QueryRow(ctx, "SELECT "+poolColumns+" FROM pools WHERE id = $1 AND deleted_at IS NULL", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("POOL_NOT_FOUND", "pool not found")
	}
	return p, err
}

// GetPoolByName 按名称查找未删除的默认分组（已删除的同名分组可能有多条）。
func GetPoolByName(ctx context.Context, db DB, name string) (*Pool, error) {
	p, err := scanPool(db.QueryRow(ctx, "SELECT "+poolColumns+" FROM pools WHERE name = $1 AND deleted_at IS NULL", name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("POOL_NOT_FOUND", "pool not found")
	}
	return p, err
}

var poolPlatforms = map[string]bool{
	"anthropic": true, "openai": true, "gemini": true, "antigravity": true, "grok": true,
	"kimi": true, "zhipu": true, "minimax": true, "opencode_go": true, "composite": true,
}

func engineProfitControlSupported(platform string) bool {
	switch platform {
	case "anthropic", "openai", "gemini", "antigravity", "grok":
		return true
	default:
		return false
	}
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

type groupProvisioner interface {
	EnsureGroup(ctx context.Context, spec provision.GroupSpec) (int64, error)
}

type PoolService struct {
	DB   DB
	Prov groupProvisioner
}

// Create 创建默认分组，并按平台支持情况同步 sub2api 利润控制配置。
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
	// 同名分组刚删除时，sub2api 里的旧分组要等出站表执行完才消失；这期间按名称会找回旧分组。
	var deleting bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pools p WHERE p.name = $1 AND p.deleted_at IS NOT NULL AND p.sub2api_group_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM sub2api_ops o WHERE o.idempotency_key = 'pool-delete-' || p.id AND o.status = 'done'))`, in.Name).Scan(&deleting); err != nil {
		return nil, err
	}
	if deleting {
		return nil, httpx.Conflict("POOL_GROUP_DELETING", "a deleted pool with this name is still being removed from sub2api, retry shortly")
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
		ProfitControl:      engineProfitControlSupported(p.Platform),
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

// Update 修改默认分组名称、售价、毛利、启用状态，同步到 sub2api 分组。
func (s *PoolService) Update(ctx context.Context, id int64, in PoolInput) (*Pool, error) {
	p, err := GetPool(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	nameChanged := false
	if name := strings.TrimSpace(in.Name); name != "" && name != p.Name {
		if !strings.HasPrefix(name, "tm-pool-") {
			return nil, httpx.BadRequest("INVALID_NAME", "pool name must start with tm-pool-")
		}
		// 新名若正被软删池占用且引擎分组未清完，按名称建/改会撞上旧组。
		var deleting bool
		if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pools p WHERE p.name = $1 AND p.deleted_at IS NOT NULL AND p.sub2api_group_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM sub2api_ops o WHERE o.idempotency_key = 'pool-delete-' || p.id AND o.status = 'done'))`, name).Scan(&deleting); err != nil {
			return nil, err
		}
		if deleting {
			return nil, httpx.Conflict("POOL_GROUP_DELETING", "a deleted pool with this name is still being removed from sub2api, retry shortly")
		}
		p.Name = name
		nameChanged = true
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
	_, err = s.DB.Exec(ctx, `UPDATE pools SET name = $2, sell_multiplier = $3, min_margin = $4, safety_buffer = $5, enabled = $6, market_published = $7, market_title = $8, market_description = $9, updated_at = now() WHERE id = $1`,
		p.ID, p.Name, p.SellMultiplier, p.MinMargin, p.SafetyBuffer, p.Enabled, p.MarketPublished, p.MarketTitle, p.MarketDescription)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, httpx.Conflict("POOL_EXISTS", "pool name already exists")
	}
	if err != nil {
		return nil, err
	}
	if p.Sub2APIGroupID != nil {
		rate, margin, buffer, on := p.SellMultiplier, p.MinMargin, p.SafetyBuffer, engineProfitControlSupported(p.Platform)
		inGroup := adminclient.UpdateGroupInput{
			RateMultiplier: &rate, ProfitControlEnabled: &on, ProfitMinMargin: &margin, ProfitSafetyBuffer: &buffer,
		}
		if nameChanged {
			inGroup.Name = p.Name
		}
		if _, err := ops.Enqueue(ctx, s.DB, ops.NewOp{
			Type:           opshandlers.TypeGroupUpdate,
			Target:         fmt.Sprintf("group:%d", *p.Sub2APIGroupID),
			Payload:        opshandlers.GroupUpdatePayload{GroupID: *p.Sub2APIGroupID, Input: inGroup},
			IdempotencyKey: fmt.Sprintf("pool-update-%d-%d", p.ID, time.Now().UnixNano()),
		}); err != nil {
			return nil, err
		}
	}
	return GetPool(ctx, s.DB, id)
}

// Delete 删除已关闭的默认分组。
//
// 本地记录只做软删除：晚到的用量仍要按 sub2api_group_id 归为 pool 渠道计费。
// sub2api 中的分组经出站表删除（引擎会级联解绑账号与授权）；买家 Key 指向它的一跳随之失效，
// 遗留的内部 key 由 CleanupDeletedKeys 回收。要求先关闭，是为了让调度器先把账号移出该分组。
func (s *PoolService) Delete(ctx context.Context, id int64) (*Pool, error) {
	p, err := GetPool(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if p.Enabled {
		return nil, httpx.Conflict("POOL_ENABLED", "disable the pool before deleting it")
	}
	tag, err := s.DB.Exec(ctx, `UPDATE pools SET deleted_at = now(), market_published = false, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND NOT enabled`, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// 读取之后被并发删除或重新开启
		return nil, httpx.Conflict("POOL_CHANGED", "pool was changed concurrently, reload and retry")
	}
	if p.Sub2APIGroupID != nil {
		if _, err := ops.Enqueue(ctx, s.DB, ops.NewOp{
			Type:           opshandlers.TypeGroupDelete,
			Target:         fmt.Sprintf("group:%d", *p.Sub2APIGroupID),
			Payload:        opshandlers.GroupDeletePayload{GroupID: *p.Sub2APIGroupID},
			IdempotencyKey: fmt.Sprintf("pool-delete-%d", p.ID),
		}); err != nil {
			return nil, fmt.Errorf("pool deleted but sub2api group deletion was not queued: %w", err)
		}
	}
	p.Enabled, p.MarketPublished = false, false
	return p, nil
}
