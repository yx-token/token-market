// Package httpapi 是控制面 HTTP 接口：账号、市场、买家、卖家、管理后台。
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DueGin/token-market/internal/account"
	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/market"
	"github.com/DueGin/token-market/internal/platform/auth"
	"github.com/DueGin/token-market/internal/platform/clientip"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/platform/ratelimit"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/risk"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	DB       *pgxpool.Pool
	Issuer   *auth.Issuer
	Accounts *account.Service
	Listings *listing.Service
	Proxies  *listing.ProxyService
	Pools    *scheduling.PoolService
	Keys     *buyer.KeyService
	Funds    *billing.Funds
	Market   *market.Service
	Quality  *quality.Engine
	Risk     *risk.Service
	Kicker   listing.Kicker
	Log      *slog.Logger
	// TrustedProxies 是前置负载均衡的地址，只有来自它们的请求才采信 X-Forwarded-For
	TrustedProxies []string

	// 防爆破：登录失败按 IP 与邮箱计数，注册按 IP 计数（进程内，各实例独立）
	loginFailsByIP    *ratelimit.Failures
	loginFailsByEmail *ratelimit.Failures
	registersByIP     *ratelimit.Failures
}

func (s *Server) settings(ctx context.Context) (settings.Settings, error) {
	return settings.Load(ctx, s.DB)
}

type route struct {
	pattern string
	h       httpx.Handler
}

