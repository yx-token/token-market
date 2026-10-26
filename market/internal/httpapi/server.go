// Package httpapi 是控制面 HTTP 接口：账号、市场、买家、卖家、管理后台。
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/account"
	"github.com/yx-token/token-market/market/internal/billing"
	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/iam"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/market"
	"github.com/yx-token/token-market/market/internal/payment/merchants"
	"github.com/yx-token/token-market/market/internal/platform/auth"
	"github.com/yx-token/token-market/market/internal/platform/buildinfo"
	"github.com/yx-token/token-market/market/internal/platform/clientip"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/platform/ratelimit"
	"github.com/yx-token/token-market/market/internal/quality"
	"github.com/yx-token/token-market/market/internal/risk"
	"github.com/yx-token/token-market/market/internal/scheduling"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/update"
)

type Server struct {
	DB       *pgxpool.Pool
	Issuer   *auth.Issuer
	Accounts *account.Service
	IAM      *iam.Service
	Listings *listing.Service
	Proxies  *listing.ProxyService
	Pools    *scheduling.PoolService
	Keys     *buyer.KeyService
	Funds    *billing.Funds
	Payments *merchants.Store
	Market   *market.Service
	Quality  *quality.Engine
	Replica  *replica.Reader
	Risk     *risk.Service
	Kicker   listing.Kicker
	Log      *slog.Logger
	// TrustedProxies 是前置负载均衡的地址，只有来自它们的请求才采信 X-Forwarded-For
	TrustedProxies []string
	// WebDir 是 C 端 + 闲猫管理端（/admin）构建产物目录
	WebDir string
	// AdminWebDir 是 sub2api 管理端 SPA（挂在 /admin）；可选
	AdminWebDir string
	// EngineProxyURL 是 sub2api（或替身）根地址，用于反代 /api/v1
	EngineProxyURL string
	// EngineAdminKey 调用替身管理员接口的 x-api-key
	EngineAdminKey string
	// EngineLoginPassword 非空时，用同一邮箱向真实 sub2api /api/v1/auth/login 换会话
	EngineLoginPassword string
	// EdgeURL 是买家调用模型的地址，在前端展示
	EdgeURL string
	Updater update.Controller

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

func (s *Server) adminRoutes() []route {
	return []route{
		{"POST /api/admin/engine/session", s.engineSession},
		{"POST /api/admin/identities/bind", s.bindIdentityShadow},
		{"GET /api/admin/system/version", s.systemVersion},
		{"GET /api/admin/system/check-update", s.checkSystemUpdate},
		{"POST /api/admin/system/update", s.startSystemUpdate},
		{"GET /api/admin/settings", s.getSettings},
		{"PUT /api/admin/settings", s.updateSettings},
		{"GET /api/admin/inspection/settings", s.inspectionSettings},
		{"PUT /api/admin/inspection/settings", s.updateInspectionSettings},
		{"GET /api/admin/inspection/checks", s.inspectionChecks},
		{"PUT /api/admin/inspection/checks/{key}", s.updateInspectionCheck},
		{"GET /api/admin/market-discovery", s.getMarketDiscovery},
		{"PUT /api/admin/market-discovery", s.updateMarketDiscovery},
		{"GET /api/admin/market-categories", s.adminMarketCategories},
		{"POST /api/admin/market-categories", s.createMarketCategory},
		{"PUT /api/admin/market-categories/{id}", s.updateMarketCategory},
		{"DELETE /api/admin/market-categories/{id}", s.deleteMarketCategory},
		{"GET /api/admin/vendors", s.adminVendors},
		{"PUT /api/admin/vendors/{platform}", s.updateVendor},
		{"GET /api/admin/proxies", s.listProxies},
		{"POST /api/admin/proxies", s.createProxy},
		{"POST /api/admin/proxies/{id}/sync", s.syncProxy},
		{"POST /api/admin/proxies/{id}/test", s.checkProxy(false, false)},
		{"POST /api/admin/proxies/{id}/quality-check", s.checkProxy(false, true)},
		{"POST /api/admin/proxies/{id}/disable", s.setProxyDisabled(true)},
		{"POST /api/admin/proxies/{id}/enable", s.setProxyDisabled(false)},
		{"GET /api/admin/pools", s.listPools},
		{"POST /api/admin/pools", s.createPool},
		{"PUT /api/admin/pools/{id}", s.updatePool},
		{"POST /api/admin/pools/{id}/sync", s.syncPool},
		{"GET /api/admin/detection-policies", s.listPolicies},
		{"PUT /api/admin/detection-policies/{platform}", s.upsertPolicy},
		{"GET /api/admin/detection-providers", s.listDetectionProviders},
		{"PUT /api/admin/detection-providers/{name}", s.updateDetectionProvider},
		{"GET /api/admin/third-party-checks", s.listThirdPartyChecks},
		{"PUT /api/admin/upstreams/{id}/detection-override", s.setOverride},
		{"DELETE /api/admin/upstreams/{id}/detection-override", s.deleteOverride},
		{"POST /api/admin/upstreams/{id}/probe", s.adminProbe},
		{"GET /api/admin/upstreams/{id}/probe-runs", s.adminProbeRuns},
		{"GET /api/admin/probe-runs", s.adminProbeRecords},
		{"GET /api/admin/probe-runs/{id}", s.adminProbeRun},
		{"GET /api/admin/probe-item-stats", s.adminProbeItemStats},
		{"GET /api/admin/probe-items/{id}/results", s.adminProbeItemResults},
		{"GET /api/admin/probe-items", s.listItems},
		{"GET /api/admin/probe-generator-templates", s.probeGeneratorTemplates},
		{"POST /api/admin/probe-items", s.createItem},
		{"PUT /api/admin/probe-items/{id}", s.updateItem},
		{"POST /api/admin/probe-items/{id}/retire", s.retireItem},
		{"GET /api/admin/reference-accounts", s.listReferences},
		{"POST /api/admin/reference-accounts", s.createReference},
		{"DELETE /api/admin/reference-accounts/{id}", s.deleteReference},
		{"GET /api/admin/quality/overview", s.qualityOverview},
		{"GET /api/admin/appeals", s.listAppeals},
		{"POST /api/admin/appeals/{id}/resolve", s.resolveAppeal},
		{"GET /api/admin/supply/accounts", s.adminSupplyAccounts},
		{"GET /api/admin/supply/groups", s.adminSupplyGroups},
		{"GET /api/admin/upstreams", s.adminUpstreams},
		{"POST /api/admin/upstreams", s.adminCreateUpstream},
		{"GET /api/admin/upstreams/{id}", s.adminGetUpstream},
		{"GET /api/admin/upstreams/{id}/models", s.adminUpstreamModels},
		{"POST /api/admin/upstreams/{id}/models/refresh", s.adminRefreshUpstreamModels},
		{"PUT /api/admin/upstreams/{id}/models", s.adminSetUpstreamModels},
		{"POST /api/admin/upstreams/{id}/oauth/start", s.adminStartUpstreamOAuth},
		{"POST /api/admin/upstreams/{id}/oauth/complete", s.adminCompleteUpstreamOAuth},
		{"POST /api/admin/upstreams/{id}/coding-key", s.adminSubmitUpstreamCodingKey},
		{"POST /api/admin/upstreams/{id}/clear-dead-account", s.adminClearDeadEngineAccount},
		{"POST /api/admin/upstreams/{id}/suspend", s.suspendUpstream(true)},
		{"POST /api/admin/upstreams/{id}/unsuspend", s.suspendUpstream(false)},
		{"DELETE /api/admin/upstreams/{id}", s.adminRemoveUpstream},
		{"GET /api/admin/kyc", s.pendingKYC},
		{"POST /api/admin/kyc/{id}/review", s.reviewKYC},
		{"GET /api/admin/users", s.adminListUsers},
		{"POST /api/admin/users", s.adminCreateUser},
		{"GET /api/admin/users/{id}", s.adminGetUser},
		{"PUT /api/admin/users/{id}", s.adminUpdateUser},
		{"POST /api/admin/users/{id}/status", s.setUserStatus},
		{"GET /api/admin/listings", s.adminListListings},
		{"POST /api/admin/listings", s.adminCreateListing},
		{"GET /api/admin/listings/{id}", s.adminGetListing},
		{"PUT /api/admin/listings/{id}", s.adminUpdateListing},
		{"POST /api/admin/listings/{id}/pause", s.adminPauseListing},
		{"POST /api/admin/listings/{id}/resume", s.adminResumeListing},
		{"POST /api/admin/listings/{id}/close", s.adminCloseListing},
		{"GET /api/admin/recharges", s.adminRecharges},
		{"POST /api/admin/recharges/{id}/confirm", s.confirmRecharge},
		{"POST /api/admin/recharges/{id}/sync", s.syncRecharge},
		{"POST /api/admin/recharges/{id}/refund", s.adminRefundRecharge},
		{"GET /api/admin/payments/status", s.paymentStatus},
		{"GET /api/admin/payments/merchants", s.listMerchants},
		{"PUT /api/admin/payments/merchants/{provider}", s.updateMerchant},
		{"POST /api/admin/payments/merchants/{provider}/test", s.testMerchant},
		{"POST /api/admin/refund-payouts/{id}/retry", s.retryPayout},
		{"POST /api/admin/refund-payouts/{id}/manual", s.payoutToManual},
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
		{"GET /api/admin/org/units", s.listOrgUnits},
		{"POST /api/admin/org/units", s.createOrgUnit},
		{"PUT /api/admin/org/units/{id}", s.updateOrgUnit},
		{"DELETE /api/admin/org/units/{id}", s.deleteOrgUnit},
		{"GET /api/admin/org/resources", s.listIAMResources},
		{"PUT /api/admin/org/resources/{id}", s.updateIAMResource},
		{"GET /api/admin/org/roles", s.listIAMRoles},
		{"POST /api/admin/org/roles", s.createIAMRole},
		{"PUT /api/admin/org/roles/{id}", s.updateIAMRole},
		{"DELETE /api/admin/org/roles/{id}", s.deleteIAMRole},
		{"GET /api/admin/org/roles/{id}/resources", s.getIAMRoleResources},
		{"PUT /api/admin/org/roles/{id}/resources", s.setIAMRoleResources},
		{"GET /api/admin/org/roles/{id}/members", s.getIAMRoleMembers},
		{"PUT /api/admin/org/roles/{id}/members", s.setIAMRoleMembers},
		{"GET /api/admin/org/staff", s.listIAMStaff},
		{"POST /api/admin/org/staff", s.createIAMStaff},
		{"GET /api/admin/org/users", s.searchIAMUsers},
		{"PUT /api/admin/org/staff/{id}", s.assignIAMStaff},
	}
}

// Handler 组装全部路由。
// Handler 独立 market-api 进程：C 端 + 市场管理 API + 反代宿主 /api/v1 + 双前端。
func (s *Server) Handler() http.Handler {
	return s.buildHandler(handlerOpts{})
}

// HandlerEmbed 挂进闲猫宿主进程：只挂 C 端/买卖 API 与 C 端静态；管理端与 /api/v1 由宿主自己提供。
func (s *Server) HandlerEmbed() http.Handler {
	return s.buildHandler(handlerOpts{EmbedInHost: true})
}

type handlerOpts struct {
	EmbedInHost bool
}

func (s *Server) buildHandler(opts handlerOpts) http.Handler {
	s.loginFailsByIP = ratelimit.NewFailures(30, 15*time.Minute)
	s.loginFailsByEmail = ratelimit.NewFailures(10, 15*time.Minute)
	s.registersByIP = ratelimit.NewFailures(20, time.Hour)
	mux := http.NewServeMux()
	public := []route{
		{"GET /healthz", s.healthz},
		{"POST /api/auth/register", s.register},
		{"POST /api/auth/login", s.login},
		{"POST /api/auth/from-engine", s.fromEngine},
		{"GET /api/market/prices", s.marketPrices},
		{"GET /api/market/config", s.marketConfig},
		{"GET /api/market/discovery", s.marketDiscovery},
		{"GET /api/market/categories", s.marketCategories},
		{"GET /api/market/listings", s.marketListings},
		{"GET /api/market/listings/{id}", s.marketListing},
		{"GET /api/market/pools", s.marketPools},
		{"GET /api/market/credit-offers", s.marketCreditOffers},
		{"GET /api/market/vendors", s.marketVendors},
	}
	user := []route{
		{"POST /api/auth/refresh", s.refreshToken},
		{"GET /api/auth/me", s.me}, // 社区 frontend 兼容
		{"GET /api/me", s.me},
		{"GET /api/me/console-access", s.myConsoleAccess},
		{"POST /api/me/kyc", s.submitKYC},

		{"GET /api/buyer/wallet", s.wallet},
		{"POST /api/buyer/recharge", s.createRecharge},
		{"GET /api/buyer/recharge/{id}", s.getRecharge},
		{"POST /api/buyer/recharge/{id}/cancel", s.cancelRecharge},
		{"POST /api/buyer/recharge/{id}/refund", s.refundRecharge},
		{"POST /api/buyer/refunds", s.requestRefund},
		{"GET /api/buyer/refunds", s.listRefunds},
		{"POST /api/buyer/packs", s.purchasePack},
		{"POST /api/buyer/credit-packs", s.purchaseCreditPack},
		{"GET /api/buyer/credit-packs", s.buyerCreditPacks},
		{"POST /api/buyer/credit-packs/{id}/refund", s.refundCreditPack},
		{"GET /api/buyer/packs", s.listPacks},
		{"POST /api/buyer/packs/{id}/refund", s.refundPack},
		{"GET /api/buyer/merchants", s.buyerMerchants},
		{"GET /api/buyer/merchants/listings", s.buyerMerchantListings},
		{"PUT /api/buyer/merchants/{id}", s.addBuyerMerchant},
		{"DELETE /api/buyer/merchants/{id}", s.removeBuyerMerchant},
		{"POST /api/buyer/keys", s.createKey},
		{"GET /api/buyer/keys", s.listKeys},
		{"GET /api/buyer/keys/{id}", s.getKey},
		{"GET /api/buyer/keys/{id}/secret", s.keySecret},
		{"PUT /api/buyer/keys/{id}", s.updateKey},
		{"POST /api/buyer/keys/{id}/disable", s.setKeyEnabled(false)},
		{"POST /api/buyer/keys/{id}/enable", s.setKeyEnabled(true)},
		{"DELETE /api/buyer/keys/{id}", s.deleteKey},
		{"GET /api/buyer/usage", s.buyerUsage},
		{"GET /api/buyer/usage/{rid}", s.buyerRequestDetail},
		{"GET /api/buyer/bills", s.buyerBills},

		{"GET /api/seller/vendors", s.marketVendors},
		{"GET /api/seller/credit-offers", s.sellerCreditOffers},
		{"POST /api/seller/credit-offers", s.createCreditOffer},
		{"PUT /api/seller/credit-offers/{id}", s.updateCreditOffer},
		{"GET /api/seller/listing-categories", s.listingCategories},
		{"GET /api/seller/proxies", s.listSellerProxies},
		{"POST /api/seller/proxies", s.createSellerProxy},
		{"PUT /api/seller/proxies/{id}", s.updateSellerProxy},
		{"DELETE /api/seller/proxies/{id}", s.deleteSellerProxy},
		{"POST /api/seller/proxies/{id}/sync", s.syncSellerProxy},
		{"POST /api/seller/proxies/{id}/test", s.checkProxy(true, false)},
		{"POST /api/seller/proxies/{id}/quality-check", s.checkProxy(true, true)},
		{"POST /api/seller/proxies/{id}/disable", s.setSellerProxyDisabled(true)},
		{"POST /api/seller/proxies/{id}/enable", s.setSellerProxyDisabled(false)},
		{"POST /api/seller/upstreams", s.createUpstream},
		{"GET /api/seller/upstreams", s.listUpstreams},
		{"GET /api/seller/upstreams/{id}", s.getUpstream},
		{"GET /api/seller/upstreams/{id}/models", s.upstreamModels},
		{"POST /api/seller/upstreams/{id}/models/refresh", s.refreshUpstreamModels},
		{"PUT /api/seller/upstreams/{id}/models", s.setUpstreamModels},
		{"POST /api/seller/upstreams/{id}/oauth/start", s.startOAuth},
		{"POST /api/seller/upstreams/{id}/oauth/complete", s.completeOAuth},
		{"POST /api/seller/upstreams/{id}/coding-key", s.submitCodingKey},
		{"POST /api/seller/upstreams/{id}/import", s.importUpstreamAccount},
		{"POST /api/seller/upstreams/{id}/retry-verification", s.retryVerification},
		{"POST /api/seller/upstreams/{id}/reauthorize", s.reauthorize},
		{"PUT /api/seller/upstreams/{id}/settings", s.upstreamSettings},
		{"DELETE /api/seller/upstreams/{id}", s.removeUpstream},
		{"POST /api/seller/upstreams/{id}/listing", s.createListing},
		{"GET /api/seller/upstreams/{id}/quote", s.currentQuote},
		{"GET /api/seller/upstreams/{id}/reports", s.sellerReports},
		{"GET /api/seller/upstreams/{id}/probe-runs", s.sellerProbeRuns},
		{"GET /api/seller/upstreams/{id}/quota", s.upstreamQuota},
		{"GET /api/seller/upstreams/{id}/reset-credits", s.resetCredits},
		{"POST /api/seller/upstreams/{id}/reset-credits/redeem", s.redeemResetCredit},
		{"PUT /api/seller/upstreams/{id}/auto-reset", s.setAutoReset},
		{"GET /api/seller/usage", s.sellerUsage},
		{"GET /api/seller/usage/records", s.sellerUsageRecords},
		{"POST /api/seller/upstreams/{id}/retest", s.manualRetest},
		{"GET /api/seller/reports/{id}", s.sellerReport},
		{"POST /api/seller/reports/{id}/appeal", s.appeal},
		{"GET /api/seller/listings", s.sellerListings},
		{"GET /api/seller/listings/{id}", s.sellerListing},
		{"POST /api/seller/listings/{id}/accept-quote", s.acceptQuote},
		{"POST /api/seller/listings/{id}/reject-quote", s.rejectQuote},
		{"PUT /api/seller/listings/{id}/price", s.changePrice},
		{"PUT /api/seller/listings/{id}/meta", s.updateListingMeta},
		{"PUT /api/seller/listings/{id}/default-pool", s.setDefaultPool},
		{"POST /api/seller/listings/{id}/pause", s.pauseListing},
		{"POST /api/seller/listings/{id}/resume", s.resumeListing},
		{"POST /api/seller/listings/{id}/close", s.closeListing},
		{"GET /api/seller/earnings", s.earnings},
		{"GET /api/seller/earnings/lines", s.earningLines},
		{"POST /api/seller/withdrawals", s.requestWithdrawal},
		{"GET /api/seller/withdrawals", s.listWithdrawals},
	}
	for _, r := range public {
		mux.Handle(r.pattern, r.h)
	}
	// 支付渠道异步通知：不鉴权，按渠道协议验签与应答
	mux.HandleFunc("POST /api/payments/{provider}/notify", s.paymentNotify)
	for _, r := range user {
		mux.Handle(r.pattern, httpx.Authenticate(s.Issuer, s.Accounts.CheckActive, r.h))
	}
	if opts.EmbedInHost {
		// 单进程：C 端「控制台」经 /open-admin 换宿主 JWT；其余管理 API 走宿主 /api/v1/admin/market/*。
		h := http.Handler(httpx.Handler(s.engineSession))
		h = httpx.RequireConsoleStaff(s.consoleStaffOK, h)
		mux.Handle("POST /api/admin/engine/session", s.authenticateAdmin(h))
	} else {
		for _, r := range s.adminRoutes() {
			h := http.Handler(r.h)
			// 组织写操作仅超管；其余按菜单资源码细粒度鉴权，未映射则退回「有控制台权限即可」
			if isOrgMutating(r.pattern) {
				h = httpx.RequireAdmin(h)
			} else if ids := adminRouteResources(r.pattern); len(ids) > 0 {
				h = httpx.RequireAnyResource(s.resourceOK, ids, h)
			} else {
				h = httpx.RequireConsoleStaff(s.consoleStaffOK, h)
			}
			mux.Handle(r.pattern, s.authenticateAdmin(h))
			// 二开命名空间：同一套闲猫管理接口挂到 /api/v1/admin/market/*
			if v1 := marketV1Pattern(r.pattern); v1 != "" {
				mux.Handle(v1, s.authenticateAdmin(h))
			}
		}
		// 闲猫增量路由必须在 /api/v1/ 反代之前注册（更具体的 pattern 优先生效）
		if s.EngineProxyURL != "" {
			if target, err := url.Parse(s.EngineProxyURL); err == nil {
				proxy := httputil.NewSingleHostReverseProxy(target)
				// 宿主的在线升级 / 回滚会用上游社区版本替换二进制；宿主只能随发布镜像升级（同 sub2api-admin nginx）。
				engineUpdateDisabled := httpx.Handler(func(http.ResponseWriter, *http.Request) error {
					return httpx.Forbidden("ENGINE_UPDATE_DISABLED", "online update is disabled; upgrade the engine image")
				})
				for _, p := range []string{"/api/v1/admin/system/update", "/api/v1/admin/system/rollback"} {
					mux.Handle(p, engineUpdateDisabled)
					mux.Handle(p+"/", engineUpdateDisabled)
				}
				mux.Handle("/api/v1/", proxy)
			}
		}
	}
	mux.Handle("/api/", httpx.Handler(func(http.ResponseWriter, *http.Request) error {
		return httpx.NotFound("NOT_FOUND", "endpoint not found")
	}))
	adminHome := "/admin/dashboard"
	if !opts.EmbedInHost && s.AdminWebDir != "" {
		mux.Handle("/admin-assets/", adminAssetHandler(s.AdminWebDir))
		adminIndex := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(s.AdminWebDir, "index.html"))
		})
		mux.Handle("/admin", adminIndex)
		mux.Handle("/admin/", adminIndex)
		// C 端第一页在宿主 frontend；/ 仍是 market/web。
		mux.Handle("/shop", adminIndex)
		mux.Handle("/shop/", adminIndex)
	}
	if !opts.EmbedInHost {
		// 旧 /engine 书签、会话桥（闲猫 SPA，不占用 /admin）
		mux.Handle("/engine", http.RedirectHandler(adminHome, http.StatusFound))
		mux.Handle("/engine/", http.RedirectHandler(adminHome, http.StatusFound))
		mux.Handle("/admin/open-engine", http.RedirectHandler("/open-admin", http.StatusFound))
		if s.WebDir != "" {
			mux.HandleFunc("GET /open-admin", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, filepath.Join(s.WebDir, "index.html"))
			})
		}
		mux.Handle("/console", http.RedirectHandler(adminHome, http.StatusFound))
		mux.Handle("/console/", http.RedirectHandler(adminHome, http.StatusFound))
	}
	if s.WebDir != "" {
		if opts.EmbedInHost {
			// 宿主已有 /admin SPA；此处只挂 C 端常见前缀，避免吞掉管理端。
			for _, p := range []string{"/buyer", "/seller", "/login", "/register", "/shop", "/open-admin"} {
				mux.Handle(p, spaHandler(s.WebDir))
				mux.Handle(p+"/", spaHandler(s.WebDir))
			}
			mux.Handle("/assets/", spaHandler(s.WebDir))
			mux.Handle("/", spaHandler(s.WebDir))
		} else {
			mux.Handle("/", spaHandler(s.WebDir))
		}
	}
	return httpx.Recover(httpx.RealIP(clientip.New(s.TrustedProxies), httpx.AccessLog(s.Log, mux)))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) error {
	if err := s.DB.Ping(r.Context()); err != nil {
		return httpx.Unavailable("DB_UNAVAILABLE", "database unavailable")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildinfo.Version})
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

