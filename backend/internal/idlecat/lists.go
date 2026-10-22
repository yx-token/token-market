package idlecat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func scanJSONRows(rows *sql.Rows) ([]map[string]any, error) {
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := raw[i]
			switch t := v.(type) {
			case []byte:
				var j any
				if json.Unmarshal(t, &j) == nil {
					row[c] = j
				} else {
					row[c] = string(t)
				}
			case time.Time:
				row[c] = t.UTC().Format(time.RFC3339)
			default:
				row[c] = v
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) queryMaps(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, err := s.Market.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanJSONRows(rows)
}

func (s *Store) ListPools(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, name, platform, enabled, sell_multiplier, market_published, market_title, sub2api_group_id
		FROM pools ORDER BY id`)
}

func (s *Store) ListProxies(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, name, protocol, host, port, status, seller_id
		FROM proxies ORDER BY id DESC LIMIT 500`)
}

func (s *Store) ListUpstreams(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, seller_id, platform, auth_type, status, health, grade, sub2api_account_id, created_at
		FROM upstreams WHERE status <> 'removed' ORDER BY id DESC LIMIT 500`)
}

func (s *Store) ListRecharges(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, user_id, amount, status, provider, created_at
		FROM recharge_orders ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListRefunds(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, user_id, amount, status, created_at
		FROM refunds ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListWithdrawals(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, seller_id AS user_id, amount, status, created_at
		FROM withdrawals ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListAppeals(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, report_id, seller_id, status, created_at
		FROM appeals ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListPolicies(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT platform, auth_type, enabled
		FROM detection_policies ORDER BY platform, auth_type`)
}

func (s *Store) ListProviders(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT name, name AS label, enabled, base_url
		FROM detection_providers ORDER BY name`)
}

func (s *Store) ListProbeItems(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, suite, check_key, status, weight
		FROM probe_items ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListProbeRuns(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, upstream_id, status, queued_at AS created_at
		FROM probe_runs ORDER BY id DESC LIMIT 200`)
}

func (s *Store) ListCategories(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT id, parent_id, name, sort, enabled
		FROM market_categories ORDER BY sort, id`)
}

func (s *Store) QualityOverview(ctx context.Context) (map[string]any, error) {
	rows, err := s.queryMaps(ctx, `
		SELECT platform, health, COALESCE(grade, '-') AS grade, count(*)::int AS n
		FROM upstreams
		WHERE status IN ('ready', 'listed') AND NOT is_reference
		GROUP BY 1, 2, 3 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	appeals, err := s.queryMaps(ctx, `SELECT status, count(*)::int AS n FROM appeals GROUP BY status`)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"upstream_health":   rows,
		"appeals_by_status": appeals,
		"mode":              "native",
	}, nil
}

func (s *Store) PaymentStatus(ctx context.Context) (map[string]any, error) {
	rows, err := s.ListMerchants(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"merchants": rows, "mode": "native"}, nil
}

func (s *Store) ListMerchants(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT provider, enabled, updated_at
		FROM payment_merchants ORDER BY provider`)
}

func (s *Store) GetSettings(ctx context.Context) (map[string]any, error) {
	keys, err := s.loadSettingsKeys(ctx)
	if err != nil {
		return nil, err
	}
	return flattenAdminSettings(keys)
}

func (s *Store) PutSettings(ctx context.Context, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	platFields := platformFieldNames()
	platPatch := map[string]any{}
	other := map[string]any{}
	for k, v := range patch {
		if k == "platform" {
			m, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("settings platform must be an object")
			}
			for pk, pv := range m {
				platPatch[pk] = pv
			}
			continue
		}
		if _, ok := platFields[k]; ok {
			platPatch[k] = v
			continue
		}
		other[k] = v
	}

	tx, err := s.Market.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if len(platPatch) > 0 {
		var stored []byte
		err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'platform'`).Scan(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			stored = nil
		}
		patchRaw, err := json.Marshal(platPatch)
		if err != nil {
			return err
		}
		out, err := overlayPlatformJSON(stored, patchRaw)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at) VALUES ('platform', $1::jsonb, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, string(out)); err != nil {
			return fmt.Errorf("settings platform: %w", err)
		}
	}

	for k, v := range other {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at) VALUES ($1, $2::jsonb, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, k, string(b)); err != nil {
			return fmt.Errorf("settings %s: %w", k, err)
		}
	}
	return tx.Commit()
}

func (s *Store) loadSettingsKeys(ctx context.Context) (map[string]any, error) {
	rows, err := s.Market.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			out[key] = string(raw)
			continue
		}
		out[key] = v
	}
	return out, rows.Err()
}

func (s *Store) MarketDiscovery(ctx context.Context) (map[string]any, error) {
	raw, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if hs, ok := raw["market_discovery"].(map[string]any); ok {
		return hs, nil
	}
	out := map[string]any{"hot_searches": []any{}}
	if hs, ok := raw["hot_searches"]; ok {
		out["hot_searches"] = hs
	}
	return out, nil
}
