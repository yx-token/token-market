// Package market 提供买家浏览的市场数据：寄售挂单及其质量与可用率摘要。
package market

import (
	"context"
	"time"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Item struct {
	ListingID        int64     `json:"listing_id"`
	Platform         string    `json:"platform"`
	AuthType         string    `json:"auth_type"`
	Tier             *string   `json:"tier,omitempty"`
	Grade            *string   `json:"grade,omitempty"`
	Health           string    `json:"health"`
	SellerMultiplier float64   `json:"-"`
	BuyerPrice       float64   `json:"price"`            // 买家价倍率 p = m_s × (1 + c)
	SuccessRate24h   *float64  `json:"success_rate_24h"` // Edge 尝试成功率
	Requests24h      int       `json:"requests_24h"`
	TTFTp50Ms        *float64  `json:"ttft_p50_ms"`
	ListedSince      time.Time `json:"listed_since"`
}

type Filter struct {
	Platform string
	Grade    string
	MaxPrice float64
	Sort     string // price / success / ttft
}

type Service struct {
	DB         *pgxpool.Pool
	Commission func(ctx context.Context) (float64, error)
	Prices     *PriceBook
}

// List 返回生效中的寄售挂单（已排除降智、不可用、被冻结的上游）。
func (s *Service) List(ctx context.Context, f Filter, limit, offset int) ([]Item, error) {
	c, err := s.Commission(ctx)
	if err != nil {
		return nil, err
	}
	order := "l.multiplier ASC"
	switch f.Sort {
	case "success":
		order = "success_rate DESC NULLS LAST"
	case "ttft":
		order = "ttft_p50 ASC NULLS LAST"
	}
	maxSeller := 1e9
	if f.MaxPrice > 0 {
		maxSeller = f.MaxPrice / (1 + c)
	}
	rows, err := s.DB.Query(ctx, `SELECT l.id, u.platform, u.auth_type, u.tier, u.grade, u.health, l.multiplier::float8, l.created_at,
			att.success_rate, COALESCE(att.requests, 0), usage.ttft_p50
		FROM listings l
		JOIN upstreams u ON u.id = l.upstream_id
		LEFT JOIN LATERAL (
			SELECT count(*) FILTER (WHERE end_reason <> 'skipped') AS requests,
				(count(*) FILTER (WHERE end_reason IN ('completed', 'client_cancel')))::float8
					/ NULLIF(count(*) FILTER (WHERE end_reason <> 'skipped'), 0) AS success_rate
			FROM edge_attempts WHERE listing_id = l.id AND started_at > now() - interval '24 hours') att ON true
		LEFT JOIN LATERAL (
			SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms) AS ttft_p50
			FROM usage_records WHERE upstream_id = u.id AND created_at > now() - interval '24 hours' AND first_token_ms IS NOT NULL) usage ON true
		WHERE l.mode = 'consignment' AND l.status = 'active' AND u.status = 'listed' AND u.health NOT IN ('degraded', 'unavailable')
			AND NOT EXISTS (SELECT 1 FROM upstream_runtime_params rp WHERE rp.upstream_id = u.id AND NOT rp.buyer_facing)
			AND ($1 = '' OR u.platform = $1) AND ($2 = '' OR u.grade = $2) AND l.multiplier <= $3
		ORDER BY `+order+` LIMIT $4 OFFSET $5`, f.Platform, f.Grade, maxSeller, limit, offset)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		var it Item
		err := row.Scan(&it.ListingID, &it.Platform, &it.AuthType, &it.Tier, &it.Grade, &it.Health, &it.SellerMultiplier, &it.ListedSince,
			&it.SuccessRate24h, &it.Requests24h, &it.TTFTp50Ms)
		it.BuyerPrice = listing.BuyerPrice(it.SellerMultiplier, c)
		return it, err
	})
	return items, err
}

// Get 返回单个挂单摘要。
func (s *Service) Get(ctx context.Context, listingID int64) (*Item, error) {
	items, err := s.listOne(ctx, listingID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, httpx.NotFound("LISTING_NOT_FOUND", "listing not found or not available")
	}
	return &items[0], nil
}

func (s *Service) listOne(ctx context.Context, listingID int64) ([]Item, error) {
	all, err := s.List(ctx, Filter{}, 1000, 0)
	if err != nil {
		return nil, err
	}
	for _, it := range all {
		if it.ListingID == listingID {
			return []Item{it}, nil
		}
	}
	return nil, nil
}

type PoolItem struct {
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Price    float64 `json:"price"`
}

// Pools 返回可选的默认分组及售价。
func (s *Service) Pools(ctx context.Context) ([]PoolItem, error) {
	pools, err := scheduling.ListPools(ctx, s.DB, true)
	if err != nil {
		return nil, err
	}
	out := make([]PoolItem, 0, len(pools))
	for _, p := range pools {
		if p.Sub2APIGroupID != nil {
			out = append(out, PoolItem{Name: p.Name, Platform: p.Platform, Price: p.SellMultiplier})
		}
	}
	return out, nil
}
