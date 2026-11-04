//go:build integration

package listing

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

type mutableEngineProxyReader struct {
	status  string
	expires *time.Time
	updated time.Time
	deleted bool
}
type engineProxyStateRow struct {
	state *mutableEngineProxyReader
	id    int64
}

func (r engineProxyStateRow) Scan(out ...any) error {
	if r.state.deleted {
		return pgx.ErrNoRows
	}
	*out[0].(*int64) = r.id
	*out[1].(*string) = "engine proxy"
	*out[2].(*string) = "http"
	*out[3].(*string) = "private-engine-host"
	*out[4].(*int) = 8080
	*out[5].(*string) = r.state.status
	*out[6].(**time.Time) = r.state.expires
	*out[7].(*time.Time) = r.state.updated
	return nil
}
func (r *mutableEngineProxyReader) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	return engineProxyStateRow{r, args[0].(int64)}
}

func TestEngineProxyEligibilityAndNoHTTPAllocation(t *testing.T) {
	f := newProxySharingFixture(t)
	if _, err := f.service.DB.Exec(f.ctx, `UPDATE vendor_configs SET require_seller_proxy=false WHERE platform='anthropic'`); err != nil {
		t.Fatal(err)
	}
	p := f.proxy(t, nil)
	state := &mutableEngineProxyReader{status: "active", updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	f.proxies.EngineProxyState = state
	// Manual test on an unshared engine proxy must save a report and return false.
	result, err := f.proxies.TestEngineProxy(f.ctx, *p.Sub2APIProxyID)
	if err != nil || result.IsShared || result.ConnectionReport == nil || !result.ConnectionReport.Success {
		t.Fatalf("unshared test was not persisted: %+v %v", result, err)
	}
	if _, err := f.proxies.SetEngineProxyShared(f.ctx, *p.Sub2APIProxyID, true); err != nil {
		t.Fatal(err)
	}
	countBefore := f.proxyTests.Load()
	if u := f.create(t, nil, false); u.ProxyID == nil || *u.ProxyID != p.ID {
		t.Fatalf("engine shared proxy not selected: %+v", u)
	}
	if f.proxyTests.Load() != countBefore {
		t.Fatal("allocation must not perform network connectivity probes")
	}
	for _, condition := range []string{"inactive", "expired", "deleted", "edited", "failed", "stale"} {
		t.Run(condition, func(t *testing.T) {
			state.status = "active"
			state.expires = nil
			state.deleted = false
			state.updated = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			if _, err := f.service.DB.Exec(f.ctx, `UPDATE engine_proxy_sharing SET connection_report='{"success":true}',connection_checked_at=now(),engine_updated_at=$2 WHERE proxy_id=$1`, *p.Sub2APIProxyID, state.updated); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "inactive":
				state.status = "inactive"
			case "expired":
				past := time.Now().Add(-time.Hour)
				state.expires = &past
			case "deleted":
				state.deleted = true
			case "edited":
				state.updated = state.updated.Add(time.Minute)
			case "failed":
				if _, err := f.service.DB.Exec(f.ctx, `UPDATE engine_proxy_sharing SET connection_report='{"success":false}' WHERE proxy_id=$1`, *p.Sub2APIProxyID); err != nil {
					t.Fatal(err)
				}
			case "stale":
				if _, err := f.service.DB.Exec(f.ctx, `UPDATE engine_proxy_sharing SET connection_checked_at=now()-interval '25 hours' WHERE proxy_id=$1`, *p.Sub2APIProxyID); err != nil {
					t.Fatal(err)
				}
			}
			if available, count, err := f.proxies.SharedProxyAvailability(f.ctx); err != nil || available || count != 0 {
				t.Fatalf("ineligible proxy advertised: %v %d %v", available, count, err)
			}
			if u := f.create(t, nil, false); u.ProxyID != nil {
				t.Fatalf("ineligible proxy assigned: %+v", u)
			}
		})
	}
}

