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

	"github.com/DueGin/token-market/internal/platform/randx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// 网关每次请求固定的用量，价格按 Claude Sonnet 的官方价（输入 $3 / 输出 $15 每百万 token）
	InputTokens  = 100000
	OutputTokens = 20000
	TotalCost    = (InputTokens*3.0 + OutputTokens*15.0) / 1e6
)

type Server struct {
	DB       *pgxpool.Pool
	AdminKey string
	Version  string

	HTTP    *httptest.Server
	handler http.Handler

	mu       sync.Mutex
	idem     map[string]cached
	tokens   map[string]int64 // access token → user id
	refresh  map[string]int64
	sessions map[string]bool
	failing  map[int64]int // account id → 网关对该账号返回的状态码
	requests []string      // 网关收到的请求的 X-Client-Request-ID
	answer   func(accountID int64, prompt string) string
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
		sessions: map[string]bool{}, failing: map[int64]int{},
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
			if r.Header.Get("x-api-key") != s.AdminKey {
				writeErr(w, &apiErr{401, "UNAUTHORIZED", "invalid admin api key"})
				return
			}
			s.serve(w, r, h)
		})
	}
	admin("GET /api/v1/admin/system/version", func(http.ResponseWriter, *http.Request) (any, error) {
		return map[string]string{"version": s.Version}, nil
	})
	admin("POST /api/v1/admin/users", s.createUser)
	admin("PUT /api/v1/admin/users/{id}", s.updateUser)
	admin("POST /api/v1/admin/users/{id}/balance", s.idempotent(s.updateBalance))
	admin("POST /api/v1/admin/groups", s.createGroup)
	admin("PUT /api/v1/admin/groups/{id}", s.updateGroup)
	admin("DELETE /api/v1/admin/groups/{id}", s.softDelete("groups"))
	admin("POST /api/v1/admin/proxies", s.idempotent(s.createProxy))
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
	admin("POST /api/v1/admin/accounts/generate-auth-url", s.authURL)
	admin("POST /api/v1/admin/openai/generate-auth-url", s.authURL)
	admin("POST /api/v1/admin/accounts/exchange-code", s.exchange)
	admin("POST /api/v1/admin/openai/exchange-code", s.exchange)

	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.login) })
	mux.HandleFunc("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.doRefresh) })
	mux.HandleFunc("POST /api/v1/keys", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.createKey) })
	mux.HandleFunc("PUT /api/v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.updateKey) })
	mux.HandleFunc("DELETE /api/v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, s.deleteKey) })

	mux.HandleFunc("POST /v1/messages", s.gateway)
	mux.HandleFunc("POST /v1/chat/completions", s.gateway)
	mux.HandleFunc("POST /v1/responses", s.gateway)
	return mux
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
		if _, err := s.DB.Exec(r.Context(), "DELETE FROM user_allowed_groups WHERE user_id = $1", id); err != nil {
			return nil, err
		}
		for _, g := range *in.AllowedGroups {
			if _, err := s.DB.Exec(r.Context(), "INSERT INTO user_allowed_groups (user_id, group_id) VALUES ($1, $2)", id, g); err != nil {
				return nil, err
			}
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
		creds, _ = json.Marshal(in.Credentials)
	}
	if in.Extra != nil {
		extra, _ = json.Marshal(in.Extra)
	}
	op := "COALESCE($9::jsonb, extra)"
	if mergeJSON {
		op = "COALESCE(extra, '{}'::jsonb) || COALESCE($9::jsonb, '{}'::jsonb)"
	}
	var status *string
	if in.Status != "" {
		status = &in.Status
	}
	if _, err := s.DB.Exec(r.Context(), `UPDATE accounts SET
			concurrency = COALESCE($2, concurrency), priority = COALESCE($3, priority),
			rate_multiplier = COALESCE($4, rate_multiplier), load_factor = COALESCE($5, load_factor),
			schedulable = COALESCE($6, schedulable), status = COALESCE($7, status),
			credentials = COALESCE(credentials, '{}'::jsonb) || COALESCE($8::jsonb, '{}'::jsonb),
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
	for _, id := range in.AccountIDs {
		if err := s.applyAccount(r, id, in.accountInput, true); err != nil {
			return nil, err
		}
	}
	return map[string]any{"success": len(in.AccountIDs), "failed": 0}, nil
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
	return map[string]any{"provider": a.Platform, "success": true, "credential_valid": true, "plan_level": "pro",
		"tiers": []map[string]any{{"window": "5h", "used_percent": 10.0, "reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}}, nil
}

// ---- 授权 ----

func (s *Server) authURL(_ http.ResponseWriter, _ *http.Request) (any, error) {
	sid := "sess-" + randx.String(12)
	state := "st-" + randx.String(8)
	s.mu.Lock()
	s.sessions[sid] = true
	s.mu.Unlock()
	return map[string]any{"auth_url": "https://auth.example.test/authorize?state=" + state, "session_id": sid, "state": state}, nil
}

// exchange 用授权码换令牌。授权码即账号身份：同一授权码代表同一个官方账号，便于测试重复挂载。
func (s *Server) exchange(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		SessionID string `json:"session_id"`
		Code      string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	ok := s.sessions[in.SessionID]
	delete(s.sessions, in.SessionID)
	s.mu.Unlock()
	if !ok {
		return nil, badRequest("session expired")
	}
	if in.Code == "bad" {
		return nil, badRequest("invalid code")
	}
	return map[string]any{
		"access_token": "at-" + randx.String(16), "refresh_token": "rt-" + randx.String(16), "token_type": "Bearer",
		"expires_in": 3600, "expires_at": time.Now().Add(time.Hour).Unix(),
		"org_uuid": "org-" + in.Code, "account_uuid": "acct-" + in.Code, "email_address": in.Code + "@example.test",
		"chatgpt_account_id": "acct-" + in.Code, "email": in.Code + "@example.test", "plan_type": "plus",
	}, nil
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

func (s *Server) login(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	var id int64
	var pw string
	err := s.DB.QueryRow(r.Context(), "SELECT id, password_hash FROM users WHERE email = $1 AND deleted_at IS NULL", in.Email).Scan(&id, &pw)
	if err != nil || pw != in.Password {
		return nil, &apiErr{401, "INVALID_CREDENTIALS", "invalid email or password"}
	}
	return s.issue(id), nil
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
	var body struct {
		Model    string `json:"model"`
		Input    any    `json:"input"`
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	prompt, _ := body.Input.(string)
	if len(body.Messages) > 0 {
		prompt, _ = body.Messages[len(body.Messages)-1].Content.(string)
	}
	if body.Model == "" {
		body.Model = "claude-sonnet-4-5"
	}
	var groupRate float64
	if err := s.DB.QueryRow(ctx, "SELECT rate_multiplier::float8 FROM groups WHERE id = $1", *groupID).Scan(&groupRate); err != nil {
		gatewayError(w, 500, "api_error", err.Error())
		return
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id, a.rate_multiplier::float8 FROM accounts a JOIN account_groups ag ON ag.account_id = a.id
		WHERE ag.group_id = $1 AND a.deleted_at IS NULL AND a.status = 'active' AND a.schedulable
		ORDER BY a.priority, a.id`, *groupID)
	if err != nil {
		gatewayError(w, 500, "api_error", err.Error())
		return
	}
	type cand struct {
		id   int64
		rate float64
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.rate); err != nil {
			rows.Close()
			gatewayError(w, 500, "api_error", err.Error())
			return
		}
		cands = append(cands, c)
	}
	rows.Close()
	lastStatus := 503
	for _, c := range cands {
		s.mu.Lock()
		failStatus := s.failing[c.id]
		s.mu.Unlock()
		if failStatus != 0 {
			lastStatus = failStatus
			continue
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
			userID, keyID, c.id, *groupID, body.Model, "client:"+rid, InputTokens, OutputTokens, TotalCost, actual, groupRate, c.rate)
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
		text := "ok"
		s.mu.Lock()
		answer := s.answer
		s.mu.Unlock()
		if answer != nil {
			text = answer(c.id, prompt)
		}
		writeJSON(w, 200, responseBody(r.URL.Path, rid, body.Model, text))
		return
	}
	gatewayError(w, lastStatus, "overloaded_error", fmt.Sprintf("no available accounts in group %d", *groupID))
}

// responseBody 按接口协议构造非流式响应（探测在拿不到 SSE 时按 JSON 解析）。
func responseBody(path, rid, model, text string) map[string]any {
	switch path {
	case "/v1/chat/completions":
		return map[string]any{
			"id": "chatcmpl-" + rid, "object": "chat.completion", "model": model,
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": InputTokens, "completion_tokens": OutputTokens},
		}
	case "/v1/responses":
		return map[string]any{
			"id": "resp_" + rid, "object": "response", "model": model, "status": "completed",
			"output": []map[string]any{{"type": "message", "content": []map[string]string{{"type": "output_text", "text": text}}}},
			"usage":  map[string]any{"input_tokens": InputTokens, "output_tokens": OutputTokens},
		}
	default:
		return map[string]any{
			"id": "msg_" + rid, "type": "message", "role": "assistant", "model": model,
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": InputTokens, "output_tokens": OutputTokens},
		}
	}
}
