// Package fakesub2api 是端到端测试用的 sub2api 替身。
//
// 它在一个应用了 sub2api 真实迁移的数据库上工作：Admin API、用户 API 和网关的写入都落到真实表结构里，
// 本系统的只读访问层（replica）因此可以照常读取。只实现本系统用到的接口，行为对照 sub2api 0.2.14：
// 响应信封、幂等键要求、自定义 key 冲突、X-Client-Request-ID、失败请求不记用量、扣费可扣成负数。
package fakesub2api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/randx"
)

const (
	// 账单测试固定的用量，价格按 Claude Sonnet 的官方价（输入 $3 / 输出 $15 每百万 token）。
	// 网关响应另按实际请求估算用量，不改变既有账单断言。
	InputTokens  = 100000
	OutputTokens = 20000
	TotalCost    = (InputTokens*3.0 + OutputTokens*15.0) / 1e6
)

type Server struct {
	DB       *pgxpool.Pool
	AdminKey string
	Version  string
	// DefaultResetCredits 是没有用 SetResetCredits 设定过的账号的重置卡张数（开发环境用于演示，测试默认 0）
	DefaultResetCredits int

	HTTP    *httptest.Server
	handler http.Handler

	mu                sync.Mutex
	idem              map[string]cached
	tokens            map[string]int64 // access token → user id
	refresh           map[string]int64
	sessions          map[string]oauthSession
	failing           map[int64]int // account id → 网关对该账号返回的状态码
	requests          []string      // 网关收到的请求的 X-Client-Request-ID
	answer            func(accountID int64, prompt string) string
	credits           map[int64]int // account id → 可用的重置卡张数
	resets            map[int64]int // account id → 实际消耗的重置卡张数
	recovers          map[int64]int // account id → 恢复账号状态的次数
	active            map[int64]int // account id → 正在转发的请求数
	gatewayConfig     map[int64]GatewayConfig
	gatewayAnswer     func(accountID int64, request GatewayRequest) GatewayResponse
	registeredAnswers map[string]GatewayResponse
	incorrectAnswers  map[int64]bool
	accountModels     map[int64][]string
	modelSyncFailures map[int64]int
	modelGetFailures  map[int64]int
	modelSyncRequests map[int64]int
	modelGetRequests  map[int64]int
	nextFailures      map[string][]int
}

type cached struct {
	status int
	body   []byte
}

// New 创建替身并在随机端口上启动（测试用）。
func New(db *pgxpool.Pool, adminKey string) *Server {
	s := NewUnstarted(db, adminKey)
	s.HTTP = httptest.NewServer(s.Handler())
	return s
}

// NewUnstarted 创建替身但不监听端口，由调用方用 Handler 自行提供服务（开发环境的 cmd/fake-sub2api）。
func NewUnstarted(db *pgxpool.Pool, adminKey string) *Server {
	s := &Server{
		DB: db, AdminKey: adminKey, Version: "0.2.14",
		idem: map[string]cached{}, tokens: map[string]int64{}, refresh: map[string]int64{},
		sessions: map[string]oauthSession{}, failing: map[int64]int{},
		credits: map[int64]int{}, resets: map[int64]int{}, recovers: map[int64]int{},
		active: map[int64]int{}, gatewayConfig: map[int64]GatewayConfig{},
		accountModels: map[int64][]string{}, modelSyncFailures: map[int64]int{}, modelGetFailures: map[int64]int{},
		modelSyncRequests: map[int64]int{}, modelGetRequests: map[int64]int{},
		nextFailures: map[string][]int{},
	}
	s.handler = s.routes()
	return s
}

// Handler 返回替身的 HTTP 处理器（Admin API、用户 API、网关）。
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Close() {
	if s.HTTP != nil {
		s.HTTP.Close()
	}
}

func (s *Server) URL() string { return s.HTTP.URL }

// FailAccount 让网关对某账号返回给定状态码（0 表示恢复正常）。
func (s *Server) FailAccount(accountID int64, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == 0 {
		delete(s.failing, accountID)
	} else {
		s.failing[accountID] = status
	}
}

// SetResponder 设置网关的回答：按实际服务的账号与用户输入返回文本（默认回答 "ok"）。
func (s *Server) SetResponder(fn func(accountID int64, prompt string) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answer = fn
}

