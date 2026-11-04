//go:build integration

package listing

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSharedPlatformProxyRotationAndDirectFallback(t *testing.T) {
	f := newProxySharingFixture(t)
	if _, err := f.service.DB.Exec(f.ctx, `UPDATE vendor_configs SET require_seller_proxy=false WHERE platform='anthropic'`); err != nil {
		t.Fatal(err)
	}
	exclusive := f.proxy(t, nil)
	ownerProxy := f.proxy(t, &f.sellerID)
	direct := f.create(t, nil, false)
	if direct.ProxyID != nil {
		t.Fatalf("optional vendor must use direct egress without shared inventory: %+v", direct)
	}
	if id, err := f.service.proxySub2APIID(f.ctx, direct); err != nil || id != nil {
		t.Fatalf("direct draft cannot authorize: id=%v err=%v", id, err)
	}
	if available, count, err := f.proxies.SharedProxyAvailability(f.ctx); err != nil || available || count != 0 {
		t.Fatalf("unexpected shared availability: %v %d %v", available, count, err)
	}
	first, second := f.proxy(t, nil), f.proxy(t, nil)
	for _, proxy := range []*Proxy{first, second} {
		if _, err := f.proxies.SetEngineProxyShared(f.ctx, *proxy.Sub2APIProxyID, true); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 6 {
		upstream := f.create(t, nil, false)
		want := first.ID
		if index%2 != 0 {
			want = second.ID
		}
		if upstream.ProxyID == nil || *upstream.ProxyID != want {
			t.Fatalf("round %d selected %+v, want proxy %d", index, upstream, want)
		}
	}
	selected := f.create(t, &ownerProxy.ID, false)
	if selected.ProxyID == nil || *selected.ProxyID != ownerProxy.ID {
		t.Fatalf("optional vendor ignored explicit seller proxy: %+v", selected)
	}
	if available, count, err := f.proxies.SharedProxyAvailability(f.ctx); err != nil || !available || count != 2 {
		t.Fatalf("shared availability ignores already assigned proxies: %v %d %v", available, count, err)
	}
	// A failed most-recent test removes the proxy from both status and allocation.
	if _, err := f.service.DB.Exec(f.ctx, `UPDATE engine_proxy_sharing SET connection_report='{"success":false}' WHERE proxy_id=$1`, *first.Sub2APIProxyID); err != nil {
		t.Fatal(err)
	}
	if available, count, err := f.proxies.SharedProxyAvailability(f.ctx); err != nil || !available || count != 1 {
		t.Fatalf("unreachable shared proxy was still eligible: %v %d %v", available, count, err)
	}
	upstream := f.create(t, nil, false)
	if upstream.ProxyID == nil || *upstream.ProxyID != second.ID {
		t.Fatalf("unreachable proxy selected: %+v", upstream)
	}
	f.assertBindings(t, exclusive.ID, "available")
}

func TestSharedPlatformProxyConcurrentRotation(t *testing.T) {
	f := newProxySharingFixture(t)
	if _, err := f.service.DB.Exec(f.ctx, `UPDATE vendor_configs SET require_seller_proxy=false WHERE platform='anthropic'`); err != nil {
		t.Fatal(err)
	}
	proxies := []*Proxy{f.proxy(t, nil), f.proxy(t, nil), f.proxy(t, nil)}
	for _, proxy := range proxies {
		if _, err := f.proxies.SetEngineProxyShared(f.ctx, *proxy.Sub2APIProxyID, true); err != nil {
			t.Fatal(err)
		}
	}
	// Capture binding order inside each allocating transaction, while the
	// rotation lock is still held. HTTP completion order is not commit order.
	if _, err := f.service.DB.Exec(f.ctx, `CREATE TABLE rotation_test_bindings(sequence bigserial, proxy_id bigint);
		CREATE FUNCTION record_rotation_test_binding() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.proxy_id IS NOT NULL THEN
				INSERT INTO rotation_test_bindings(proxy_id) VALUES(NEW.proxy_id);
			END IF;
			RETURN NULL;
		END $$;
		CREATE TRIGGER record_rotation_test_binding AFTER UPDATE OF proxy_id ON upstreams
		FOR EACH ROW EXECUTE FUNCTION record_rotation_test_binding()`); err != nil {
		t.Fatal(err)
	}
	const attempts = 18
	testsBefore := f.proxyTests.Load()
	started := time.Now()
	type result struct {
		u   *Upstream
		err error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := range attempts {
		wg.Go(func() {
			<-start
			sellerID := f.sellerID
			if index%2 == 1 {
				sellerID = f.otherID
			}
			u, err := f.service.CreateUpstream(f.ctx, sellerID, "anthropic", "oauth", false, nil, false)
			results <- result{u, err}
		})
	}
	close(start)
	wg.Wait()
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("concurrent allocation unexpectedly slow: %v", elapsed)
	}
	if f.proxyTests.Load() != testsBefore {
		t.Fatal("allocation performed an engine connectivity test while holding rotation lock")
	}
	close(results)
	counts := map[int64]int{}
	for result := range results {
		if result.err != nil || result.u.ProxyID == nil {
			t.Fatalf("concurrent allocation failed: %+v %v", result.u, result.err)
		}
		counts[*result.u.ProxyID]++
	}
	for _, proxy := range proxies {
		if counts[proxy.ID] != attempts/len(proxies) {
			t.Fatalf("rotation is not balanced across concurrent commits: %+v", counts)
		}
	}
	rows, err := f.service.DB.Query(f.ctx, `SELECT proxy_id FROM rotation_test_bindings ORDER BY sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var proxyID int64
		if err := rows.Scan(&proxyID); err != nil {
			t.Fatal(err)
		}
		if want := proxies[index%len(proxies)].ID; proxyID != want {
			t.Fatalf("concurrent binding %d selected %d, want %d", index, proxyID, want)
		}
		index++
	}
	if err := rows.Err(); err != nil || index != attempts {
		t.Fatalf("binding history count=%d err=%v", index, err)
	}
	var lastProxyID int64
	if err := f.service.DB.QueryRow(f.ctx, `SELECT last_proxy_id FROM shared_proxy_rotation WHERE id=1`).Scan(&lastProxyID); err != nil || lastProxyID != proxies[len(proxies)-1].ID {
		t.Fatalf("persistent cursor=%d err=%v", lastProxyID, err)
	}
}

func TestSharedPlatformProxyPolicyAndMutationGuards(t *testing.T) {
	f := newProxySharingFixture(t)
	_, err := f.service.CreateUpstream(f.ctx, f.sellerID, "anthropic", "oauth", false, nil, false)
	assertProxySharingHTTPError(t, err, http.StatusBadRequest, "SELLER_PROXY_REQUIRED")
	importDraft := f.create(t, nil, true)
	_, err = f.service.proxySub2APIID(f.ctx, importDraft)
	assertProxySharingHTTPError(t, err, http.StatusBadRequest, "SELLER_PROXY_REQUIRED")
	_, err = f.service.ImportAccount(f.ctx, f.sellerID, importDraft.ID, ImportAccountInput{Data: []byte(`{"accounts":[{"platform":"anthropic","type":"oauth","credentials":{"access_token":"no-proxy"}}]}`)})
	assertProxySharingHTTPError(t, err, http.StatusBadRequest, "SELLER_PROXY_REQUIRED")
	proxy := f.proxy(t, nil)
	if _, err := f.proxies.SetEngineProxyShared(f.ctx, *proxy.Sub2APIProxyID, true); err != nil {
		t.Fatal(err)
	}
	for _, sellerID := range []int64{f.sellerID, f.otherID} {
		if _, err := f.service.DB.Exec(f.ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'anthropic','oauth',$2)`, sellerID, proxy.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.proxies.SetEngineProxyShared(f.ctx, *proxy.Sub2APIProxyID, false)
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_SHARED_IN_USE")
	_, err = f.proxies.SetDisabled(f.ctx, proxy.ID, true)
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_IN_USE")
	err = f.proxies.Delete(f.ctx, proxy.ID)
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_IN_USE")
	for _, query := range []string{
		`UPDATE engine_proxy_sharing SET is_shared=false WHERE proxy_id=$1`,
		`UPDATE proxies SET status='disabled' WHERE id=$1`,
		`DELETE FROM proxies WHERE id=$1`,
	} {
		id := proxy.ID
		if strings.Contains(query, "engine_proxy_sharing") {
			id = *proxy.Sub2APIProxyID
		}
		_, err := f.service.DB.Exec(f.ctx, query, id)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("database guard accepted invalid mutation %q: %v", query, err)
		}
	}
	sellerProxy := f.proxy(t, &f.sellerID)
	_, err = f.proxies.SetShared(f.ctx, sellerProxy.ID, true)
	assertProxySharingHTTPError(t, err, http.StatusBadRequest, "SELLER_PROXY_CANNOT_BE_SHARED")
	_, err = f.service.DB.Exec(f.ctx, `UPDATE proxies SET is_shared=true WHERE id=$1`, sellerProxy.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("seller proxy must not become platform shared: %v", err)
	}
}
