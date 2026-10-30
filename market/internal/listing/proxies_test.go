package listing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

type proxyRow struct {
	ids []int64
}

func (row proxyRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		*dest[0].(*int64) = 1
		return nil
	}
	values := []any{int64(1), (*int64)(nil), "proxy", "http", "proxy.test", 8080,
		(*string)(nil), (*string)(nil), func() *int64 { id := int64(91); return &id }(),
		"available", row.ids, time.Now(), []byte(nil), []byte(nil), (*time.Time)(nil), (*time.Time)(nil)}
	for index, value := range values {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type proxyWriteDB struct {
	pgx.Tx
	testing   *testing.T
	affected  int64
	writeErr  error
	commitErr error
	committed bool
	queries   int
	ids       []int64
}

func (db *proxyWriteDB) Begin(context.Context) (pgx.Tx, error) { return db, nil }

func (db *proxyWriteDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db.queries++
	if db.queries == 1 && !strings.Contains(query, "FOR UPDATE") {
		db.testing.Fatalf("first statement must lock proxy: %s", query)
	}
	return proxyRow{ids: db.ids}
}

func (db *proxyWriteDB) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	if db.queries != 2 || !strings.Contains(query, "NOT EXISTS") || !strings.Contains(query, "status <> 'removed'") {
		db.testing.Fatalf("write must follow lock and fresh binding read with SQL guard: %s", query)
	}
	if db.affected == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), db.writeErr
	}
	return pgconn.NewCommandTag("DELETE 1"), db.writeErr
}

func (db *proxyWriteDB) Commit(context.Context) error {
	db.committed = db.commitErr == nil
	return db.commitErr
}

func (db *proxyWriteDB) Rollback(context.Context) error { return nil }

func TestScanProxyDerivesBindings(t *testing.T) {
	for _, ids := range [][]int64{nil, {}, {3}, {3, 8}} {
		proxy, err := scanProxy(proxyRow{ids: ids})
		if err != nil || proxy.AssignedUpstreamIDs == nil || proxy.AssignedUpstreamCount != len(ids) {
			t.Fatalf("binding response: %+v, %v", proxy, err)
		}
		if len(ids) == 0 && proxy.AssignedUpstreamID != nil || len(ids) > 0 && (proxy.AssignedUpstreamID == nil || *proxy.AssignedUpstreamID != ids[0]) {
			t.Fatalf("legacy binding must be first ID: %+v", proxy)
		}
	}
}

func TestUpdateSellerRejectsZeroRowsAffected(t *testing.T) {
	db := &proxyWriteDB{testing: t}
	service := &ProxyService{DB: db}
	_, err := service.UpdateSeller(context.Background(), 7, 1, CreateProxyInput{Name: "edited", Protocol: "http", Host: "proxy.test", Port: 8080})
	var conflict *httpx.Error
	if !errors.As(err, &conflict) || conflict.Code != "PROXY_IN_USE" || db.committed {
		t.Fatalf("zero-row update must conflict without committing: %v", err)
	}
}

func TestDeleteSellerOnlyCleansEngineAfterCommit(t *testing.T) {
	for _, outcome := range []string{"bound", "zero rows", "delete error", "commit error", "success"} {
		t.Run(outcome, func(t *testing.T) {
			db := &proxyWriteDB{testing: t, affected: 1}
			switch outcome {
			case "bound":
				db.ids = []int64{3, 8}
			case "zero rows":
				db.affected = 0
			case "delete error":
				db.writeErr = errors.New("delete failed")
			case "commit error":
				db.commitErr = errors.New("commit failed")
			}
			calls := 0
			engine := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if !db.committed || request.Method != http.MethodDelete || request.URL.Path != "/api/v1/admin/proxies/91" {
					t.Errorf("external cleanup before database commit or wrong request: %s %s", request.Method, request.URL.Path)
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer engine.Close()
			service := &ProxyService{DB: db, Admin: adminclient.New(engine.URL, "test", time.Second)}
			err := service.DeleteSeller(context.Background(), 7, 1)
			if outcome == "success" {
				if err != nil || calls != 1 {
					t.Fatalf("successful delete must clean engine: calls=%d err=%v", calls, err)
				}
			} else if err == nil || calls != 0 {
				t.Fatalf("failed delete must preserve engine: calls=%d err=%v", calls, err)
			}
		})
	}
}
