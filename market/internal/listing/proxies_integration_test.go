//go:build integration

package listing

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	platformdb "github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func proxyTestService(t *testing.T) (*ProxyService, *pgxpool.Pool, int64, int64) {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := platformdb.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var sellerID, proxyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('proxy-seller@test.local','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO proxies(seller_id,name,protocol,host,port,region,sub2api_proxy_id)
		VALUES($1,'original','http','proxy.test',8080,'sg',91) RETURNING id`, sellerID).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	keyring, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	return &ProxyService{DB: pool, Keyring: keyring}, pool, sellerID, proxyID
}

func requireProxyConflict(t *testing.T, err error) {
	t.Helper()
	var conflict *httpx.Error
	if !errors.As(err, &conflict) || conflict.Code != "PROXY_IN_USE" {
		t.Fatalf("expected PROXY_IN_USE, got %v", err)
	}
}

func TestProxyBindingsDerivedFromUpstreams(t *testing.T) {
	service, pool, sellerID, proxyID := proxyTestService(t)
	ctx := context.Background()
	var first, second int64
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id,status) VALUES($1,'openai','oauth',$2,'failed') RETURNING id`, sellerID, proxyID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id,status) VALUES($1,'openai','oauth',$2,'removing') RETURNING id`, sellerID, proxyID).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id,status) VALUES($1,'openai','oauth',$2,'removed')`, sellerID, proxyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE proxies SET assigned_upstream_id=NULL WHERE id=$1`, proxyID); err != nil {
		t.Fatal(err)
	}
	assert := func(proxy *Proxy) {
		t.Helper()
		if proxy.AssignedUpstreamCount != 2 || len(proxy.AssignedUpstreamIDs) != 2 || proxy.AssignedUpstreamIDs[0] != first || proxy.AssignedUpstreamIDs[1] != second || proxy.AssignedUpstreamID == nil || *proxy.AssignedUpstreamID != first || proxy.Region == nil || *proxy.Region != "sg" || proxy.Sub2APIProxyID == nil || *proxy.Sub2APIProxyID != 91 {
			t.Fatalf("incorrect derived proxy response: %+v", proxy)
		}
	}
	for _, get := range []func(context.Context, int64) (*Proxy, error){service.Get, func(ctx context.Context, id int64) (*Proxy, error) { return service.GetSeller(ctx, sellerID, id) }} {
		proxy, err := get(ctx, proxyID)
		if err != nil {
			t.Fatal(err)
		}
		assert(proxy)
	}
	for _, list := range []func(context.Context) ([]Proxy, error){service.List, func(ctx context.Context) ([]Proxy, error) { return service.ListSeller(ctx, sellerID) }} {
		proxies, err := list(ctx)
		if err != nil || len(proxies) != 1 {
			t.Fatalf("proxy list: %+v %v", proxies, err)
		}
		assert(&proxies[0])
	}
	_, err := service.UpdateSeller(ctx, sellerID, proxyID, CreateProxyInput{Name: "edited", Protocol: "http", Host: "proxy.test", Port: 8080})
	requireProxyConflict(t, err)
	_, err = service.SetDisabled(ctx, proxyID, true)
	requireProxyConflict(t, err)
	_, err = service.SetSellerDisabled(ctx, sellerID, proxyID, false)
	requireProxyConflict(t, err)
	requireProxyConflict(t, service.DeleteSeller(ctx, sellerID, proxyID))
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='removed' WHERE proxy_id=$1`, proxyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE proxies SET assigned_upstream_id=$2 WHERE id=$1`, proxyID, first); err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "new-password"} {
		proxy, err := service.UpdateSeller(ctx, sellerID, proxyID, CreateProxyInput{Name: "edited", Protocol: "http", Host: "proxy.test", Port: 8080, Password: password})
		if err != nil || proxy.AssignedUpstreamCount != 0 || proxy.AssignedUpstreamID != nil || proxy.AssignedUpstreamIDs == nil {
			t.Fatalf("stale legacy ID must not block editing: %+v %v", proxy, err)
		}
	}
	if _, err := service.SetSellerDisabled(ctx, sellerID, proxyID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetDisabled(ctx, proxyID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE proxies SET sub2api_proxy_id=NULL WHERE id=$1`, proxyID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSeller(ctx, sellerID, proxyID); err != nil {
		t.Fatal(err)
	}
}

func TestProxyWritesRecheckBindingsAfterLockWait(t *testing.T) {
	for _, operation := range []string{"edit", "disable", "delete"} {
		t.Run(operation, func(t *testing.T) {
			service, pool, sellerID, proxyID := proxyTestService(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			writer, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(context.Background())
			if _, err := writer.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id) VALUES($1,'openai','oauth',$2)`, sellerID, proxyID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "edit":
					_, err = service.UpdateSeller(ctx, sellerID, proxyID, CreateProxyInput{Name: "edited", Protocol: "http", Host: "proxy.test", Port: 8080})
				case "disable":
					_, err = service.SetDisabled(ctx, proxyID, true)
				case "delete":
					err = service.DeleteSeller(ctx, sellerID, proxyID)
				}
				done <- err
			}()
			for {
				var waiting bool
				err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM proxies WHERE id = $1%')`).Scan(&waiting)
				if err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("write did not wait for binding transaction: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err := writer.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			requireProxyConflict(t, <-done)
			proxy, err := service.Get(ctx, proxyID)
			if err != nil || proxy.Name != "original" || proxy.Status != "assigned" || proxy.AssignedUpstreamCount != 1 {
				t.Fatalf("concurrent binding was overwritten: %+v %v", proxy, err)
			}
		})
	}
}