func TestEngineProxyOldProbeCannotOverwriteNewFailure(t *testing.T) {
	f := newProxySharingFixture(t)
	p := f.proxy(t, nil)
	if _, err := f.proxies.SetEngineProxyShared(f.ctx, *p.Sub2APIProxyID, true); err != nil {
		t.Fatal(err)
	}
	state, err := f.proxies.liveEngineProxy(f.ctx, *p.Sub2APIProxyID)
	if err != nil {
		t.Fatal(err)
	}
	oldStarted := time.Now().Add(-time.Minute)
	if err := f.proxies.saveEngineProxyTest(f.ctx, *p.Sub2APIProxyID, &adminclient.ProxyTestResult{Success: false}, state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.proxies.saveEngineProxyTest(f.ctx, *p.Sub2APIProxyID, &adminclient.ProxyTestResult{Success: true}, state, oldStarted); err != nil {
		t.Fatal(err)
	}
	result, err := f.proxies.engineProxySharing(f.ctx, *p.Sub2APIProxyID)
	if err != nil || result.ConnectionReport == nil || result.ConnectionReport.Success {
		t.Fatalf("old in-flight success replaced latest failure: %+v %v", result, err)
	}
}

func TestEngineProxyOwnedBySellerCanTestButCannotShare(t *testing.T) {
	f := newProxySharingFixture(t)
	p := f.proxy(t, &f.sellerID)
	result, err := f.proxies.TestEngineProxy(f.ctx, *p.Sub2APIProxyID)
	if err != nil || result.IsShared || result.ConnectionReport == nil || !result.ConnectionReport.Success {
		t.Fatalf("engine diagnostics regressed for seller-owned proxy: %+v %v", result, err)
	}
	_, err = f.proxies.SetEngineProxyShared(f.ctx, *p.Sub2APIProxyID, true)
	assertProxySharingHTTPError(t, err, 409, "PROXY_MAPPING_CONFLICT")
	items, err := f.proxies.ListEngineProxySharing(f.ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("seller-owned proxy entered sharing inventory: %+v %v", items, err)
	}
}

func TestEngineProxyCreatesPrivateBindingSlot(t *testing.T) {
	f := newProxySharingFixture(t)
	if _, err := f.service.DB.Exec(f.ctx, `UPDATE vendor_configs SET require_seller_proxy=false WHERE platform='anthropic'`); err != nil {
		t.Fatal(err)
	}
	const engineID int64 = 987654
	if _, err := f.proxies.SetEngineProxyShared(f.ctx, engineID, true); err != nil {
		t.Fatal(err)
	}
	var marketID int64
	var username *string
	var password []byte
	if err := f.service.DB.QueryRow(f.ctx, `SELECT p.id,p.username,p.password_enc FROM proxies p JOIN engine_proxy_sharing e ON e.market_proxy_id=p.id WHERE e.proxy_id=$1`, engineID).Scan(&marketID, &username, &password); err != nil {
		t.Fatal(err)
	}
	if username != nil || len(password) != 0 {
		t.Fatal("engine credentials must not be copied to the market binding slot")
	}
	if u := f.create(t, nil, false); u.ProxyID == nil || *u.ProxyID != marketID {
		t.Fatalf("native engine proxy mapping was not used: %+v", u)
	}
}

func TestEngineProxyCheckRequestFailureClearsEligibility(t *testing.T) {
	for _, operation := range []string{"manual-test", "enable-sharing"} {
		t.Run(operation, func(t *testing.T) {
			f := newProxySharingFixture(t)
			p := f.proxy(t, nil)
			if _, err := f.proxies.SetEngineProxyShared(f.ctx, *p.Sub2APIProxyID, true); err != nil {
				t.Fatal(err)
			}
			f.proxyTestError.Store(true)
			var err error
			if operation == "manual-test" {
				_, err = f.proxies.TestEngineProxy(f.ctx, *p.Sub2APIProxyID)
			} else {
				_, err = f.proxies.SetEngineProxyShared(f.ctx, *p.Sub2APIProxyID, true)
			}
			assertProxySharingHTTPError(t, err, 503, "PROXY_CHECK_FAILED")
			result, err := f.proxies.engineProxySharing(f.ctx, *p.Sub2APIProxyID)
			if err != nil || result.ConnectionReport == nil || result.ConnectionReport.Success {
				t.Fatalf("failed check retained successful report: %+v %v", result, err)
			}
			if available, count, err := f.proxies.SharedProxyAvailability(f.ctx); err != nil || available || count != 0 {
				t.Fatalf("failed check retained eligibility: %v %d %v", available, count, err)
			}
		})
	}
}
