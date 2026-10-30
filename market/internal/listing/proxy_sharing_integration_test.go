//go:build integration

package listing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	platformdb "github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/provision"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

type proxySharingReplica struct{ *pgxpool.Pool }

func (rep proxySharingReplica) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return rep.Pool.Query(ctx, "SELECT 1 WHERE false")
}

type proxySharingFixture struct {
	ctx            context.Context
	service        *Service
	proxies        *ProxyService
	sellerID       int64
	otherID        int64
	accountProxies chan int64
}

func newProxySharingFixture(t *testing.T) *proxySharingFixture {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	if _, err := platformdb.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	fixture := &proxySharingFixture{ctx: ctx, accountProxies: make(chan int64, 32)}
	for email, target := range map[string]*int64{"proxy-sharing@test.local": &fixture.sellerID, "proxy-other@test.local": &fixture.otherID} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, email).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE vendor_configs SET enabled = true WHERE platform = 'anthropic'`); err != nil {
		t.Fatal(err)
	}
	reader, err := replica.New(proxySharingReplica{pool})
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "test", Keys: map[string]string{
		"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
	}})
	if err != nil {
		t.Fatal(err)
	}
	var nextID atomic.Int64
	nextID.Store(10000)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var data any
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/admin/groups":
			data = map[string]any{"id": nextID.Add(1)}
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/admin/accounts":
			var input adminclient.CreateAccountInput
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil || input.ProxyID == nil {
				t.Errorf("account must be created with its bound proxy: %+v, %v", input, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			fixture.accountProxies <- *input.ProxyID
			data = map[string]any{"id": nextID.Add(1)}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/models/sync-upstream"):
			data = map[string]any{"models": []string{"claude-sonnet-4-6"}}
		case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/api/v1/admin/proxies/") && strings.HasSuffix(request.URL.Path, "/test"):
			data = adminclient.ProxyTestResult{Success: true, Message: "connected"}
		case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/api/v1/admin/proxies/"):
			data = map[string]any{}
		default:
			t.Errorf("unexpected engine request: %s %s", request.Method, request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
			t.Errorf("encode engine response: %v", err)
		}
	}))
	t.Cleanup(engine.Close)
	admin := adminclient.New(engine.URL, "test", 5*time.Second)
	fixture.service = &Service{
		DB: pool, Admin: admin, Prov: &provision.Provisioner{Admin: admin, Replica: reader}, Keyring: keyring,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Settings: func(context.Context) (settings.Settings, error) {
			return settings.Settings{RequireSellerProxy: true}, nil
		},
	}
	fixture.proxies = &ProxyService{DB: pool, Admin: admin, Keyring: keyring}
	return fixture
}

func (fixture *proxySharingFixture) proxy(t *testing.T, sellerID *int64) *Proxy {
	t.Helper()
	var proxyID int64
	if err := fixture.service.DB.QueryRow(fixture.ctx, `INSERT INTO proxies(seller_id,name,protocol,host,port,connection_report,connection_checked_at)
		VALUES($1,'shared proxy','http','proxy-sharing.test',8080,'{"success":true}',now()) RETURNING id`, sellerID).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.DB.Exec(fixture.ctx, `UPDATE proxies SET sub2api_proxy_id = id + 1000 WHERE id = $1`, proxyID); err != nil {
		t.Fatal(err)
	}
	proxy, err := fixture.proxies.Get(fixture.ctx, proxyID)
	if err != nil {
		t.Fatal(err)
	}
	return proxy
}

func (fixture *proxySharingFixture) create(t *testing.T, proxyID *int64, fromImport bool) *Upstream {
	t.Helper()
	upstream, err := fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, proxyID, fromImport)
	if err != nil {
		t.Fatal(err)
	}
	if fromImport {
		if upstream.ProxyID != nil {
			t.Fatalf("import draft must remain unbound: %+v", upstream)
		}
	} else if proxyID != nil && (upstream.ProxyID == nil || *upstream.ProxyID != *proxyID) {
		t.Fatalf("selected proxy was not bound: %+v", upstream)
	}
	return upstream
}

func (fixture *proxySharingFixture) assertBindings(t *testing.T, proxyID int64, status string, upstreamIDs ...int64) {
	t.Helper()
	wantIDs := append([]int64{}, upstreamIDs...)
	slices.Sort(wantIDs)
	assert := func(proxy *Proxy) {
		t.Helper()
		if proxy.Status != status || proxy.AssignedUpstreamCount != len(wantIDs) || proxy.AssignedUpstreamIDs == nil || !slices.Equal(proxy.AssignedUpstreamIDs, wantIDs) {
			t.Fatalf("proxy bindings: got %+v, want status=%s ids=%v", proxy, status, wantIDs)
		}
		if len(wantIDs) == 0 {
			if proxy.AssignedUpstreamID != nil {
				t.Fatalf("empty proxy retained legacy binding: %+v", proxy)
			}
		} else if proxy.AssignedUpstreamID == nil || *proxy.AssignedUpstreamID != wantIDs[0] {
			t.Fatalf("legacy binding must be minimum upstream ID: %+v", proxy)
		}
	}
	proxy, err := fixture.proxies.Get(fixture.ctx, proxyID)
	if err != nil {
		t.Fatal(err)
	}
	assert(proxy)
	if proxy.SellerID != nil {
		owned, err := fixture.proxies.GetSeller(fixture.ctx, *proxy.SellerID, proxyID)
		if err != nil {
			t.Fatal(err)
		}
		assert(owned)
		listed, err := fixture.proxies.ListSeller(fixture.ctx, *proxy.SellerID)
		if err != nil || len(listed) != 1 {
			t.Fatalf("seller proxy inventory: %+v, %v", listed, err)
		}
		assert(&listed[0])
	}
	var storedStatus string
	var legacyID *int64
	var actualIDs []int64
	if err := fixture.service.DB.QueryRow(fixture.ctx, `SELECT status, assigned_upstream_id,
		ARRAY(SELECT id FROM upstreams WHERE proxy_id = proxies.id AND status <> 'removed' ORDER BY id)
		FROM proxies WHERE id = $1`, proxyID).Scan(&storedStatus, &legacyID, &actualIDs); err != nil {
		t.Fatal(err)
	}
	if storedStatus != status || !slices.Equal(actualIDs, wantIDs) ||
		(len(wantIDs) == 0 && legacyID != nil) || (len(wantIDs) > 0 && (legacyID == nil || *legacyID != wantIDs[0])) {
		t.Fatalf("stored bindings disagree with upstream source of truth: status=%s legacy=%v ids=%v, want %s %v", storedStatus, legacyID, actualIDs, status, wantIDs)
	}
}

func assertProxySharingHTTPError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Status != status || httpErr.Code != code {
		t.Fatalf("got error %v, want HTTP %d %s", err, status, code)
	}
}

func (fixture *proxySharingFixture) assertProtected(t *testing.T, proxyID int64) {
	t.Helper()
	for name, operation := range map[string]func() error{
		"seller disable": func() error {
			_, err := fixture.proxies.SetSellerDisabled(fixture.ctx, fixture.sellerID, proxyID, true)
			return err
		},
		"admin disable": func() error {
			_, err := fixture.proxies.SetDisabled(fixture.ctx, proxyID, true)
			return err
		},
		"edit": func() error {
			_, err := fixture.proxies.UpdateSeller(fixture.ctx, fixture.sellerID, proxyID, CreateProxyInput{Name: "changed", Protocol: "http", Host: "changed.test", Port: 9090})
			return err
		},
		"delete": func() error { return fixture.proxies.DeleteSeller(fixture.ctx, fixture.sellerID, proxyID) },
	} {
		t.Run(name, func(t *testing.T) {
			assertProxySharingHTTPError(t, operation(), http.StatusConflict, "PROXY_IN_USE")
		})
	}
	proxy, err := fixture.proxies.Get(fixture.ctx, proxyID)
	if err != nil || proxy.Name != "shared proxy" || proxy.Host != "proxy-sharing.test" || proxy.Port != 8080 {
		t.Fatalf("rejected mutation changed proxy: %+v, %v", proxy, err)
	}
}

func TestProxySharingCreateRemoveAndMutationProtection(t *testing.T) {
	fixture := newProxySharingFixture(t)
	proxy := fixture.proxy(t, &fixture.sellerID)
	first := fixture.create(t, &proxy.ID, false)
	second := fixture.create(t, &proxy.ID, false)
	fixture.assertBindings(t, proxy.ID, "assigned", first.ID, second.ID)
	fixture.assertProtected(t, proxy.ID)
	for _, upstream := range []*Upstream{first, second} {
		if err := fixture.service.Remove(fixture.ctx, fixture.sellerID, upstream.ID); err != nil {
			t.Fatal(err)
		}
		removed, err := GetUpstream(fixture.ctx, fixture.service.DB, upstream.ID)
		if err != nil || removed.Status != StatusRemoved || removed.ProxyID != nil {
			t.Fatalf("removed draft must release its forward binding: %+v, %v", removed, err)
		}
		if upstream.ID == first.ID {
			if err := fixture.service.Remove(fixture.ctx, fixture.sellerID, first.ID); err != nil {
				t.Fatalf("repeated removal: %v", err)
			}
			fixture.assertBindings(t, proxy.ID, "assigned", second.ID)
			fixture.assertProtected(t, proxy.ID)
		}
	}
	fixture.assertBindings(t, proxy.ID, "available")
	if _, err := fixture.proxies.SetSellerDisabled(fixture.ctx, fixture.sellerID, proxy.ID, true); err != nil {
		t.Fatal(err)
	}
	fixture.assertBindings(t, proxy.ID, "disabled")
	if _, err := fixture.proxies.SetSellerDisabled(fixture.ctx, fixture.sellerID, proxy.ID, false); err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.proxies.UpdateSeller(fixture.ctx, fixture.sellerID, proxy.ID, CreateProxyInput{Name: "released", Protocol: proxy.Protocol, Host: proxy.Host, Port: proxy.Port})
	if err != nil || updated.Name != "released" {
		t.Fatalf("released proxy must be editable: %+v, %v", updated, err)
	}
	if err := fixture.proxies.DeleteSeller(fixture.ctx, fixture.sellerID, proxy.ID); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.proxies.Get(fixture.ctx, proxy.ID)
	assertProxySharingHTTPError(t, err, http.StatusNotFound, "PROXY_NOT_FOUND")
}

func (fixture *proxySharingFixture) importInput(token string) ImportAccountInput {
	return ImportAccountInput{Data: json.RawMessage(fmt.Sprintf(`{
		"accounts":[{"platform":"anthropic","type":"oauth","proxy_key":"shared","credentials":{"access_token":%q}}],
		"proxies":[{"proxy_key":"shared","protocol":"http","host":"proxy-sharing.test","port":8080}]
	}`, token))}
}

func TestProxySharingImportReusesAssignedSellerProxy(t *testing.T) {
	fixture := newProxySharingFixture(t)
	proxy := fixture.proxy(t, &fixture.sellerID)
	selected := fixture.create(t, &proxy.ID, false)
	upstreamIDs := []int64{selected.ID}
	for index := range 2 {
		draft := fixture.create(t, nil, true)
		imported, err := fixture.service.ImportAccount(fixture.ctx, fixture.sellerID, draft.ID, fixture.importInput(fmt.Sprintf("import-token-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		if imported.ProxyID == nil || *imported.ProxyID != proxy.ID || imported.Sub2APIAccountID == nil || imported.Status != StatusSelectingModels {
			t.Fatalf("import must reuse the selected account's seller proxy: %+v", imported)
		}
		select {
		case actualProxy := <-fixture.accountProxies:
			if actualProxy != *proxy.Sub2APIProxyID {
				t.Fatalf("engine account proxy = %d, want %d", actualProxy, *proxy.Sub2APIProxyID)
			}
		default:
			t.Fatal("import did not provision an engine account")
		}
		upstreamIDs = append(upstreamIDs, imported.ID)
		fixture.assertBindings(t, proxy.ID, "assigned", upstreamIDs...)
	}
	fixture.assertProtected(t, proxy.ID)
}

func TestProxySharingSellerIsolationAndDisabled(t *testing.T) {
	fixture := newProxySharingFixture(t)
	proxy := fixture.proxy(t, &fixture.sellerID)
	otherProxies, err := fixture.proxies.ListSeller(fixture.ctx, fixture.otherID)
	if err != nil || len(otherProxies) != 0 {
		t.Fatalf("other seller must not see proxy inventory: %+v, %v", otherProxies, err)
	}
	for name, operation := range map[string]func() error{
		"get": func() error { _, err := fixture.proxies.GetSeller(fixture.ctx, fixture.otherID, proxy.ID); return err },
		"disable": func() error {
			_, err := fixture.proxies.SetSellerDisabled(fixture.ctx, fixture.otherID, proxy.ID, true)
			return err
		},
		"edit": func() error {
			_, err := fixture.proxies.UpdateSeller(fixture.ctx, fixture.otherID, proxy.ID, CreateProxyInput{})
			return err
		},
		"delete": func() error { return fixture.proxies.DeleteSeller(fixture.ctx, fixture.otherID, proxy.ID) },
		"create": func() error {
			_, err := fixture.service.CreateUpstream(fixture.ctx, fixture.otherID, "anthropic", "oauth", false, &proxy.ID, false)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertProxySharingHTTPError(t, operation(), http.StatusNotFound, "PROXY_NOT_FOUND")
		})
	}
	otherDraft, err := fixture.service.CreateUpstream(fixture.ctx, fixture.otherID, "anthropic", "oauth", false, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.ImportAccount(fixture.ctx, fixture.otherID, otherDraft.ID, fixture.importInput("foreign-import"))
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_ALREADY_EXISTS")
	_, err = fixture.service.ImportAccount(fixture.ctx, fixture.sellerID, otherDraft.ID, fixture.importInput("foreign-upstream"))
	if !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("cross-seller import: %v", err)
	}
	if err := fixture.service.Remove(fixture.ctx, fixture.sellerID, otherDraft.ID); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("cross-seller removal: %v", err)
	}
	if _, err := fixture.proxies.SetSellerDisabled(fixture.ctx, fixture.sellerID, proxy.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, &proxy.ID, false)
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_IN_USE")
	draft := fixture.create(t, nil, true)
	_, err = fixture.service.ImportAccount(fixture.ctx, fixture.sellerID, draft.ID, fixture.importInput("disabled-import"))
	assertProxySharingHTTPError(t, err, http.StatusConflict, "PROXY_IN_USE")
	fixture.assertBindings(t, proxy.ID, "disabled")
	var total, bound int
	if err := fixture.service.DB.QueryRow(fixture.ctx, `SELECT count(*), count(proxy_id) FROM upstreams`).Scan(&total, &bound); err != nil || total != 2 || bound != 0 {
		t.Fatalf("rejected creation/import must not leave bindings or extra drafts: total=%d bound=%d, %v", total, bound, err)
	}
}

func TestProxySharingPlatformRemainsExclusive(t *testing.T) {
	fixture := newProxySharingFixture(t)
	sellerProxy := fixture.proxy(t, &fixture.sellerID)
	fixture.service.Settings = func(context.Context) (settings.Settings, error) { return settings.Settings{}, nil }
	_, err := fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, nil, false)
	if !errors.Is(err, ErrNoProxy) {
		t.Fatalf("platform fallback must not borrow seller inventory: %v", err)
	}
	platformProxy := fixture.proxy(t, nil)
	first := fixture.create(t, nil, false)
	if first.ProxyID == nil || *first.ProxyID != platformProxy.ID {
		t.Fatalf("platform fallback selected the wrong inventory: %+v", first)
	}
	_, err = fixture.service.CreateUpstream(fixture.ctx, fixture.otherID, "anthropic", "oauth", false, nil, false)
	if !errors.Is(err, ErrNoProxy) {
		t.Fatalf("second platform binding must fail: %v", err)
	}
	_, err = fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, &platformProxy.ID, false)
	assertProxySharingHTTPError(t, err, http.StatusNotFound, "PROXY_NOT_FOUND")
	_, err = fixture.service.DB.Exec(fixture.ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'anthropic','oauth',$2)`, fixture.otherID, platformProxy.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("database must enforce platform exclusivity: %v", err)
	}
	fixture.assertBindings(t, platformProxy.ID, "assigned", first.ID)
	fixture.assertBindings(t, sellerProxy.ID, "available")
	if err := fixture.service.Remove(fixture.ctx, fixture.sellerID, first.ID); err != nil {
		t.Fatal(err)
	}
	fixture.assertBindings(t, platformProxy.ID, "available")
	next, err := fixture.service.CreateUpstream(fixture.ctx, fixture.otherID, "anthropic", "oauth", false, nil, false)
	if err != nil || next.ProxyID == nil || *next.ProxyID != platformProxy.ID {
		t.Fatalf("released platform proxy must be reusable: %+v, %v", next, err)
	}
	fixture.assertBindings(t, platformProxy.ID, "assigned", next.ID)
}

