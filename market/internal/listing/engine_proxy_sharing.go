package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

type EngineProxyConnectionReport struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Region    string `json:"region,omitempty"`
	IP        string `json:"ip,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

type EngineProxySharing struct {
	ProxyID             int64                        `json:"proxy_id"`
	IsShared            bool                         `json:"is_shared"`
	ConnectionReport    *EngineProxyConnectionReport `json:"connection_report"`
	ConnectionCheckedAt *time.Time                   `json:"connection_checked_at"`
}

func (s *ProxyService) ListEngineProxySharing(ctx context.Context) ([]EngineProxySharing, error) {
	rows, err := s.DB.Query(ctx, `SELECT proxy_id,is_shared,connection_report,connection_checked_at FROM engine_proxy_sharing ORDER BY proxy_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EngineProxySharing, 0)
	for rows.Next() {
		var item EngineProxySharing
		var raw []byte
		if err := rows.Scan(&item.ProxyID, &item.IsShared, &raw, &item.ConnectionCheckedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			var report EngineProxyConnectionReport
			if err := json.Unmarshal(raw, &report); err != nil {
				return nil, err
			}
			item.ConnectionReport = &report
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *ProxyService) engineProxySharing(ctx context.Context, id int64) (*EngineProxySharing, error) {
	var out EngineProxySharing
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT proxy_id,is_shared,connection_report,connection_checked_at FROM engine_proxy_sharing WHERE proxy_id=$1`, id).Scan(&out.ProxyID, &out.IsShared, &raw, &out.ConnectionCheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "engine proxy sharing record not found")
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		var report EngineProxyConnectionReport
		if err := json.Unmarshal(raw, &report); err != nil {
			return nil, err
		}
		out.ConnectionReport = &report
	}
	return &out, nil
}

func (s *ProxyService) ensureEngineProxyMapping(ctx context.Context, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, sharedProxyRotationLock); err != nil {
		return err
	}
	var marketID int64
	err = tx.QueryRow(ctx, `SELECT market_proxy_id FROM engine_proxy_sharing WHERE proxy_id=$1 FOR UPDATE`, id).Scan(&marketID)
	if err == nil {
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var sellerID *int64
	err = tx.QueryRow(ctx, `SELECT id,seller_id FROM proxies WHERE sub2api_proxy_id=$1`, id).Scan(&marketID, &sellerID)
	if err == nil && sellerID != nil {
		return httpx.Conflict("PROXY_MAPPING_CONFLICT", "engine proxy is already registered as a seller proxy")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO proxies(name,protocol,host,port,status,sub2api_proxy_id) VALUES($1,'http',$2,1,'available',$3) RETURNING id`, fmt.Sprintf("engine-proxy-%d", id), fmt.Sprintf("engine-proxy-%d.invalid", id), id).Scan(&marketID)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO engine_proxy_sharing(proxy_id,market_proxy_id,is_shared) VALUES($1,$2,false)`, id, marketID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ProxyService) SetEngineProxyShared(ctx context.Context, id int64, shared bool) (*EngineProxySharing, error) {
	if id <= 0 {
		return nil, httpx.BadRequest("INVALID_PROXY", "proxy_id must be positive")
	}
	if shared {
		probeStarted := time.Now()
		p, err := s.liveEngineProxy(ctx, id)
		if err != nil {
			return nil, httpx.NotFound("PROXY_NOT_FOUND", "engine proxy not found")
		}
		if !engineProxyActive(p, time.Now()) {
			return nil, httpx.Conflict("PROXY_NOT_AVAILABLE", "only active, unexpired engine proxies can be shared")
		}
		result, probeErr := s.Admin.TestProxy(ctx, id)
		if probeErr != nil {
			result = &adminclient.ProxyTestResult{Success: false}
		}
		if err = s.ensureEngineProxyMapping(ctx, id); err != nil {
			return nil, err
		}
		if err = s.saveEngineProxyTest(ctx, id, result, p, probeStarted); err != nil {
			return nil, err
		}
		if probeErr != nil {
			return nil, httpx.Unavailable("PROXY_CHECK_FAILED", "proxy connectivity test failed; retry")
		}
		if !result.Success {
			return nil, httpx.Conflict("PROXY_NOT_CONNECTED", "proxy connectivity test did not succeed")
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, sharedProxyRotationLock); err != nil {
		return nil, err
	}
	var marketID int64
	if err = tx.QueryRow(ctx, `SELECT market_proxy_id FROM engine_proxy_sharing WHERE proxy_id=$1 FOR UPDATE`, id).Scan(&marketID); errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "engine proxy sharing record not found")
	}
	if err != nil {
		return nil, err
	}
	if !shared {
		var active int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM upstreams WHERE proxy_id=$1 AND status<>'removed'`, marketID).Scan(&active); err != nil {
			return nil, err
		}
		if active > 0 {
			return nil, httpx.Conflict("PROXY_SHARED_IN_USE", "remove active market upstream bindings before disabling engine proxy sharing")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE engine_proxy_sharing SET is_shared=$2,updated_at=now() WHERE proxy_id=$1`, id, shared); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.engineProxySharing(ctx, id)
}

func engineProxyActive(p *adminclient.Proxy, now time.Time) bool {
	return p != nil && p.Status == "active" && (p.ExpiresAt == nil || p.ExpiresAt.After(now))
}

func (s *ProxyService) liveEngineProxy(ctx context.Context, id int64) (*adminclient.Proxy, error) {
	if s.EngineProxyState != nil {
		var p adminclient.Proxy
		err := s.EngineProxyState.QueryRow(ctx, `SELECT id,name,protocol,host,port,status,expires_at,updated_at FROM proxies WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Status, &p.ExpiresAt, &p.UpdatedAt)
		if err == nil {
			return &p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "engine proxy not found")
	}
	return s.Admin.GetProxy(ctx, id)
}

