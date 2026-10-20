package idlecat

import (
	"context"
	"database/sql"
	"fmt"
)

func rowsChanged(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// SetVendorEnabled 只改厂商开关，其余配置保持原值。
func (s *Store) SetVendorEnabled(ctx context.Context, platform string, enabled bool) error {
	res, err := s.Market.ExecContext(ctx, `
		UPDATE vendor_configs SET enabled = $2, updated_at = now() WHERE platform = $1`, platform, enabled)
	if err != nil {
		return err
	}
	return rowsChanged(res)
}

// SetProxyDisabled 停用或启用尚未分配给上游的代理。
func (s *Store) SetProxyDisabled(ctx context.Context, id int64, disabled bool) error {
	status := "available"
	if disabled {
		status = "disabled"
	}
	res, err := s.Market.ExecContext(ctx, `
		UPDATE proxies SET status = $2 WHERE id = $1 AND assigned_upstream_id IS NULL`, id, status)
	if err != nil {
		return err
	}
	return rowsChanged(res)
}

// SetPoolEnabled 开关默认分组。关闭时同时下架市场发布。
func (s *Store) SetPoolEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := s.Market.ExecContext(ctx, `
		UPDATE pools
		SET enabled = $2,
		    market_published = CASE WHEN $2 THEN market_published ELSE false END,
		    updated_at = now()
		WHERE id = $1`, id, enabled)
	if err != nil {
		return err
	}
	return rowsChanged(res)
}

// SetPolicyEnabled 开关某厂商、某接入方式的检测策略。
func (s *Store) SetPolicyEnabled(ctx context.Context, platform, authType string, enabled bool) error {
	res, err := s.Market.ExecContext(ctx, `
		UPDATE detection_policies SET enabled = $3, version = version + 1, updated_at = now()
		WHERE platform = $1 AND auth_type = $2`, platform, authType, enabled)
	if err != nil {
		return err
	}
	return rowsChanged(res)
}

// RetireProbeItem 将未下线的题目标为 retired。
func (s *Store) RetireProbeItem(ctx context.Context, id int64) error {
	res, err := s.Market.ExecContext(ctx, `
		UPDATE probe_items SET status = 'retired', updated_at = now()
		WHERE id = $1 AND status <> 'retired'`, id)
	if err != nil {
		return err
	}
	return rowsChanged(res)
}

// PutDiscovery 整包写回市场发现配置（含热搜开关）。
func (s *Store) PutDiscovery(ctx context.Context, body map[string]any) error {
	return s.PutSettings(ctx, map[string]any{"market_discovery": body})
}
