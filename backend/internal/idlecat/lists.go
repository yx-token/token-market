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
		SELECT p.id, p.name, p.protocol, p.host, p.port, p.status, p.seller_id,
			COALESCE(u.email, '') AS seller_email, p.username, p.region, p.sub2api_proxy_id,
			p.created_at, p.connection_report, p.quality_report, p.connection_checked_at, p.quality_checked_at,
			assigned.ids AS assigned_upstream_ids, assigned.count AS assigned_upstream_count,
			assigned.first_id AS assigned_upstream_id
		FROM proxies p
		LEFT JOIN users u ON u.id = p.seller_id
		CROSS JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(up.id ORDER BY up.id), '[]'::jsonb) AS ids,
				count(*)::int AS count, min(up.id) AS first_id
			FROM upstreams up WHERE up.proxy_id = p.id AND up.status <> 'removed'
		) assigned
		ORDER BY p.id DESC LIMIT 500`)
}

func (s *Store) ListUpstreams(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT up.id, up.seller_id, COALESCE(u.email, '') AS seller_email, up.platform, up.auth_type,
			up.status, up.health, up.grade, up.sub2api_account_id, up.created_at,
			up.account_email, up.name, up.note, up.code
		FROM upstreams up
		LEFT JOIN users u ON u.id = up.seller_id
		WHERE up.status <> 'removed' ORDER BY up.id DESC LIMIT 500`)
}

func (s *Store) ListRecharges(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT r.id, r.user_id, COALESCE(u.email, '') AS user_email, r.amount, r.status, r.provider, r.channel, r.created_at
		FROM recharge_orders r
		LEFT JOIN users u ON u.id = r.user_id
		ORDER BY r.id DESC LIMIT 200`)
}

func (s *Store) ListRefunds(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT r.id, r.user_id, COALESCE(u.email, '') AS user_email, r.amount, r.status, r.created_at
		FROM refunds r
		LEFT JOIN users u ON u.id = r.user_id
		ORDER BY r.id DESC LIMIT 200`)
}

func (s *Store) ListWithdrawals(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT w.id, w.seller_id, COALESCE(u.email, '') AS seller_email, w.amount, w.status, w.channel, w.created_at
		FROM withdrawals w
		LEFT JOIN users u ON u.id = w.seller_id
		ORDER BY w.id DESC LIMIT 200`)
}

func (s *Store) ListAppeals(ctx context.Context) ([]map[string]any, error) {
	return s.queryMaps(ctx, `
		SELECT a.id, a.report_id, a.seller_id, COALESCE(u.email, '') AS seller_email, a.status, a.created_at
		FROM appeals a
		LEFT JOIN users u ON u.id = a.seller_id
		ORDER BY a.id DESC LIMIT 200`)
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
