// Package market 提供买家浏览的市场数据：寄售挂单及其质量与可用率摘要。
package market

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/scheduling"
)

type Item struct {
	listing.InspectionView
	UpstreamID       int64     `json:"-"`
	SellerID         int64     `json:"seller_id"`
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
	Mode             string    `json:"mode"`
	SKUKind          string    `json:"sku_kind"`
	FaceValue        *float64  `json:"face_value,omitempty"`
	Stock            *int      `json:"stock,omitempty"`
	StockSold        int       `json:"stock_sold"`
	MinAmount        float64   `json:"min_amount"`
	Title            *string   `json:"title,omitempty"`
	Description      *string   `json:"description,omitempty"`
	Categories       []string  `json:"categories,omitempty"`
	Models           []string  `json:"models"` // 空数组表示存量上游未声明，实际不限制模型
}

type Filter struct {
	SellerID  int64
	BuyerID   int64 // 非零时只返回该买家商家清单内的挂单
	ListingID int64

	Platform string
	Grade    string
	MaxPrice float64
	Kind     string // channel / pack / 空=全部
	Category string // low_rate / coding / featured / pack
	Sort     string // price / success / ttft
	Model    string // 指定挂售模型；存量未声明白名单的上游不限制
}

type Service struct {
	DB         *pgxpool.Pool
	Commission func(ctx context.Context) (float64, error)
	Prices     *PriceBook
}

// List 返回生效中的寄售挂单（已排除降智、不可用、被冻结的上游），含 24h 成功率 / 首字延迟聚合。
func (s *Service) List(ctx context.Context, f Filter, limit, offset int) ([]Item, error) {
	return s.list(ctx, f, limit, offset, true)
}

func (s *Service) list(ctx context.Context, f Filter, limit, offset int, withMetrics bool) ([]Item, error) {
	c, err := s.Commission(ctx)
	if err != nil {
		return nil, err
	}
	order := "l.multiplier ASC"
	if withMetrics {
		switch f.Sort {
		case "success":
			order = "success_rate DESC NULLS LAST"
		case "ttft":
			order = "ttft_p50 ASC NULLS LAST"
		}
	}
	maxSeller := 1e9
	if f.MaxPrice > 0 {
		maxSeller = f.MaxPrice / (1 + c)
	}
	kind := f.Kind
	if kind != "pack" && kind != "channel" {
		kind = ""
	}
	var q string
	if withMetrics {
		q = `SELECT l.id, l.seller_id, u.id, u.platform, u.auth_type, u.tier, u.grade, u.health, l.multiplier::float8, l.created_at,
			att.success_rate, COALESCE(att.requests, 0), usage.ttft_p50,
			l.mode, l.sku_kind, l.face_value, l.stock, l.stock_sold, l.min_amount::float8, l.title, l.description,
			COALESCE(l.categories, '{}'), COALESCE(u.model_whitelist, '{}')
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
		WHERE ` + listing.MarketAvailableSQL + `
			AND ($1 = '' OR u.platform = $1) AND ($2 = '' OR u.grade = $2) AND l.multiplier <= $3
			AND ($6 = '' OR l.sku_kind = $6)
			AND ($7 = '' OR $7 = ANY (COALESCE(l.categories, '{}')))
			AND ($8 = '' OR u.model_whitelist IS NULL OR $8 = ANY(u.model_whitelist))
			AND ($9::bigint = 0 OR l.seller_id = $9)
			AND ($10::bigint = 0 OR EXISTS (SELECT 1 FROM buyer_merchants bm WHERE bm.user_id = $10 AND bm.seller_id = l.seller_id))
			AND ($11::bigint = 0 OR l.id = $11)
		ORDER BY ` + order + ` LIMIT $4 OFFSET $5`
	} else {
		q = `SELECT l.id, l.seller_id, u.id, u.platform, u.auth_type, u.tier, u.grade, u.health, l.multiplier::float8, l.created_at,
			NULL::float8, 0, NULL::float8,
			l.mode, l.sku_kind, l.face_value, l.stock, l.stock_sold, l.min_amount::float8, l.title, l.description,
			COALESCE(l.categories, '{}'), COALESCE(u.model_whitelist, '{}')
		FROM listings l
		JOIN upstreams u ON u.id = l.upstream_id
		WHERE ` + listing.MarketAvailableSQL + `
			AND ($1 = '' OR u.platform = $1) AND ($2 = '' OR u.grade = $2) AND l.multiplier <= $3
			AND ($6 = '' OR l.sku_kind = $6)
			AND ($7 = '' OR $7 = ANY (COALESCE(l.categories, '{}')))
			AND ($8 = '' OR u.model_whitelist IS NULL OR $8 = ANY(u.model_whitelist))
			AND ($9::bigint = 0 OR l.seller_id = $9)
			AND ($10::bigint = 0 OR EXISTS (SELECT 1 FROM buyer_merchants bm WHERE bm.user_id = $10 AND bm.seller_id = l.seller_id))
			AND ($11::bigint = 0 OR l.id = $11)
		ORDER BY ` + order + ` LIMIT $4 OFFSET $5`
	}
	rows, err := s.DB.Query(ctx, q, f.Platform, f.Grade, maxSeller, limit, offset, kind, f.Category, f.Model, f.SellerID, f.BuyerID, f.ListingID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		var it Item
		err := row.Scan(&it.ListingID, &it.SellerID, &it.UpstreamID, &it.Platform, &it.AuthType, &it.Tier, &it.Grade, &it.Health, &it.SellerMultiplier, &it.ListedSince,
			&it.SuccessRate24h, &it.Requests24h, &it.TTFTp50Ms,
			&it.Mode, &it.SKUKind, &it.FaceValue, &it.Stock, &it.StockSold, &it.MinAmount, &it.Title, &it.Description, &it.Categories, &it.Models)
		if it.Categories == nil {
			it.Categories = []string{}
		}
		it.BuyerPrice = listing.BuyerPrice(it.SellerMultiplier, c)
		return it, err
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].UpstreamID
	}
	views, err := listing.LoadInspectionViews(ctx, s.DB, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].InspectionView = views[items[i].UpstreamID]
	}
	return items, nil
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
	return s.List(ctx, Filter{ListingID: listingID}, 1, 0)
}

type PoolItem struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Platform          string  `json:"platform"`
	Price             float64 `json:"price"`
	MarketPublished   bool    `json:"market_published"`
	MarketTitle       string  `json:"market_title,omitempty"`
	MarketDescription string  `json:"market_description,omitempty"`
}

// Pools 返回可选的默认分组及售价（含市场推销字段；买家路由仍可用全部已同步分组）。
func (s *Service) Pools(ctx context.Context) ([]PoolItem, error) {
	pools, err := scheduling.ListPools(ctx, s.DB, true)
	if err != nil {
		return nil, err
	}
	out := make([]PoolItem, 0, len(pools))
	for _, p := range pools {
		if p.Sub2APIGroupID != nil {
			out = append(out, PoolItem{
				ID:                p.ID,
				Name:              p.Name,
				Platform:          p.Platform,
				Price:             p.SellMultiplier,
				MarketPublished:   p.MarketPublished,
				MarketTitle:       p.MarketTitle,
				MarketDescription: p.MarketDescription,
			})
		}
	}
	return out, nil
}