// GatewayRequests 返回网关已分配的请求 ID。
func (s *Server) GatewayRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	admin := func(pattern string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !s.allowAdmin(r) {
				writeErr(w, &apiErr{401, "UNAUTHORIZED", "invalid admin api key"})
				return
			}
			s.serve(w, r, h)
		})
	}
	admin("GET /api/v1/admin/system/version", func(http.ResponseWriter, *http.Request) (any, error) {
		return map[string]string{"version": s.Version}, nil
	})
	// 市场控制台已登录管理员换发引擎会话（x-api-key），避免二次登录
	admin("POST /api/v1/admin/auth/session", s.adminAuthSession)
	admin("GET /api/v1/admin/accounts", s.listAccounts)
	admin("GET /api/v1/admin/groups", s.listGroups)
	admin("GET /api/v1/admin/users", s.listUsers)
	admin("GET /api/v1/admin/proxies", s.listProxies)
	admin("POST /api/v1/admin/users", s.createUser)
	admin("PUT /api/v1/admin/users/{id}", s.updateUser)
	admin("POST /api/v1/admin/users/{id}/balance", s.idempotent(s.updateBalance))
	admin("POST /api/v1/admin/groups", s.createGroup)
	admin("PUT /api/v1/admin/groups/{id}", s.updateGroup)
	admin("DELETE /api/v1/admin/groups/{id}", s.softDelete("groups"))
	admin("POST /api/v1/admin/proxies", s.idempotent(s.createProxy))
	admin("POST /api/v1/admin/proxies/{id}/test", s.testProxy)
	admin("POST /api/v1/admin/proxies/{id}/quality-check", s.checkProxyQuality)
	admin("DELETE /api/v1/admin/proxies/{id}", s.softDelete("proxies"))
	admin("POST /api/v1/admin/accounts", s.idempotent(s.createAccount))
	admin("PUT /api/v1/admin/accounts/{id}", s.updateAccount)
	admin("POST /api/v1/admin/accounts/bulk-update", s.bulkUpdate)
	admin("POST /api/v1/admin/accounts/{id}/schedulable", s.setSchedulable)
	admin("DELETE /api/v1/admin/accounts/{id}", s.softDelete("accounts"))
	admin("POST /api/v1/admin/accounts/{id}/set-privacy", s.setPrivacy)
	admin("POST /api/v1/admin/accounts/{id}/clear-error", s.clearError)
	admin("GET /api/v1/admin/channels/model-pricing", s.modelPricing)
	admin("GET /api/v1/admin/cn-providers/accounts/{id}/quota", s.cnQuota)
	admin("GET /api/v1/admin/accounts/{id}/usage", s.accountUsage)
	admin("POST /api/v1/admin/accounts/{id}/models/sync-upstream", s.syncAccountModels)
	admin("GET /api/v1/admin/accounts/{id}/models", s.getAccountModels)
	admin("GET /api/v1/admin/accounts/{id}", func(_ http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.getAccount(r, id)
	})
	admin("POST /api/v1/admin/accounts/{id}/recover-state", s.recoverState)
	admin("POST /api/v1/admin/openai/accounts/{id}/quota/refresh", s.openAIQuotaRefresh)
	admin("POST /api/v1/admin/openai/accounts/{id}/reset-quota", s.openAIResetQuota)
	admin("GET /api/v1/admin/accounts/{id}/claude/reset-credits", s.claudeResetCredits)
	admin("POST /api/v1/admin/accounts/{id}/claude/reset-credits/redeem", s.idempotent(s.claudeRedeem))
	admin("POST /api/v1/admin/accounts/generate-auth-url", s.authURL)
	admin("POST /api/v1/admin/openai/generate-auth-url", s.authURL)
	admin("POST /api/v1/admin/accounts/exchange-code", s.exchange)
	admin("POST /api/v1/admin/openai/exchange-code", s.exchange)
	admin("POST /api/v1/admin/accounts/generate-setup-token-url", s.authURL)
	admin("POST /api/v1/admin/accounts/exchange-setup-token-code", s.exchange)
	for _, p := range []string{"gemini", "antigravity", "grok"} {
		admin("POST /api/v1/admin/"+p+"/oauth/auth-url", s.authURL)
		admin("POST /api/v1/admin/"+p+"/oauth/exchange-code", s.exchange)
	}
	admin("POST /api/v1/admin/kimi/oauth/device-code", s.codingOAuthStart)
	admin("POST /api/v1/admin/kimi/oauth/poll", s.codingOAuthPoll)
	admin("POST /api/v1/admin/zhipu/oauth/init", s.codingOAuthStart)
	admin("POST /api/v1/admin/zhipu/oauth/poll", s.codingOAuthPoll)
	admin("POST /api/v1/admin/zhipu/oauth/exchange-code", s.codingOAuthExchange)

	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.login) })
	mux.HandleFunc("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.doRefresh) })
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.authMe) })
	mux.HandleFunc("GET /api/v1/settings/public", func(w http.ResponseWriter, r *http.Request) {
		s.serve(w, r, func(http.ResponseWriter, *http.Request) (any, error) {
			return map[string]any{
				"site_name": "闲猫", "version": s.Version,
				"backend_mode_enabled": false, "registration_enabled": false,
				"linuxdo_oauth_enabled": false, "wechat_oauth_enabled": false,
				"oidc_oauth_enabled": false, "github_oauth_enabled": false, "google_oauth_enabled": false,
				"balance_low_notify_enabled": false, "account_quota_notify_enabled": false,
				"channel_monitor_enabled": false, "available_channels_enabled": false,
				"subscription_enabled": true, "payment_balance_disabled": false,
				"model_plaza_enabled": false, "model_plaza_require_auth": false,
				"plugin_management_enabled": false, "service_quota_enabled": false, "affiliate_enabled": false,
				"custom_menu_items": []any{},
			}, nil
		})
	})
	// 开发替身：未实现的管理读接口返回空数据，避免页面整片报错
	admin("GET /api/v1/admin/dashboard/stats", s.stubDashboardStats)
	admin("GET /api/v1/admin/dashboard/realtime", s.stubRealtime)
	admin("GET /api/v1/admin/dashboard/trend", s.stubTrend)
	admin("GET /api/v1/admin/dashboard/models", s.stubModels)
	admin("GET /api/v1/admin/dashboard/{rest...}", s.stubEmptyList)
	admin("GET /api/v1/admin/settings", s.stubSettings)
	admin("PUT /api/v1/admin/settings", s.stubSettings)
	admin("GET /api/v1/admin/compliance", s.stubCompliance)
	admin("POST /api/v1/admin/compliance/accept", s.stubCompliance)
	admin("GET /api/v1/admin/usage/{rest...}", s.stubEmptyList)
	admin("GET /api/v1/admin/announcements", s.stubEmptyList)
	admin("GET /api/v1/admin/audit-logs", s.stubEmptyList)
	admin("GET /api/v1/admin/{rest...}", s.stubEmptyList)

	mux.HandleFunc("POST /api/v1/keys", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.createKey) })
	mux.HandleFunc("PUT /api/v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.updateKey) })
	mux.HandleFunc("DELETE /api/v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.deleteKey) })

	mux.HandleFunc("POST /v1/messages", s.gateway)
	mux.HandleFunc("POST /v1/chat/completions", s.gateway)
	mux.HandleFunc("POST /v1/responses", s.gateway)
	mux.HandleFunc("POST /v1beta/models/{action}", s.gateway) // Gemini 原生协议（Gemini、Antigravity）
	return mux
}

func (s *Server) stubDashboardStats(http.ResponseWriter, *http.Request) (any, error) {
	var accounts, users int
	_ = s.DB.QueryRow(context.Background(), `SELECT count(*) FROM accounts WHERE deleted_at IS NULL`).Scan(&accounts)
	_ = s.DB.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&users)
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"total_users": users, "today_new_users": 0, "active_users": 0, "hourly_active_users": 0,
		"stats_updated_at": now, "stats_stale": false,
		"total_api_keys": 0, "active_api_keys": 0,
		"total_accounts": accounts, "normal_accounts": accounts, "error_accounts": 0,
		"ratelimit_accounts": 0, "overload_accounts": 0,
		"total_requests": 0, "total_input_tokens": 0, "total_output_tokens": 0,
		"total_cache_creation_tokens": 0, "total_cache_read_tokens": 0, "total_tokens": 0,
		"total_cost": 0, "total_actual_cost": 0, "total_account_cost": 0,
		"today_requests": 0, "today_input_tokens": 0, "today_output_tokens": 0,
		"today_cache_creation_tokens": 0, "today_cache_read_tokens": 0, "today_tokens": 0,
		"today_cost": 0, "today_actual_cost": 0, "today_account_cost": 0,
	}, nil
}