func TestProxySharingConcurrentBindings(t *testing.T) {
	for _, mode := range []string{"seller", "import", "platform"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newProxySharingFixture(t)
			owner := &fixture.sellerID
			if mode == "platform" {
				owner = nil
				fixture.service.Settings = func(context.Context) (settings.Settings, error) { return settings.Settings{}, nil }
			}
			proxy := fixture.proxy(t, owner)
			attempts := 4
			if mode == "import" {
				attempts = min(attempts, int(fixture.service.DB.Config().MaxConns)-1)
			}
			drafts := make([]*Upstream, attempts)
			if mode == "import" {
				for index := range attempts {
					drafts[index] = fixture.create(t, nil, true)
				}
			}
			type result struct {
				upstream *Upstream
				err      error
			}
			results := make(chan result, attempts)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for index := range attempts {
				workers.Go(func() {
					<-start
					var upstream *Upstream
					var err error
					switch mode {
					case "import":
						upstream, err = fixture.service.ImportAccount(fixture.ctx, fixture.sellerID, drafts[index].ID, fixture.importInput(fmt.Sprintf("parallel-import-%d", index)))
					case "seller":
						upstream, err = fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, &proxy.ID, false)
					case "platform":
						upstream, err = fixture.service.CreateUpstream(fixture.ctx, fixture.sellerID, "anthropic", "oauth", false, nil, false)
					}
					results <- result{upstream, err}
				})
			}
			close(start)
			workers.Wait()
			close(results)
			var upstreamIDs []int64
			for result := range results {
				if result.err != nil {
					if mode != "platform" || !errors.Is(result.err, ErrNoProxy) {
						t.Fatalf("concurrent %s binding: %v", mode, result.err)
					}
					continue
				}
				if result.upstream.ProxyID == nil || *result.upstream.ProxyID != proxy.ID {
					t.Fatalf("concurrent binding used wrong proxy: %+v", result.upstream)
				}
				upstreamIDs = append(upstreamIDs, result.upstream.ID)
			}
			wantCount := attempts
			if mode == "platform" {
				wantCount = 1
			}
			if len(upstreamIDs) != wantCount {
				t.Fatalf("concurrent %s: got %d successful bindings, want %d", mode, len(upstreamIDs), wantCount)
			}
			fixture.assertBindings(t, proxy.ID, "assigned", upstreamIDs...)
			var total int
			if err := fixture.service.DB.QueryRow(fixture.ctx, `SELECT count(*) FROM upstreams`).Scan(&total); err != nil || total != wantCount {
				t.Fatalf("concurrent bindings left orphan drafts: count=%d want=%d, %v", total, wantCount, err)
			}
		})
	}
}
