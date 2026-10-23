package idlecat

import (
	"context"
	"database/sql"
	"encoding/json"
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