func (s *Server) stubRealtime(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{"active_requests": 0, "requests_per_minute": 0, "average_response_time": 0, "error_rate": 0}, nil
}

func (s *Server) stubTrend(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{"trend": []any{}, "start_date": "", "end_date": "", "granularity": "day"}, nil
}

func (s *Server) stubModels(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{"models": []any{}, "items": []any{}}, nil
}

func (s *Server) stubEmptyList(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 20}, nil
}

func (s *Server) stubSettings(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{
		"site_name": "闲猫", "site_subtitle": "订阅额度集市", "version": s.Version,
		"registration_enabled": false, "backend_mode_enabled": false,
	}, nil
}

func (s *Server) stubCompliance(http.ResponseWriter, *http.Request) (any, error) {
	return map[string]any{"required": false, "accepted": true}, nil
}

// ---- 信封 ----

type apiErr struct {
	status int
	code   string
	msg    string
}

func (e *apiErr) Error() string { return e.msg }

func badRequest(msg string) error { return &apiErr{400, "BAD_REQUEST", msg} }
func notFound(msg string) error   { return &apiErr{404, "NOT_FOUND", msg} }

func (s *Server) serve(w http.ResponseWriter, r *http.Request, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
	if status := s.nextFailure(r.Method, r.URL.Path); status != 0 {
		writeErr(w, &apiErr{status, "SIMULATED_FAILURE", "模拟接口暂时失败"})
		return
	}
	data, err := h(w, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"code": 0, "message": "success", "data": data})
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiErr
	if !errors.As(err, &ae) {
		ae = &apiErr{500, "INTERNAL", err.Error()}
	}
	writeJSON(w, ae.status, map[string]any{"code": ae.status, "message": ae.msg, "reason": ae.code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return badRequest("invalid json: " + err.Error())
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, badRequest("invalid id")
	}
	return id, nil
}

// idempotent 要求 Idempotency-Key，并对同一个键返回首次结果。
func (s *Server) idempotent(h func(w http.ResponseWriter, r *http.Request) (any, error)) func(w http.ResponseWriter, r *http.Request) (any, error) {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			return nil, &apiErr{400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required"}
		}
		ck := r.Method + " " + r.URL.Path + " " + key
		s.mu.Lock()
		c, ok := s.idem[ck]
		s.mu.Unlock()
		if ok {
			return json.RawMessage(c.body), nil
		}
		data, err := h(w, r)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(data)
		s.mu.Lock()
		s.idem[ck] = cached{200, raw}
		s.mu.Unlock()
		return json.RawMessage(raw), nil
	}
}

// ---- 用户 ----

type userOut struct {
	ID          int64   `json:"id"`
	Email       string  `json:"email"`
	Balance     float64 `json:"balance"`
	Concurrency int     `json:"concurrency"`
	Status      string  `json:"status"`
}

func (s *Server) getUser(r *http.Request, id int64) (*userOut, error) {
	var u userOut
	err := s.DB.QueryRow(r.Context(), `SELECT id, email, balance::float8, concurrency, status FROM users WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&u.ID, &u.Email, &u.Balance, &u.Concurrency, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("user not found")
	}
	return &u, err
}

func (s *Server) createUser(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email       string   `json:"email"`
		Password    string   `json:"password"`
		Username    string   `json:"username"`
		Notes       string   `json:"notes"`
		Balance     *float64 `json:"balance"`
		Concurrency int      `json:"concurrency"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Email == "" || len(in.Password) < 6 {
		return nil, badRequest("email and password are required")
	}
	bal := 0.0
	if in.Balance != nil {
		bal = *in.Balance
	}
	if in.Concurrency <= 0 {
		in.Concurrency = 5
	}
	var id int64
	// 测试替身直接保存明文密码，登录时比对
	err := s.DB.QueryRow(r.Context(), `INSERT INTO users (email, password_hash, username, notes, balance, concurrency, status, role)
		VALUES ($1, $2, $3, $4, $5, $6, 'active', 'user') RETURNING id`, in.Email, in.Password, in.Username, in.Notes, bal, in.Concurrency).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, &apiErr{409, "EMAIL_EXISTS", "email already exists"}
		}
		return nil, err
	}
	return s.getUser(r, id)
}

func (s *Server) updateUser(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Status        string   `json:"status"`
		Concurrency   *int     `json:"concurrency"`
		AllowedGroups *[]int64 `json:"allowed_groups"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if _, err := s.getUser(r, id); err != nil {
		return nil, err
	}
	if in.Status != "" {
		if _, err := s.DB.Exec(r.Context(), "UPDATE users SET status = $2 WHERE id = $1", id, in.Status); err != nil {
			return nil, err
		}
	}
	if in.Concurrency != nil {
		if _, err := s.DB.Exec(r.Context(), "UPDATE users SET concurrency = $2 WHERE id = $1", id, *in.Concurrency); err != nil {
			return nil, err
		}
	}
	if in.AllowedGroups != nil {
		// 整体替换放在一个事务里，并发创建 key 时不会看到中间的空集合（与 sub2api 一致）
		tx, err := s.DB.Begin(r.Context())
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		if _, err := tx.Exec(r.Context(), "DELETE FROM user_allowed_groups WHERE user_id = $1", id); err != nil {
			return nil, err
		}
		for _, g := range *in.AllowedGroups {
			if _, err := tx.Exec(r.Context(), "INSERT INTO user_allowed_groups (user_id, group_id) VALUES ($1, $2)", id, g); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			return nil, err
		}
	}
	return s.getUser(r, id)
}

func (s *Server) updateBalance(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Balance   float64 `json:"balance"`
		Operation string  `json:"operation"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	u, err := s.getUser(r, id)
	if err != nil {
		return nil, err
	}
	switch in.Operation {
	case "add":
		_, err = s.DB.Exec(r.Context(), "UPDATE users SET balance = balance + $2 WHERE id = $1", id, in.Balance)
	case "subtract":
		if u.Balance < in.Balance {
			return nil, badRequest("insufficient balance")
		}
		_, err = s.DB.Exec(r.Context(), "UPDATE users SET balance = balance - $2 WHERE id = $1", id, in.Balance)
	default:
		return nil, badRequest("invalid operation")
	}
	if err != nil {
		return nil, err
	}
	return s.getUser(r, id)
}