func (s *ProxyService) TestEngineProxy(ctx context.Context, id int64) (*EngineProxySharing, error) {
	if id <= 0 {
		return nil, httpx.BadRequest("INVALID_PROXY", "proxy_id must be positive")
	}
	probeStarted := time.Now()
	p, err := s.liveEngineProxy(ctx, id)
	if err != nil {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "engine proxy not found")
	}
	var result *adminclient.ProxyTestResult
	var probeErr error
	if engineProxyActive(p, time.Now()) {
		result, probeErr = s.Admin.TestProxy(ctx, id)
		if probeErr != nil {
			result = &adminclient.ProxyTestResult{Success: false}
		}
	} else {
		result = &adminclient.ProxyTestResult{Success: false, Message: "proxy is inactive or expired"}
	}
	if err = s.ensureEngineProxyMapping(ctx, id); err != nil {
		var conflict *httpx.Error
		if errors.As(err, &conflict) && conflict.Code == "PROXY_MAPPING_CONFLICT" {
			if probeErr != nil {
				return nil, httpx.Unavailable("PROXY_CHECK_FAILED", "proxy connectivity test failed; retry")
			}
			checked := time.Now()
			message := "proxy connectivity test failed"
			if result.Success {
				message = "proxy connectivity test succeeded"
			}
			return &EngineProxySharing{ProxyID: id, IsShared: false, ConnectionReport: &EngineProxyConnectionReport{Success: result.Success, Message: message, Region: result.CountryCode, IP: result.IPAddress, LatencyMS: result.LatencyMs}, ConnectionCheckedAt: &checked}, nil
		}
		return nil, err
	}
	if err = s.saveEngineProxyTest(ctx, id, result, p, probeStarted); err != nil {
		return nil, err
	}
	if probeErr != nil {
		return nil, httpx.Unavailable("PROXY_CHECK_FAILED", "proxy connectivity test failed; retry")
	}
	return s.engineProxySharing(ctx, id)
}