// Handler 组装全部路由。
func (s *Server) Handler() http.Handler {
	s.loginFailsByIP = ratelimit.NewFailures(30, 15*time.Minute)
	s.loginFailsByEmail = ratelimit.NewFailures(10, 15*time.Minute)
	s.registersByIP = ratelimit.NewFailures(20, time.Hour)
	mux := http.NewServeMux()
	public := []route{
		{"GET /healthz", s.healthz},
		{"POST /api/auth/register", s.register},
		{"POST /api/auth/login", s.login},
		{"GET /api/market/prices", s.marketPrices},
		{"GET /api/market/listings", s.marketListings},
		{"GET /api/market/listings/{id}", s.marketListing},
		{"GET /api/market/pools", s.marketPools},
		{"GET /api/market/vendors", s.marketVendors},
	}
	user := []route{
		{"POST /api/auth/refresh", s.refreshToken},
		{"GET /api/me", s.me},
		{"POST /api/me/kyc", s.submitKYC},

		{"GET /api/buyer/wallet", s.wallet},
		{"POST /api/buyer/recharge", s.createRecharge},
		{"GET /api/buyer/recharge/{id}", s.getRecharge},
		{"POST /api/buyer/recharge/{id}/cancel", s.cancelRecharge},
		{"POST /api/buyer/refunds", s.requestRefund},
		{"GET /api/buyer/refunds", s.listRefunds},
		{"POST /api/buyer/keys", s.createKey},
		{"GET /api/buyer/keys", s.listKeys},
		{"GET /api/buyer/keys/{id}", s.getKey},
		{"PUT /api/buyer/keys/{id}", s.updateKey},
		{"POST /api/buyer/keys/{id}/disable", s.setKeyEnabled(false)},
		{"POST /api/buyer/keys/{id}/enable", s.setKeyEnabled(true)},
		{"DELETE /api/buyer/keys/{id}", s.deleteKey},
		{"GET /api/buyer/usage", s.buyerUsage},
		{"GET /api/buyer/usage/{rid}", s.buyerRequestDetail},
		{"GET /api/buyer/bills", s.buyerBills},

		{"GET /api/seller/vendors", s.marketVendors},
		{"POST /api/seller/upstreams", s.createUpstream},
		{"GET /api/seller/upstreams", s.listUpstreams},
		{"GET /api/seller/upstreams/{id}", s.getUpstream},
		{"POST /api/seller/upstreams/{id}/oauth/start", s.startOAuth},
		{"POST /api/seller/upstreams/{id}/oauth/complete", s.completeOAuth},
		{"POST /api/seller/upstreams/{id}/coding-key", s.submitCodingKey},
		{"POST /api/seller/upstreams/{id}/retry-verification", s.retryVerification},
		{"POST /api/seller/upstreams/{id}/reauthorize", s.reauthorize},
		{"PUT /api/seller/upstreams/{id}/settings", s.upstreamSettings},
		{"DELETE /api/seller/upstreams/{id}", s.removeUpstream},
		{"POST /api/seller/upstreams/{id}/listing", s.createListing},
		{"GET /api/seller/upstreams/{id}/quote", s.currentQuote},
		{"GET /api/seller/upstreams/{id}/reports", s.sellerReports},
		{"GET /api/seller/upstreams/{id}/quality-trend", s.qualityTrend},
		{"POST /api/seller/upstreams/{id}/retest", s.manualRetest},
		{"GET /api/seller/reports/{id}", s.sellerReport},
		{"POST /api/seller/reports/{id}/appeal", s.appeal},
		{"GET /api/seller/listings", s.sellerListings},
		{"GET /api/seller/listings/{id}", s.sellerListing},
		{"POST /api/seller/listings/{id}/accept-quote", s.acceptQuote},
		{"POST /api/seller/listings/{id}/reject-quote", s.rejectQuote},
		{"PUT /api/seller/listings/{id}/price", s.changePrice},
		{"PUT /api/seller/listings/{id}/default-pool", s.setDefaultPool},
		{"POST /api/seller/listings/{id}/pause", s.pauseListing},
		{"POST /api/seller/listings/{id}/resume", s.resumeListing},
		{"POST /api/seller/listings/{id}/close", s.closeListing},
		{"GET /api/seller/earnings", s.earnings},
		{"GET /api/seller/earnings/lines", s.earningLines},
		{"POST /api/seller/withdrawals", s.requestWithdrawal},
		{"GET /api/seller/withdrawals", s.listWithdrawals},
	}
	admin := []route{
		{"GET /api/admin/settings", s.getSettings},
		{"PUT /api/admin/settings", s.updateSettings},
		{"GET /api/admin/vendors", s.adminVendors},
		{"PUT /api/admin/vendors/{platform}", s.updateVendor},
		{"GET /api/admin/proxies", s.listProxies},
		{"POST /api/admin/proxies", s.createProxy},
		{"POST /api/admin/proxies/{id}/sync", s.syncProxy},
		{"POST /api/admin/proxies/{id}/disable", s.setProxyDisabled(true)},
		{"POST /api/admin/proxies/{id}/enable", s.setProxyDisabled(false)},
		{"GET /api/admin/pools", s.listPools},
		{"POST /api/admin/pools", s.createPool},
		{"PUT /api/admin/pools/{id}", s.updatePool},
		{"POST /api/admin/pools/{id}/sync", s.syncPool},
		{"GET /api/admin/detection-policies", s.listPolicies},
		{"PUT /api/admin/detection-policies/{platform}", s.upsertPolicy},
		{"PUT /api/admin/upstreams/{id}/detection-override", s.setOverride},
		{"DELETE /api/admin/upstreams/{id}/detection-override", s.deleteOverride},
		{"POST /api/admin/upstreams/{id}/probe", s.adminProbe},
		{"GET /api/admin/probe-items", s.listItems},
		{"POST /api/admin/probe-items", s.createItem},
		{"PUT /api/admin/probe-items/{id}", s.updateItem},
		{"POST /api/admin/probe-items/{id}/retire", s.retireItem},
		{"GET /api/admin/reference-accounts", s.listReferences},
		{"POST /api/admin/reference-accounts", s.createReference},
		{"DELETE /api/admin/reference-accounts/{id}", s.deleteReference},
		{"GET /api/admin/quality/overview", s.qualityOverview},
		{"GET /api/admin/appeals", s.listAppeals},
		{"POST /api/admin/appeals/{id}/resolve", s.resolveAppeal},
		{"GET /api/admin/upstreams", s.adminUpstreams},
		{"POST /api/admin/upstreams/{id}/suspend", s.suspendUpstream(true)},
		{"POST /api/admin/upstreams/{id}/unsuspend", s.suspendUpstream(false)},
		{"DELETE /api/admin/upstreams/{id}", s.adminRemoveUpstream},
		{"GET /api/admin/kyc", s.pendingKYC},
		{"POST /api/admin/kyc/{id}/review", s.reviewKYC},
		{"POST /api/admin/users/{id}/status", s.setUserStatus},
		{"GET /api/admin/recharges", s.adminRecharges},
		{"POST /api/admin/recharges/{id}/confirm", s.confirmRecharge},
		{"GET /api/admin/refunds", s.adminRefunds},
		{"POST /api/admin/refunds/{id}/paid", s.refundPaid},
		{"GET /api/admin/withdrawals", s.adminWithdrawals},
		{"POST /api/admin/withdrawals/{id}/review", s.reviewWithdrawal},
		{"POST /api/admin/withdrawals/{id}/paid", s.withdrawalPaid},
		{"GET /api/admin/anomalies", s.listAnomalies},
		{"POST /api/admin/anomalies/{id}/resolve", s.resolveAnomaly},
		{"POST /api/admin/adjustments", s.adjust},
		{"GET /api/admin/risk-events", s.listRisk},
		{"POST /api/admin/risk-events/{id}/resolve", s.resolveRisk},
		{"GET /api/admin/ledger", s.platformLedger},
		{"GET /api/admin/sub2api/status", s.sub2apiStatus},
		{"GET /api/admin/audit-logs", s.auditLogs},
	}
	for _, r := range public {
		mux.Handle(r.pattern, r.h)
	}
	for _, r := range user {
		mux.Handle(r.pattern, httpx.Authenticate(s.Issuer, s.Accounts.CheckActive, r.h))
	}
	for _, r := range admin {
		mux.Handle(r.pattern, httpx.Authenticate(s.Issuer, s.Accounts.CheckActive, httpx.RequireAdmin(r.h)))
	}
	return httpx.Recover(httpx.RealIP(clientip.New(s.TrustedProxies), httpx.AccessLog(s.Log, mux)))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) error {
	if err := s.DB.Ping(r.Context()); err != nil {
		return httpx.Unavailable("DB_UNAVAILABLE", "database unavailable")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// audit 记录管理员操作，失败不影响主流程。
func (s *Server) audit(r *http.Request, action, target string, detail any) {
	u := httpx.MustUser(r)
	raw, _ := json.Marshal(detail)
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO audit_logs (actor_type, actor_id, action, target, detail) VALUES ('admin', $1, $2, $3, $4)`,
		u.UserID, action, target, raw)
}

func page(r *http.Request) (int, int) {
	return httpx.QueryInt(r, "limit", 50, 1, 500), httpx.QueryInt(r, "offset", 0, 0, 1_000_000)
}

func queryInt64(r *http.Request, name string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return v
}

func queryTime(r *http.Request, name string) time.Time {
	t, _ := time.Parse(time.RFC3339, r.URL.Query().Get(name))
	return t
}

func ok(w http.ResponseWriter, v any) error {
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

func created(w http.ResponseWriter, v any) error {
	httpx.WriteJSON(w, http.StatusCreated, v)
	return nil
}