// ---- 分组 ----

func (s *Server) getGroup(r *http.Request, id int64) (map[string]any, error) {
	var name, platform string
	var rate float64
	var exclusive bool
	err := s.DB.QueryRow(r.Context(), `SELECT name, platform, rate_multiplier::float8, is_exclusive FROM groups WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&name, &platform, &rate, &exclusive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("group not found")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "platform": platform, "rate_multiplier": rate, "is_exclusive": exclusive}, nil
}

func (s *Server) createGroup(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Name                 string  `json:"name"`
		Description          string  `json:"description"`
		Platform             string  `json:"platform"`
		RateMultiplier       float64 `json:"rate_multiplier"`
		IsExclusive          bool    `json:"is_exclusive"`
		ProfitControlEnabled bool    `json:"profit_control_enabled"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Name == "" || in.Platform == "" {
		return nil, badRequest("name and platform are required")
	}
	var id int64
	err := s.DB.QueryRow(r.Context(), `INSERT INTO groups (name, description, platform, rate_multiplier, is_exclusive, profit_control_enabled)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, in.Name, in.Description, in.Platform, in.RateMultiplier, in.IsExclusive, in.ProfitControlEnabled).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, &apiErr{409, "GROUP_EXISTS", "group name already exists"}
		}
		return nil, err
	}
	return s.getGroup(r, id)
}

func (s *Server) updateGroup(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		RateMultiplier       *float64 `json:"rate_multiplier"`
		IsExclusive          *bool    `json:"is_exclusive"`
		ProfitControlEnabled *bool    `json:"profit_control_enabled"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if _, err := s.getGroup(r, id); err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(r.Context(), `UPDATE groups SET rate_multiplier = COALESCE($2, rate_multiplier),
		is_exclusive = COALESCE($3, is_exclusive), profit_control_enabled = COALESCE($4, profit_control_enabled) WHERE id = $1`,
		id, in.RateMultiplier, in.IsExclusive, in.ProfitControlEnabled); err != nil {
		return nil, err
	}
	return s.getGroup(r, id)
}

func (s *Server) softDelete(table string) func(http.ResponseWriter, *http.Request) (any, error) {
	return func(_ http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		tag, err := s.DB.Exec(r.Context(), "UPDATE "+table+" SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL", id)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, notFound(table + " not found")
		}
		return nil, nil
	}
}

// ---- 代理 ----

func (s *Server) createProxy(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	var id int64
	if err := s.DB.QueryRow(r.Context(), `INSERT INTO proxies (name, protocol, host, port, username, password) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		in.Name, in.Protocol, in.Host, in.Port, in.Username, in.Password).Scan(&id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": in.Name}, nil
}

// ---- 账号 ----

type accountOut struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Concurrency int            `json:"concurrency"`
	Extra       map[string]any `json:"extra"`
}

func (s *Server) getAccount(r *http.Request, id int64) (*accountOut, error) {
	var a accountOut
	var extra []byte
	err := s.DB.QueryRow(r.Context(), `SELECT id, name, platform, type, status, schedulable, priority, concurrency, COALESCE(extra, '{}')
		FROM accounts WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&a.ID, &a.Name, &a.Platform, &a.Type, &a.Status, &a.Schedulable, &a.Priority, &a.Concurrency, &extra)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("account not found")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(extra, &a.Extra)
	return &a, nil
}

type accountInput struct {
	Name           string         `json:"name"`
	Notes          *string        `json:"notes"`
	Platform       string         `json:"platform"`
	Type           string         `json:"type"`
	Credentials    map[string]any `json:"credentials"`
	Extra          map[string]any `json:"extra"`
	ProxyID        *int64         `json:"proxy_id"`
	Concurrency    *int           `json:"concurrency"`
	Priority       *int           `json:"priority"`
	RateMultiplier *float64       `json:"rate_multiplier"`
	LoadFactor     *int           `json:"load_factor"`
	Status         string         `json:"status"`
	Schedulable    *bool          `json:"schedulable"`
	GroupIDs       *[]int64       `json:"group_ids"`
}

func (s *Server) createAccount(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Name == "" || in.Platform == "" || in.Type == "" {
		return nil, badRequest("name, platform and type are required")
	}
	creds, _ := json.Marshal(orEmpty(in.Credentials))
	extra, _ := json.Marshal(orEmpty(in.Extra))
	var id int64
	err := s.DB.QueryRow(r.Context(), `INSERT INTO accounts (name, notes, platform, type, credentials, extra, proxy_id, concurrency, priority,
			rate_multiplier, load_factor, status, schedulable)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, 3), COALESCE($9, 50), COALESCE($10, 1), $11, 'active', true) RETURNING id`,
		in.Name, in.Notes, in.Platform, in.Type, creds, extra, in.ProxyID, in.Concurrency, in.Priority, in.RateMultiplier, in.LoadFactor).Scan(&id)
	if err != nil {
		return nil, err
	}
	if in.GroupIDs != nil {
		if err := s.setGroups(r, id, *in.GroupIDs); err != nil {
			return nil, err
		}
	}
	return s.getAccount(r, id)
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func (s *Server) setGroups(r *http.Request, accountID int64, groups []int64) error {
	if _, err := s.DB.Exec(r.Context(), "DELETE FROM account_groups WHERE account_id = $1", accountID); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := s.DB.Exec(r.Context(), "INSERT INTO account_groups (account_id, group_id) VALUES ($1, $2)", accountID, g); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) applyAccount(r *http.Request, id int64, in accountInput, mergeJSON bool) error {
	if _, err := s.getAccount(r, id); err != nil {
		return err
	}
	var creds, extra []byte
	if in.Credentials != nil {
		credentials := in.Credentials
		if !mergeJSON {
			// v0.2.14 的 PUT 保留未提交的敏感凭证，其他键由新对象决定。
			if len(credentials) == 0 {
				credentials = nil
			} else {
				existing, err := s.AccountCredentials(id)
				if err != nil {
					return err
				}
				credentials = mergeSensitiveAccountCredentials(existing, credentials)
			}
		}
		if credentials != nil {
			creds, _ = json.Marshal(credentials)
		}
	}
	if in.Extra != nil {
		extra, _ = json.Marshal(in.Extra)
	}
	op := "COALESCE($9::jsonb, extra)"
	credentialsOp := "COALESCE($8::jsonb, credentials)"
	if mergeJSON {
		op = "COALESCE(extra, '{}'::jsonb) || COALESCE($9::jsonb, '{}'::jsonb)"
		credentialsOp = "COALESCE(credentials, '{}'::jsonb) || COALESCE($8::jsonb, '{}'::jsonb)"
	}
	var status *string
	if in.Status != "" {
		status = &in.Status
	}
	if _, err := s.DB.Exec(r.Context(), `UPDATE accounts SET
			concurrency = COALESCE($2, concurrency), priority = COALESCE($3, priority),
			rate_multiplier = COALESCE($4, rate_multiplier), load_factor = COALESCE($5, load_factor),
			schedulable = COALESCE($6, schedulable), status = COALESCE($7, status),
			credentials = `+credentialsOp+`,
			extra = `+op+`, proxy_id = COALESCE($10, proxy_id), updated_at = now()
		WHERE id = $1`, id, in.Concurrency, in.Priority, in.RateMultiplier, in.LoadFactor, in.Schedulable, status, creds, extra, in.ProxyID); err != nil {
		return err
	}
	if in.GroupIDs != nil {
		return s.setGroups(r, id, *in.GroupIDs)
	}
	return nil
}

func (s *Server) updateAccount(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in accountInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := s.applyAccount(r, id, in, false); err != nil {
		return nil, err
	}
	return s.getAccount(r, id)
}

func (s *Server) bulkUpdate(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountIDs []int64 `json:"account_ids"`
		accountInput
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if len(in.AccountIDs) == 0 {
		return nil, badRequest("account_ids is required")
	}
	successIDs, failedIDs := []int64{}, []int64{}
	results := make([]map[string]any, 0, len(in.AccountIDs))
	for _, id := range in.AccountIDs {
		if err := s.applyAccount(r, id, in.accountInput, true); err != nil {
			failedIDs = append(failedIDs, id)
			results = append(results, map[string]any{"account_id": id, "success": false, "error": err.Error()})
			continue
		}
		successIDs = append(successIDs, id)
		results = append(results, map[string]any{"account_id": id, "success": true})
	}
	return map[string]any{"success": len(successIDs), "failed": len(failedIDs), "success_ids": successIDs, "failed_ids": failedIDs, "results": results}, nil
}

func (s *Server) setSchedulable(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Schedulable bool `json:"schedulable"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := s.applyAccount(r, id, accountInput{Schedulable: &in.Schedulable}, true); err != nil {
		return nil, err
	}
	return s.getAccount(r, id)
}

func (s *Server) setPrivacy(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if err := s.applyAccount(r, id, accountInput{Extra: map[string]any{"privacy_mode": "training_off"}}, true); err != nil {
		return nil, err
	}
	return s.getAccount(r, id)
}

// SetAccountError 模拟账号授权失效（sub2api 把账号标记为 error）。
func (s *Server) SetAccountError(accountID int64, msg string) error {
	_, err := s.DB.Exec(t0(), "UPDATE accounts SET status = 'error', error_message = $2 WHERE id = $1", accountID, msg)
	return err
}

// AccountCredentials 返回账号凭证（测试断言用）。
func (s *Server) AccountCredentials(accountID int64) (map[string]any, error) {
	var raw []byte
	if err := s.DB.QueryRow(t0(), "SELECT COALESCE(credentials, '{}') FROM accounts WHERE id = $1", accountID).Scan(&raw); err != nil {
		return nil, err
	}
	var out map[string]any
	err := json.Unmarshal(raw, &out)
	return out, err
}

func t0() context.Context { return context.Background() }

func (s *Server) clearError(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if _, err := s.getAccount(r, id); err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(r.Context(), "UPDATE accounts SET status = 'active', error_message = NULL WHERE id = $1", id)
	return nil, err
}

// modelPricing 只认识 Claude Sonnet（输入 $3、输出 $15 每百万 token）。
func (s *Server) modelPricing(_ http.ResponseWriter, r *http.Request) (any, error) {
	if r.URL.Query().Get("model") != "claude-sonnet-4-5" {
		return map[string]any{"found": false}, nil
	}
	return map[string]any{"found": true, "input_price": 3e-6, "output_price": 15e-6, "cache_write_price": 3.75e-6, "cache_read_price": 0.3e-6}, nil
}

func (s *Server) cnQuota(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	a, err := s.getAccount(r, id)
	if err != nil {
		return nil, err
	}
	creds, err := s.AccountCredentials(id)
	if err != nil {
		return nil, err
	}
	if key, _ := creds["api_key"].(string); strings.HasPrefix(key, "invalid-") {
		return map[string]any{"provider": a.Platform, "success": false, "credential_valid": false, "tiers": []any{}}, nil
	}
	return map[string]any{"provider": a.Platform, "success": true, "credential_valid": true, "plan_level": "pro",
		"tiers": []map[string]any{{"window": "5h", "used_percent": 10.0, "reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}}, nil
}

// accountUsage 模拟官方订阅窗口用量：按该账号在窗口内的实际用量折算利用率（5 小时窗口按 10 美元、7 天窗口按 100 美元计）。
// 与 sub2api 一致，被动快照（source=passive）只支持 Claude 账号。
func (s *Server) accountUsage(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	a, err := s.getAccount(r, id)
	if err != nil {
		return nil, err
	}
	source := r.URL.Query().Get("source")
	if source == "passive" && a.Platform != "anthropic" {
		return nil, badRequest("passive usage only supported for Anthropic OAuth/SetupToken accounts")
	}
	window := func(d time.Duration, budget float64) (map[string]any, error) {
		var n, tokens int64
		var cost float64
		var first *time.Time
		if err := s.DB.QueryRow(r.Context(), `SELECT count(*), COALESCE(sum(input_tokens + output_tokens), 0), COALESCE(sum(total_cost), 0)::float8, min(created_at)
			FROM usage_logs WHERE account_id = $1 AND created_at > now() - make_interval(secs => $2)`, id, d.Seconds()).Scan(&n, &tokens, &cost, &first); err != nil {
			return nil, err
		}
		resets := time.Now().Add(d)
		if first != nil {
			resets = first.Add(d)
		}
		return map[string]any{"utilization": math.Min(100, math.Round(cost/budget*1000)/10), "resets_at": resets.UTC().Format(time.RFC3339),
			"remaining_seconds": int(time.Until(resets).Seconds()), "window_stats": map[string]any{"requests": n, "tokens": tokens, "cost": cost}}, nil
	}
	if source == "" {
		source = "active"
	}
	out := map[string]any{"source": source, "updated_at": time.Now().UTC().Format(time.RFC3339)}
	switch a.Platform {
	case "gemini":
		w, err := window(24*time.Hour, 20)
		if err != nil {
			return nil, err
		}
		out["gemini_pro_daily"] = w
	case "antigravity":
		out["antigravity_quota"] = map[string]any{"gemini-2.5-pro": map[string]any{"utilization": 12, "reset_time": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)}}
	default:
		five, err := window(5*time.Hour, 10)
		if err != nil {
			return nil, err
		}
		seven, err := window(7*24*time.Hour, 100)
		if err != nil {
			return nil, err
		}
		out["five_hour"], out["seven_day"] = five, seven
	}
	return out, nil
}

// ---- 授权 ----

// oauthSession 是一次授权会话；Gemini 的 oauth_type 与项目 ID 在生成授权链接时确定（与 sub2api 一致）。
type oauthSession struct {
	oauthType string
	projectID string
	provider  string
	proxyID   *int64
	polls     int
}

func (s *Server) authURL(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		OAuthType string `json:"oauth_type"`
		ProjectID string `json:"project_id"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return nil, badRequest("invalid request")
		}
	}
	if strings.Contains(r.URL.Path, "/gemini/") {
		if in.OAuthType == "" {
			in.OAuthType = "code_assist"
		}
		if in.OAuthType != "code_assist" && in.OAuthType != "google_one" && in.OAuthType != "ai_studio" {
			return nil, badRequest("Invalid oauth_type: must be 'code_assist', 'google_one', or 'ai_studio'")
		}
	}
	sid := "sess-" + randx.String(12)
	state := "st-" + randx.String(8)
	s.mu.Lock()
	s.sessions[sid] = oauthSession{oauthType: in.OAuthType, projectID: in.ProjectID}
	s.mu.Unlock()
	return map[string]any{"auth_url": "https://auth.example.test/authorize?state=" + state, "session_id": sid, "state": state}, nil
}

