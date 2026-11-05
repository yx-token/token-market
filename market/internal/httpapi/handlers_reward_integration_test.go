//go:build integration

package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yx-token/token-market/market/internal/app"
	"github.com/yx-token/token-market/market/internal/platform/auth"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/rewardtask"
)

func TestRewardTicketsThroughRegisterBindAndAdmin(t *testing.T) {
	s, pool := promoTestServer(t)
	ctx := context.Background()
	kr, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "k", Keys: map[string]string{"k": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("w", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	s.Reward = &rewardtask.Service{DB: pool, Keyring: kr}
	loxo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.URL.Path != "/api/v1/reward-open/ping" || r.Header.Get("X-Reward-Signature") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":401,"message":"bad","reason":"REWARD_SIGNATURE_INVALID"}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"message":"success","data":{"partner_name":"闲猫","app_key":"rk_live","server_time":1}}`)
	}))
	defer loxo.Close()

	var adminID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password_hash,is_admin) VALUES ('reward-admin@example.com','test',true) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := s.Issuer.Issue(auth.Claims{UserID: adminID, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: want %d got %d %s", method, path, status, rec.Code, rec.Body.String())
		}
		return rec
	}

	base := "/api/admin/reward-tasks"
	if rec := call("GET", base+"/config", adminToken, "", 200); !strings.Contains(rec.Body.String(), `"secret_set":false`) || !strings.Contains(rec.Body.String(), `"base_url":"https://loxo.cc"`) {
		t.Fatal(rec.Body.String())
	}
	call("PUT", base+"/config", adminToken, `{"enabled":true,"base_url":"https://loxo.cc","app_key":"rk_live","app_secret":"","sdk_enabled":true}`, 400)
	call("PUT", base+"/config", adminToken, `{"enabled":false,"base_url":"http://evil.example","app_key":"rk_live","app_secret":"rs_live_secret_value"}`, 400)
	call("POST", base+"/ping", adminToken, `{}`, 400) // 还没有凭证
	saved := call("PUT", base+"/config", adminToken, `{"enabled":true,"base_url":"`+loxo.URL+`","app_key":"rk_live","app_secret":"rs_live_secret_value","sdk_enabled":true}`, 200)
	if strings.Contains(saved.Body.String(), "rs_live_secret_value") || !strings.Contains(saved.Body.String(), `"secret_set":true`) {
		t.Fatalf("config response leaks or lost secret: %s", saved.Body.String())
	}
	if rec := call("POST", base+"/ping", adminToken, `{}`, 200); !strings.Contains(rec.Body.String(), `"partner_name":"闲猫"`) {
		t.Fatal(rec.Body.String())
	}

	// 注册带票据：绑定为新用户；票据格式非法不影响注册。
	const regTicket = "rt_1111111111111111111111111111111111111111"
	const loginTicket = "rt_2222222222222222222222222222222222222222"
	call("POST", "/api/auth/register", "", `{"email":"loxo-new@example.com","password":"test-password-123","reward_ticket":"`+regTicket+`"}`, 201)
	call("POST", "/api/auth/register", "", `{"email":"loxo-bad@example.com","password":"test-password-123","reward_ticket":"not-a-ticket"}`, 201)
	var newUser bool
	if err := pool.QueryRow(ctx, `SELECT new_user FROM reward_tickets WHERE ticket=$1`, regTicket).Scan(&newUser); err != nil || !newUser {
		t.Fatalf("register ticket new_user=%v err=%v", newUser, err)
	}

	// 老用户登录后绑定：不算新用户。
	login := call("POST", "/api/auth/login", "", `{"email":"loxo-bad@example.com","password":"test-password-123"}`, 200)
	var lr struct {
		Data struct {
			Token string `json:"token"`
		}
	}
	if err := json.Unmarshal(login.Body.Bytes(), &lr); err != nil || lr.Data.Token == "" {
		t.Fatalf("login: %s %v", login.Body.String(), err)
	}
	if rec := call("POST", "/api/me/reward-ticket", lr.Data.Token, `{"ticket":"`+loginTicket+`"}`, 200); !strings.Contains(rec.Body.String(), `"accepted":true`) {
		t.Fatal(rec.Body.String())
	}
	if rec := call("POST", "/api/me/reward-ticket", lr.Data.Token, `{"ticket":"`+regTicket+`"}`, 200); !strings.Contains(rec.Body.String(), `"accepted":false`) {
		t.Fatalf("someone else's ticket: %s", rec.Body.String())
	}
	call("POST", "/api/me/reward-ticket", lr.Data.Token, `{"ticket":"bad"}`, 400)
	if err := pool.QueryRow(ctx, `SELECT new_user FROM reward_tickets WHERE ticket=$1`, loginTicket).Scan(&newUser); err != nil || newUser {
		t.Fatalf("login ticket new_user=%v err=%v", newUser, err)
	}

	list := call("GET", base+"/tickets?state=pending&page=1&page_size=20", adminToken, "", 200)
	var page struct {
		Data struct {
			Items []rewardtask.TicketRow `json:"items"`
			Total int64                  `json:"total"`
		}
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || page.Data.Total != 2 || page.Data.Items[0].Ticket != loginTicket || page.Data.Items[0].UserEmail != "loxo-bad@example.com" {
		t.Fatalf("tickets: %s %v", list.Body.String(), err)
	}
	call("GET", base+"/tickets?state=bogus", adminToken, "", 400)
	call("GET", base+"/events", adminToken, "", 200)
	call("POST", base+"/events/999/retry", adminToken, "", 404)

	// 普通用户进不了管理接口。
	call("GET", base+"/config", lr.Data.Token, "", 403)

	// 引擎侧 /api/v1/admin/market/reward-tasks/* 经 relay 进同一处理函数。
	status, body := ServeAdmin(&app.App{DB: pool, Accounts: s.Accounts, Issuer: s.Issuer, Reward: s.Reward}, ctx, "GET", "reward-tasks/config", "", nil, adminID)
	if status != 200 || !strings.Contains(string(body), `"app_key":"rk_live"`) || strings.Contains(string(body), "rs_live_secret_value") {
		t.Fatalf("relay: %d %s", status, body)
	}
}