func (s *Server) consoleStaffOK(ctx context.Context, userID int64, isAdmin bool) (bool, error) {
	if s.IAM == nil {
		return isAdmin, nil
	}
	return s.IAM.HasConsoleAccess(ctx, userID, isAdmin)
}

func (s *Server) resourceOK(ctx context.Context, userID int64, isAdmin bool, resourceIDs ...string) (bool, error) {
	if s.IAM == nil {
		return isAdmin, nil
	}
	return s.IAM.HasAnyResource(ctx, userID, isAdmin, resourceIDs...)
}

// isOrgMutating 组织权限写操作仅超管可做（读仍走员工闸）。
func isOrgMutating(pattern string) bool {
	switch {
	case strings.HasPrefix(pattern, "POST /api/admin/org/"),
		strings.HasPrefix(pattern, "PUT /api/admin/org/"),
		strings.HasPrefix(pattern, "DELETE /api/admin/org/"):
		return true
	default:
		return false
	}
}

// spaHandler 提供前端静态文件；找不到的路径返回 index.html，由前端路由处理。
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil && !info.IsDir() {
			if strings.HasPrefix(p, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // 文件名带内容哈希
			}
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// adminAssetHandler 提供管理端带哈希的静态资源（与 C 端 /assets 分离，避免同端口冲突）。
func adminAssetHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

// adminSPAHandler 提供管理端 SPA；/admin/* 一律回退到 index.html。
func adminSPAHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