func (s *ProxyService) saveEngineProxyTest(ctx context.Context, id int64, result *adminclient.ProxyTestResult, state *adminclient.Proxy, probeStarted time.Time) error {
	region := result.CountryCode
	if region == "" {
		region = result.Region
	}
	if region == "" {
		region = result.Country
	}
	var engineUpdatedAt any
	if state != nil && !state.UpdatedAt.IsZero() {
		engineUpdatedAt = state.UpdatedAt
	}
	if state != nil && result.Success {
		current, readErr := s.liveEngineProxy(ctx, id)
		if readErr != nil || !engineProxyActive(current, time.Now()) || !current.UpdatedAt.Equal(state.UpdatedAt) {
			return nil
		}
	}
	message := "proxy connectivity test failed"
	if result.Success {
		message = "proxy connectivity test succeeded"
	}
	raw, err := json.Marshal(EngineProxyConnectionReport{Success: result.Success, Message: message, Region: region, IP: result.IPAddress, LatencyMS: result.LatencyMs})
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE engine_proxy_sharing SET connection_report=$2,connection_checked_at=now(),engine_updated_at=$3,updated_at=now() WHERE proxy_id=$1
		AND (connection_checked_at IS NULL OR connection_checked_at<=$4)`, id, raw, engineUpdatedAt, probeStarted)
	return err
}

// Allocation performs only database reads while holding the rotation lock.
// Connectivity is refreshed by the explicit test API and background worker.
func (s *ProxyService) nextSharedEngineProxy(ctx context.Context, tx pgx.Tx, last *int64) (int64, error) {
	if s.EngineProxyState == nil {
		return 0, pgx.ErrNoRows
	}
	rows, err := tx.Query(ctx, `SELECT e.proxy_id,e.market_proxy_id,e.engine_updated_at FROM engine_proxy_sharing e JOIN proxies p ON p.id=e.market_proxy_id
		WHERE e.is_shared AND p.status<>'disabled' AND e.connection_report->>'success'='true' AND e.connection_checked_at>=now()-interval '24 hours'
		AND (e.engine_updated_at IS NULL OR e.engine_updated_at<=e.connection_checked_at)
		ORDER BY CASE WHEN e.market_proxy_id>COALESCE($1,0) THEN 0 ELSE 1 END,e.market_proxy_id FOR UPDATE OF p`, last)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		id, marketID    int64
		engineUpdatedAt *time.Time
	}
	items := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.marketID, &item.engineUpdatedAt); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		engine, err := s.liveEngineProxy(ctx, item.id)
		if err != nil || !engineProxyActive(engine, time.Now()) || item.engineUpdatedAt != nil && !engine.UpdatedAt.Equal(*item.engineUpdatedAt) {
			continue
		}
		return item.marketID, nil
	}
	return 0, pgx.ErrNoRows
}

// RefreshEngineSharedProxies performs bounded network work outside allocation
// transactions. A failed latest probe replaces any previously successful report.
func (s *ProxyService) RefreshEngineSharedProxies(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT proxy_id FROM engine_proxy_sharing WHERE is_shared ORDER BY proxy_id`)
	if err != nil {
		return err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		probeStarted := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		p, err := s.liveEngineProxy(probeCtx, id)
		result := &adminclient.ProxyTestResult{Success: false, Message: "proxy is inactive, expired or unavailable"}
		if err == nil && engineProxyActive(p, time.Now()) {
			result, err = s.Admin.TestProxy(probeCtx, id)
			if err != nil {
				result = &adminclient.ProxyTestResult{Success: false, Message: "connectivity test request failed"}
			}
		}
		cancel()
		if err := s.saveEngineProxyTest(ctx, id, result, p, probeStarted); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProxyService) SharedEngineProxyAvailability(ctx context.Context) (bool, int, error) {
	rows, err := s.DB.Query(ctx, `SELECT e.proxy_id,e.engine_updated_at FROM engine_proxy_sharing e JOIN proxies p ON p.id=e.market_proxy_id
		WHERE e.is_shared AND p.status<>'disabled' AND e.connection_report->>'success'='true'
		AND e.connection_checked_at>=now()-interval '24 hours'
		AND (e.engine_updated_at IS NULL OR e.engine_updated_at<=e.connection_checked_at) ORDER BY e.proxy_id`)
	if err != nil {
		return false, 0, err
	}
	type candidate struct {
		id        int64
		updatedAt *time.Time
	}
	ids := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.updatedAt); err != nil {
			rows.Close()
			return false, 0, err
		}
		ids = append(ids, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, 0, err
	}
	count := 0
	for _, item := range ids {
		p, err := s.liveEngineProxy(ctx, item.id)
		if err == nil && engineProxyActive(p, time.Now()) && (item.updatedAt == nil || p.UpdatedAt.Equal(*item.updatedAt)) {
			count++
		}
	}
	return count > 0, count, nil
}