// exchange 用授权码换令牌。授权码即账号身份：同一授权码代表同一个官方账号，便于测试重复挂载。
// 返回字段按平台对照 sub2api 0.2.14 各厂商的令牌结构。
func (s *Server) exchange(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		SessionID string `json:"session_id"`
		Code      string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess, ok := s.sessions[in.SessionID]
	delete(s.sessions, in.SessionID)
	s.mu.Unlock()
	if !ok {
		return nil, badRequest("session expired")
	}
	if in.Code == "bad" {
		return nil, badRequest("invalid code")
	}
	tok := map[string]any{
		"access_token": "at-" + randx.String(16), "refresh_token": "rt-" + randx.String(16), "token_type": "Bearer",
		"expires_in": 3600, "expires_at": time.Now().Add(time.Hour).Unix(),
	}
	switch {
	case strings.Contains(r.URL.Path, "/gemini/"):
		project := sess.projectID
		if project == "" {
			project = "proj-" + in.Code
		}
		tier := map[string]string{"google_one": "google_ai_pro", "code_assist": "gcp_standard", "ai_studio": "aistudio_free"}[sess.oauthType]
		tok["project_id"], tok["oauth_type"], tok["tier_id"], tok["scope"] = project, sess.oauthType, tier, "cloud-platform"
	case strings.Contains(r.URL.Path, "/antigravity/"):
		tok["email"], tok["project_id"], tok["plan_type"] = in.Code+"@example.test", "proj-"+in.Code, "pro"
	case strings.Contains(r.URL.Path, "/grok/"):
		tok["sub"], tok["email"], tok["subscription_tier"], tok["entitlement_status"] = "sub-"+in.Code, in.Code+"@example.test", "supergrok", "active"
	default:
		tok["org_uuid"], tok["account_uuid"], tok["email_address"] = "org-"+in.Code, "acct-"+in.Code, in.Code+"@example.test"
		tok["chatgpt_account_id"], tok["email"], tok["plan_type"] = "acct-"+in.Code, in.Code+"@example.test", "plus"
	}
	return tok, nil
}

