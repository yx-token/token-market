package market

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/sub2api/adminclient"
)

// Price 是一个模型的官方价格（美元 / 百万 token），寄售与回收倍率都相对它计算。
type Price struct {
	Model           string  `json:"model"`
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m"`
}

// PriceBook 从 sub2api 内置价目表读取官方价格并缓存，避免本系统另外维护一份价目。
type PriceBook struct {
	Admin  *adminclient.Client
	Models func(ctx context.Context) ([]string, error)
	TTL    time.Duration

	mu    sync.Mutex
	at    time.Time
	items []Price
}

func per1M(perToken float64) float64 { return math.Round(perToken*1e6*1e6) / 1e6 }

// List 返回配置的模型中 sub2api 价目表里能查到的价格。查询失败时沿用上次的缓存。
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
	models, err := b.Models(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Price, 0, len(models))
	for _, m := range models {
		p, err := b.Admin.GetModelPricing(ctx, m)
		if err != nil {
			if b.items != nil {
				return b.items, nil
			}
			return nil, err
		}
		if !p.Found {
			continue
		}
		items = append(items, Price{Model: m, InputPer1M: per1M(p.InputPrice), OutputPer1M: per1M(p.OutputPrice),
			CacheWritePer1M: per1M(p.CacheWritePrice), CacheReadPer1M: per1M(p.CacheReadPrice)})
	}
	b.items, b.at = items, time.Now()
	return items, nil
}
