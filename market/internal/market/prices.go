package market

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

// Price 是一个模型的官方价格（人民币 / 百万 token），寄售与回收倍率都相对它计算。
// 引擎加载 LiteLLM 美元价目时已统一按 1 USD = 7 CNY 换算，这里原样展示，不再换算。
type Price struct {
	Model           string  `json:"model"`
	Provider        string  `json:"provider,omitempty"`
	Mode            string  `json:"mode,omitempty"`
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m"`
}

// 短名单回退路径的默认缓存；渠道 catalog 路径每次现取，保证改渠道定价后模型价格页立刻生效。
const defaultShortlistTTL = 15 * time.Second

// PriceBook 读取市场「模型价格」官方参考价。
// 优先一次拉渠道定价 catalog（仅官方支持模型，不走长缓存）；
// 旧引擎或空结果时回退到 Models 短名单逐条查询（短 TTL）。
type PriceBook struct {
	Admin  *adminclient.Client
	Models func(ctx context.Context) ([]string, error)
	TTL    time.Duration

	mu    sync.Mutex
	at    time.Time
	items []Price
	// sourceCatalog 标记当前缓存是否来自渠道 catalog；catalog 命中不复用长缓存。
	sourceCatalog bool
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

// Invalidate 丢弃缓存，使下次 List 重新拉渠道定价。
func (b *PriceBook) Invalidate() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = nil
	b.at = time.Time{}
	b.sourceCatalog = false
}

// List 返回官方价目。渠道 catalog 每次现取；失败或空时回退到配置的短名单（短缓存）。
func (b *PriceBook) List(ctx context.Context) ([]Price, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.Admin != nil {
		if catalog, err := b.Admin.ListPricingCatalog(ctx); err == nil && len(catalog) > 0 {
			items := make([]Price, 0, len(catalog))
			for _, e := range catalog {
				if e.Model == "" {
					continue
				}
				items = append(items, priceFromCatalog(e))
			}
			b.items, b.at, b.sourceCatalog = items, time.Now(), true
			return items, nil
		}
	}

	ttl := b.TTL
	if ttl <= 0 {
		ttl = defaultShortlistTTL
	}
	if b.items != nil && !b.sourceCatalog && time.Since(b.at) < ttl {
		return b.items, nil
	}
	items, err := b.listFromModels(ctx)
	if err != nil {
		if b.items != nil {
			return b.items, nil
		}
		return nil, err
	}
	b.items, b.at, b.sourceCatalog = items, time.Now(), false
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