// ---- 用户 API ----

func (s *Server) issue(userID int64) map[string]any {
	at, rt := "uat-"+randx.String(24), "urt-"+randx.String(24)
	s.mu.Lock()
	s.tokens[at] = userID
	s.refresh[rt] = userID
	s.mu.Unlock()
	return map[string]any{"access_token": at, "refresh_token": rt, "expires_in": 3600, "token_type": "Bearer"}
}

func (s *Server) adminAuthSession(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" {
		return nil, &apiErr{400, "INVALID_REQUEST", "email required"}
	}
	var id int64
	var role, status string
	var balance float64
	var concurrency int
	err := s.DB.QueryRow(r.Context(), `SELECT id, COALESCE(role,'user'), COALESCE(status,'active'), balance::float8, concurrency
		FROM users WHERE email = $1 AND deleted_at IS NULL`, email).Scan(&id, &role, &status, &balance, &concurrency)
	if err != nil {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO users (email, password_hash, username, notes, balance, concurrency, status, role)
			VALUES ($1, $2, 'admin', 'bridge', 1000, 10, 'active', 'admin') RETURNING id`, email, "bridge-no-password").Scan(&id)
		if err != nil {
			return nil, err
		}
		role, status, balance, concurrency = "admin", "active", 1000, 10
	} else if role != "admin" {
		if _, err := s.DB.Exec(r.Context(), `UPDATE users SET role='admin', status='active' WHERE id=$1`, id); err != nil {
			return nil, err
		}
		role, status = "admin", "active"
	}
	out := s.issue(id)
	out["user"] = map[string]any{
		"id": id, "email": email, "username": email, "role": role, "status": status,
		"balance": balance, "concurrency": concurrency,
	}
	return out, nil
}

func (s *Server) allowAdmin(r *http.Request) bool {
	if r.Header.Get("x-api-key") == s.AdminKey {
		return true
	}
	id, err := s.bearerUser(r)
	if err != nil || id == 0 {
		return false
	}
	var role string
	if err := s.DB.QueryRow(r.Context(), "SELECT COALESCE(role,'') FROM users WHERE id = $1 AND deleted_at IS NULL", id).Scan(&role); err != nil {
		return false
	}
	return role == "admin"
}

func (s *Server) sessionUser(r *http.Request) (map[string]any, error) {
	id, err := s.bearerUser(r)
	if err != nil || id == 0 {
		return nil, &apiErr{401, "UNAUTHORIZED", "invalid token"}
	}
	var email, role, status string
	var balance float64
	var concurrency int
	err = s.DB.QueryRow(r.Context(), `SELECT email, COALESCE(role,'user'), COALESCE(status,'active'), balance::float8, concurrency
		FROM users WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&email, &role, &status, &balance, &concurrency)
	if err != nil {
		return nil, &apiErr{401, "UNAUTHORIZED", "invalid token"}
	}
	return map[string]any{
		"id": id, "email": email, "username": email, "role": role, "status": status,
		"balance": balance, "concurrency": concurrency,
	}, nil
}

