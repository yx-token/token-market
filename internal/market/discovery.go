package market

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const discoveryKey = "market_discovery"

// DiscoveryConfig 市场运营发现配置（热搜 / 货架开关等）。结构与前端 market/config.ts 对齐。
type DiscoveryConfig map[string]any

type discoveryDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// LoadDiscovery 读取运营配置；未设置时返回空对象（前端用本地默认值合并）。
func LoadDiscovery(ctx context.Context, db discoveryDB) (DiscoveryConfig, error) {
	var raw []byte
	err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, discoveryKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DiscoveryConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out DiscoveryConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = DiscoveryConfig{}
	}
	return out, nil
}

// SaveDiscovery 整份覆盖保存运营配置。
func SaveDiscovery(ctx context.Context, db discoveryDB, cfg DiscoveryConfig) (DiscoveryConfig, error) {
	if cfg == nil {
		cfg = DiscoveryConfig{}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var check any
	if err := json.Unmarshal(raw, &check); err != nil {
		return nil, httpx.BadRequest("INVALID_DISCOVERY", "discovery config must be valid JSON object")
	}
	_, err = db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, discoveryKey, raw)
	if err != nil {
		return nil, err
	}
	return LoadDiscovery(ctx, db)
}
