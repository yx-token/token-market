// Package httpx 是控制面 HTTP 接口的公共工具：统一的 JSON 响应、错误、鉴权中间件。
//
// 成功响应：{"data": ...}；错误响应：{"error": {"code": "...", "message": "..."}}。
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/auth"
	"github.com/DueGin/token-market/internal/platform/clientip"
)

// Error 是可以直接返回给调用方的业务错误。
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func BadRequest(code, msg string) *Error   { return NewError(http.StatusBadRequest, code, msg) }
func NotFound(code, msg string) *Error     { return NewError(http.StatusNotFound, code, msg) }
func Forbidden(code, msg string) *Error    { return NewError(http.StatusForbidden, code, msg) }
func Conflict(code, msg string) *Error     { return NewError(http.StatusConflict, code, msg) }
func Unauthorized(code, msg string) *Error { return NewError(http.StatusUnauthorized, code, msg) }
func TooMany(code, msg string) *Error      { return NewError(http.StatusTooManyRequests, code, msg) }
func Unavailable(code, msg string) *Error  { return NewError(http.StatusServiceUnavailable, code, msg) }

// WriteJSON 写出 {"data": v}。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

// WriteError 把错误写成统一格式；非业务错误记录日志并返回 500，不泄露内部信息。
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if !errors.As(err, &he) {
		slog.Error("http handler error", "method", r.Method, "path", r.URL.Path, "err", err)
		he = NewError(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(he.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": he})
}

// Handler 是返回错误的 HTTP 处理函数。
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

const maxBody = 1 << 20

// Decode 解析 JSON 请求体，拒绝未知字段。
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return BadRequest("INVALID_JSON", "invalid request body: "+err.Error())
	}
	return nil
}

// PathID 读取路径参数中的正整数 ID。
func PathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, BadRequest("INVALID_ID", "invalid "+name)
	}
	return id, nil
}

// QueryInt 读取查询参数中的整数，缺省时返回 def，并限制在 [min, max]。
func QueryInt(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

type ctxKey struct{}

// UserFrom 返回当前请求的登录用户。
func UserFrom(ctx context.Context) (auth.Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(auth.Claims)
	return c, ok
}

// MustUser 返回当前登录用户，中间件保证一定存在。
func MustUser(r *http.Request) auth.Claims {
	c, _ := UserFrom(r.Context())
	return c
}

// UserChecker 在令牌校验通过后确认用户仍然可用（未被冻结）。
type UserChecker func(ctx context.Context, userID int64) error

// Authenticate 校验 Bearer 令牌并把用户放入上下文。
func Authenticate(iss *auth.Issuer, check UserChecker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			WriteError(w, r, Unauthorized("UNAUTHORIZED", "missing bearer token"))
			return
		}
		claims, err := iss.Parse(token)
		if err != nil {
			WriteError(w, r, Unauthorized("UNAUTHORIZED", "invalid or expired token"))
			return
		}
		if check != nil {
			if err := check(r.Context(), claims.UserID); err != nil {
				WriteError(w, r, err)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, claims)))
	})
}

// RequireAdmin 要求当前用户是管理员，必须套在 Authenticate 内层。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := UserFrom(r.Context()); !ok || !c.IsAdmin {
			WriteError(w, r, Forbidden("FORBIDDEN", "admin only"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP 取客户端 IP：部署在负载均衡后时取 X-Forwarded-For 的第一个地址。
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type clientIPKey struct{}

// RealIP 按受信任代理规则解析客户端 IP 并放入请求上下文，ClientIP 从中读取。
// 只有直连方是受信任的代理时才采信 X-Forwarded-For，防止伪造请求头规避风控。
func RealIP(res clientip.Resolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := res.Resolve(r.RemoteAddr, r.Header.Values("X-Forwarded-For"))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}

// Recover 把 panic 转成 500，避免单个请求拖垮进程。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("http panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(rec))
				WriteError(w, r, NewError(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// AccessLog 记录请求元数据（不记录请求体）。
func AccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}