func (s *Server) authMe(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.sessionUser(r)
}

func (s *Server) login(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	var id int64
	var pw, role, email, status string
	var balance float64
	var concurrency int
	err := s.DB.QueryRow(r.Context(), `SELECT id, password_hash, COALESCE(role,'user'), email, COALESCE(status,'active'), balance::float8, concurrency
		FROM users WHERE email = $1 AND deleted_at IS NULL`, in.Email).Scan(&id, &pw, &role, &email, &status, &balance, &concurrency)
	if err != nil || pw != in.Password {
		return nil, &apiErr{401, "INVALID_CREDENTIALS", "invalid email or password"}
	}
	out := s.issue(id)
	out["user"] = map[string]any{"id": id, "email": email, "username": email, "role": role, "balance": balance, "concurrency": concurrency, "status": status}
	return out, nil
}

func (s *Server) listAccounts(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.DB.Query(r.Context(), `SELECT id, name, platform, type, status, schedulable FROM accounts WHERE deleted_at IS NULL ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, platform, typ, status string
		var sched bool
		if err := rows.Scan(&id, &name, &platform, &typ, &status, &sched); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "name": name, "platform": platform, "type": typ, "status": status, "schedulable": sched})
	}
	return map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 200}, rows.Err()
}

func (s *Server) listGroups(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.DB.Query(r.Context(), `SELECT id, name, platform, rate_multiplier::float8 FROM groups WHERE deleted_at IS NULL ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, platform string
		var rate float64
		if err := rows.Scan(&id, &name, &platform, &rate); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "name": name, "platform": platform, "rate_multiplier": rate})
	}
	return map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 200}, rows.Err()
}

func (s *Server) listUsers(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.DB.Query(r.Context(), `SELECT id, email, COALESCE(role,'user'), COALESCE(status,'active'), balance::float8 FROM users WHERE deleted_at IS NULL ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var email, role, status string
		var balance float64
		if err := rows.Scan(&id, &email, &role, &status, &balance); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "email": email, "username": email, "role": role, "status": status, "balance": balance})
	}
	return map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 200}, rows.Err()
}

func (s *Server) listProxies(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.DB.Query(r.Context(), `SELECT id, name, protocol, host, port FROM proxies WHERE deleted_at IS NULL ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, protocol, host string
		var port int
		if err := rows.Scan(&id, &name, &protocol, &host, &port); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "name": name, "protocol": protocol, "host": host, "port": port, "status": "active"})
	}
	return map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 200}, rows.Err()
}

func (s *Server) doRefresh(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	id, ok := s.refresh[in.RefreshToken]
	delete(s.refresh, in.RefreshToken)
	s.mu.Unlock()
	if !ok {
		return nil, &apiErr{401, "INVALID_REFRESH_TOKEN", "invalid refresh token"}
	}
	return s.issue(id), nil
}

func (s *Server) bearerUser(r *http.Request) (int64, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	id, found := s.tokens[tok]
	s.mu.Unlock()
	if !ok || !found {
		return 0, &apiErr{401, "UNAUTHORIZED", "invalid token"}
	}
	return id, nil
}

func (s *Server) keyOut(r *http.Request, id int64) (map[string]any, error) {
	var userID int64
	var key, name, status string
	var groupID *int64
	err := s.DB.QueryRow(r.Context(), "SELECT user_id, key, name, group_id, status FROM api_keys WHERE id = $1 AND deleted_at IS NULL", id).
		Scan(&userID, &key, &name, &groupID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("key not found")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "user_id": userID, "key": key, "name": name, "group_id": groupID, "status": status}, nil
}

