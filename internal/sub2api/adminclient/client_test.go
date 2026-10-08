package adminclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type recorded struct {
	method, path, apiKey, idemKey string
	body                          map[string]any
}

func newServer(t *testing.T, status int, respBody string, headers map[string]string) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method, rec.path = r.Method, r.URL.Path
		rec.apiKey = r.Header.Get("x-api-key")
		rec.idemKey = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "admin-key", 5*time.Second), rec
}

func TestCreateAccountSendsAuthAndIdempotencyKey(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"message":"success","data":{"id":42,"name":"tm-u-1","platform":"anthropic"}}`, nil)
	acc, err := c.CreateAccount(context.Background(), CreateAccountInput{
		Name: "tm-u-1", Platform: "anthropic", Type: "oauth",
		Credentials: map[string]any{"access_token": "x"}, GroupIDs: []int64{7},
	}, "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID != 42 {
		t.Fatalf("id = %d", acc.ID)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/admin/accounts" {
		t.Fatalf("unexpected request %s %s", rec.method, rec.path)
	}
	if rec.apiKey != "admin-key" || rec.idemKey != "op-1" {
		t.Fatalf("headers: api key %q idem %q", rec.apiKey, rec.idemKey)
	}
	if ids, _ := rec.body["group_ids"].([]any); len(ids) != 1 {
		t.Fatalf("group_ids not sent: %v", rec.body)
	}
}

func TestIdempotencyKeyRequired(t *testing.T) {
	c, _ := newServer(t, 200, `{"code":0}`, nil)
	ctx := context.Background()
	if _, err := c.CreateAccount(ctx, CreateAccountInput{}, ""); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := c.CreateProxy(ctx, CreateProxyInput{}, ""); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("CreateProxy: %v", err)
	}
	if _, err := c.UpdateBalance(ctx, 1, BalanceAdd, 1, "", ""); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("UpdateBalance: %v", err)
	}
}

func TestUpdateBalancePayload(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"data":{"id":5,"balance":12.5}}`, nil)
	u, err := c.UpdateBalance(context.Background(), 5, BalanceAdd, 2.5, "recharge 9", "op-9")
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != 12.5 {
		t.Fatalf("balance = %v", u.Balance)
	}
	if rec.path != "/api/v1/admin/users/5/balance" || rec.body["operation"] != "add" || rec.body["balance"] != 2.5 {
		t.Fatalf("unexpected payload %s %v", rec.path, rec.body)
	}
}

func TestErrorMappingAndRetryable(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		headers   map[string]string
		retryable bool
	}{
		{"bad request", 400, `{"code":400,"message":"invalid"}`, nil, false},
		{"server error", 502, `bad gateway`, nil, true},
		{"rate limited", 429, `{"code":429,"message":"slow down"}`, nil, true},
		{"idempotency in progress", 409, `{"code":409,"message":"in progress"}`, map[string]string{"Retry-After": "1"}, true},
		{"conflict without retry", 409, `{"code":409,"message":"exists"}`, nil, false},
		{"business error with 200", 200, `{"code":1001,"message":"nope"}`, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newServer(t, tc.status, tc.body, tc.headers)
			_, err := c.GetVersion(context.Background())
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %v", err)
			}
			if apiErr.HTTPStatus != tc.status {
				t.Fatalf("status = %d", apiErr.HTTPStatus)
			}
			if IsRetryable(err) != tc.retryable {
				t.Fatalf("retryable = %v, want %v", IsRetryable(err), tc.retryable)
			}
		})
	}
}

func TestNetworkErrorIsRetryable(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", time.Second)
	_, err := c.GetVersion(context.Background())
	if err == nil || !IsRetryable(err) {
		t.Fatalf("network error should be retryable: %v", err)
	}
}

func TestBulkUpdateOmitsUnsetFields(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"data":{"success":2}}`, nil)
	empty := []int64{}
	_, err := c.BulkUpdateAccounts(context.Background(), BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2},
		GroupIDs:   &empty,
		Extra:      map[string]any{"max_sessions": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.body["priority"]; ok {
		t.Fatalf("unset priority must be omitted: %v", rec.body)
	}
	if ids, ok := rec.body["group_ids"].([]any); !ok || len(ids) != 0 {
		t.Fatalf("explicit empty group_ids must be sent to clear groups: %v", rec.body)
	}
}
