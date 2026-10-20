package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/yx-token/idlicat/market/internal/app"
	"github.com/yx-token/idlicat/market/internal/platform/auth"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

// ServeAdmin 用现有管理处理函数执行一次请求。引擎进程通过它承接尚未单独重写的闲猫管理接口。
func ServeAdmin(a *app.App, method, rest, rawQuery string, body []byte, adminID int64) (int, []byte) {
	s := &Server{
		DB: a.DB, Issuer: a.Issuer, Accounts: a.Accounts, IAM: a.IAM,
		Listings: a.Listings, Proxies: a.Proxies, Pools: a.Pools, Keys: a.Keys,
		Funds: a.Funds, Payments: a.Payments, Market: a.Market, Quality: a.Quality,
		Replica: a.Replica, Risk: a.Risk, Kicker: a.Kicker, Log: a.Log,
	}
	mux := http.NewServeMux()
	for _, rt := range s.adminRoutes() {
		mux.Handle(rt.pattern, rt.h)
	}
	path := "/api/admin/" + strings.TrimPrefix(rest, "/")
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.WithUser(req.Context(), auth.Claims{UserID: adminID, IsAdmin: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}
