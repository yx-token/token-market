//go:build integration

package buyer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/testutil"
)

func TestCreditShadowDoesNotInheritDefaultSignupBalance(t *testing.T) {
	_, db := testutil.NewDatabase(t)
	ctx := t.Context()
	if _, err := db.Exec(ctx, `CREATE TABLE users(id bigserial PRIMARY KEY, email text UNIQUE,
		balance numeric NOT NULL, status text DEFAULT 'active', deleted_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	const defaultGift = 37.5
	var creates atomic.Int64
	// 模拟 sub2api 的真实语义：未传 balance 时使用后台默认赠金，显式 0 时为 0。
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in adminclient.CreateUserInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		balance := defaultGift
		if in.Balance != nil {
			balance = *in.Balance
		}
		var id int64
		if err := db.QueryRow(r.Context(), `INSERT INTO users(email,balance) VALUES($1,$2) RETURNING id`, in.Email, balance).Scan(&id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		creates.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": adminclient.User{ID: id, Email: in.Email, Balance: balance, Status: "active"}})
	}))
	defer api.Close()
	reader, err := replica.New(db)
	if err != nil {
		t.Fatal(err)
	}
	s := &ShadowService{Admin: adminclient.New(api.URL, "test", time.Second), Replica: reader}
	for _, kind := range []string{KindCreditPack, KindBuyer, KindProbe} {
		it := &Identity{Kind: kind, OwnerID: 12, Email: kind + "@gift-test.local"}
		id, err := s.createUser(ctx, it, "test-password", 5)
		if err != nil {
			t.Fatal(err)
		}
		u, err := reader.UserByEmail(ctx, it.Email)
		if err != nil || u == nil {
			t.Fatalf("read %s shadow: %v", kind, err)
		}
		want := defaultGift
		if kind == KindCreditPack {
			want = 0
		}
		if u.Balance != want {
			t.Errorf("%s shadow inherited incorrect balance: got %v want %v", kind, u.Balance, want)
		}
		if again, err := s.createUser(ctx, it, "test-password", 5); err != nil || again != id {
			t.Fatalf("retry must reuse %s shadow: got %d want %d err=%v", kind, again, id, err)
		}
	}
	if creates.Load() != 3 {
		t.Fatalf("retry created duplicate shadows: %d", creates.Load())
	}
}