func (s *Server) createKey(_ http.ResponseWriter, r *http.Request) (any, error) {
	uid, err := s.bearerUser(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Name      string `json:"name"`
		GroupID   int64  `json:"group_id"`
		CustomKey string `json:"custom_key"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// 独占分组只有被授权的用户可以绑定
	var exclusive bool
	if err := s.DB.QueryRow(r.Context(), "SELECT is_exclusive FROM groups WHERE id = $1 AND deleted_at IS NULL", in.GroupID).Scan(&exclusive); err != nil {
		return nil, badRequest("group not found")
	}
	if exclusive {
		var allowed bool
		if err := s.DB.QueryRow(r.Context(), "SELECT EXISTS (SELECT 1 FROM user_allowed_groups WHERE user_id = $1 AND group_id = $2)", uid, in.GroupID).
			Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, &apiErr{403, "GROUP_NOT_ALLOWED", "group not allowed"}
		}
	}
	var id int64
	err = s.DB.QueryRow(r.Context(), "INSERT INTO api_keys (user_id, key, name, group_id, status) VALUES ($1, $2, $3, $4, 'active') RETURNING id",
		uid, in.CustomKey, in.Name, in.GroupID).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, &apiErr{409, "KEY_EXISTS", "custom key already exists"}
		}
		return nil, err
	}
	return s.keyOut(r, id)
}

func (s *Server) ownedKey(r *http.Request) (int64, error) {
	uid, err := s.bearerUser(r)
	if err != nil {
		return 0, err
	}
	id, err := pathID(r)
	if err != nil {
		return 0, err
	}
	var owner int64
	if err := s.DB.QueryRow(r.Context(), "SELECT user_id FROM api_keys WHERE id = $1 AND deleted_at IS NULL", id).Scan(&owner); err != nil || owner != uid {
		return 0, notFound("key not found")
	}
	return id, nil
}

func (s *Server) updateKey(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := s.ownedKey(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Status != "" {
		if _, err := s.DB.Exec(r.Context(), "UPDATE api_keys SET status = $2 WHERE id = $1", id, in.Status); err != nil {
			return nil, err
		}
	}
	return s.keyOut(r, id)
}

func (s *Server) deleteKey(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := s.ownedKey(r)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(r.Context(), "UPDATE api_keys SET deleted_at = now() WHERE id = $1", id)
	return nil, err
}

// ---- 网关 ----

func gatewayError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": msg}})
}

// gateway 模拟一次转发：鉴权 → 余额检查 → 按优先级选账号 → 成功则扣费并写用量。
// 与 sub2api 一致：每个请求都返回 X-Client-Request-ID；失败的请求不写用量、不扣费。
func (s *Server) gateway(w http.ResponseWriter, r *http.Request) {
	rid := "req-" + randx.String(20)
	w.Header().Set("X-Client-Request-ID", rid)
	s.mu.Lock()
	s.requests = append(s.requests, rid)
	s.mu.Unlock()

	key := r.Header.Get("x-api-key")
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		key = v
	}
	if v := r.Header.Get("x-goog-api-key"); v != "" {
		key = v
	}
	ctx := r.Context()
	var keyID, userID int64
	var groupID *int64
	var keyStatus, userStatus string
	var balance float64
	err := s.DB.QueryRow(ctx, `SELECT k.id, k.user_id, k.group_id, k.status, u.status, u.balance::float8
		FROM api_keys k JOIN users u ON u.id = k.user_id WHERE k.key = $1 AND k.deleted_at IS NULL AND u.deleted_at IS NULL`, key).
		Scan(&keyID, &userID, &groupID, &keyStatus, &userStatus, &balance)
	if err != nil || keyStatus != "active" || userStatus != "active" || groupID == nil {
		gatewayError(w, 401, "authentication_error", "invalid api key")
		return
	}
	if balance <= 0 {
		writeJSON(w, 403, map[string]any{"type": "error", "error": map[string]string{"type": "INSUFFICIENT_BALANCE", "message": "insufficient balance"}})
		return
	}
	var body map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 40<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		gatewayError(w, 400, "invalid_request_error", "invalid request body")
		return
	}
	request := parseGatewayRequest(body, r.PathValue("action"))
	var groupRate float64
	if err := s.DB.QueryRow(ctx, "SELECT rate_multiplier::float8 FROM groups WHERE id = $1", *groupID).Scan(&groupRate); err != nil {
		gatewayError(w, 500, "api_error", err.Error())
		return
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id, a.rate_multiplier::float8, a.concurrency, COALESCE(a.credentials, '{}') FROM accounts a JOIN account_groups ag ON ag.account_id = a.id
		WHERE ag.group_id = $1 AND a.deleted_at IS NULL AND a.status = 'active' AND a.schedulable
		ORDER BY a.priority, a.id`, *groupID)
	if err != nil {
		gatewayError(w, 500, "api_error", err.Error())
		return
	}
	type cand struct {
		id          int64
		rate        float64
		concurrency int
		credentials []byte
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.rate, &c.concurrency, &c.credentials); err != nil {
			rows.Close()
			gatewayError(w, 500, "api_error", err.Error())
			return
		}
		cands = append(cands, c)
	}
	rows.Close()
	lastStatus := 503
	concurrencyLimited := false
	for _, c := range cands {
		if !supportsAccountModel(c.credentials, request.Model) {
			continue
		}
		s.mu.Lock()
		failStatus := s.failing[c.id]
		s.mu.Unlock()
		if failStatus != 0 {
			lastStatus = failStatus
			continue
		}
		if !s.acquireAccount(c.id, c.concurrency) {
			concurrencyLimited = true
			continue
		}
		defer s.releaseAccount(c.id)
		response, ok := s.gatewayResponse(ctx, c.id, request)
		if !ok {
			return
		}
		actual := math.Round(TotalCost*groupRate*1e10) / 1e10
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			gatewayError(w, 500, "api_error", err.Error())
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO usage_logs (user_id, api_key_id, account_id, group_id, model, request_id,
				input_tokens, output_tokens, total_cost, actual_cost, rate_multiplier, account_rate_multiplier, first_token_ms, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 300, 1200)`,
			userID, keyID, c.id, *groupID, request.Model, "client:"+rid, InputTokens, OutputTokens, TotalCost, actual, groupRate, c.rate)
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE users SET balance = balance - $2 WHERE id = $1", userID, actual)
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			gatewayError(w, 500, "api_error", err.Error())
			return
		}
		writeJSON(w, 200, responseBody(r.URL.Path, rid, request.Model, response))
		return
	}
	if concurrencyLimited {
		gatewayConcurrencyError(w)
		return
	}
	gatewayError(w, lastStatus, "overloaded_error", fmt.Sprintf("no available accounts in group %d", *groupID))
}
