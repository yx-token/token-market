package market

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

// Price 是一个模型的官方价格（美元 / 百万 token），寄售与回收倍率都相对它计算。
type Price struct {
	Model           string  `json:"model"`
	Provider        string  `json:"provider,omitempty"`
	Mode            string  `json:"mode,omitempty"`
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m"`
}

// PriceBook 从 sub2api 内置价目表读取官方价格并缓存，避免本系统另外维护一份价目。
// 优先一次拉全量 catalog；旧引擎没有 catalog 时回退到 Models 短名单逐条查询。
type PriceBook struct {
	Admin  *adminclient.Client
	Models func(ctx context.Context) ([]string, error)
	TTL    time.Duration

	mu    sync.Mutex
	at    time.Time
	items []Price
}

func per1M(perToken float64) float64 { return math.Round(perToken*1e6*1e6) / 1e6 }

func priceFromCatalog(e adminclient.PricingCatalogEntry) Price {
	return Price{
		Model:           e.Model,
		Provider:        e.Provider,
		Mode:            e.Mode,
		InputPer1M:      per1M(e.InputPrice),
		OutputPer1M:     per1M(e.OutputPrice),
		CacheWritePer1M: per1M(e.CacheWritePrice),
		CacheReadPer1M:  per1M(e.CacheReadPrice),
	}
}

// List 返回官方价目。优先全量 catalog；失败或空时回退到配置的短名单。
func (b *PriceBook) List(ctx context.Context) ([]Price, error) {
	ttl := b.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.items != nil && time.Since(b.at) < ttl {
		return b.items, nil
	}
	if b.Admin != nil {
		if catalog, err := b.Admin.ListPricingCatalog(ctx); err == nil && len(catalog) > 0 {
			items := make([]Price, 0, len(catalog))
			for _, e := range catalog {
				if e.Model == "" {
					continue
				}
				items = append(items, priceFromCatalog(e))
			}
			b.items, b.at = items, time.Now()
			return items, nil
		}
	}
	items, err := b.listFromModels(ctx)
	if err != nil {
		if b.items != nil {
			return b.items, nil
		}
		return nil, err
	}
	b.items, b.at = items, time.Now()
	return items, nil
}

func (b *PriceBook) listFromModels(ctx context.Context) ([]Price, error) {
	if b.Models == nil || b.Admin == nil {
		return []Price{}, nil
	}
	models, err := b.Models(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Price, 0, len(models))
	for _, m := range models {
		p, err := b.Admin.GetModelPricing(ctx, m)
		if err != nil {
			return nil, err
		}
		if !p.Found {
			continue
		}
		items = append(items, Price{Model: m, InputPer1M: per1M(p.InputPrice), OutputPer1M: per1M(p.OutputPrice),
			CacheWritePer1M: per1M(p.CacheWritePrice), CacheReadPer1M: per1M(p.CacheReadPrice)})
	}
	return items, nil
}
