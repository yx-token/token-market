package routes

import (
	"errors"
	"io"
	"log"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/yx-token/token-market/internal/idlecat"
	"github.com/yx-token/token-market/internal/pkg/response"
	"github.com/yx-token/token-market/internal/server/middleware"
	"github.com/yx-token/token-market/market/enginehost"
)

var (
	idleCatOnce  sync.Once
	idleCatStore *idlecat.Store
)

func idleCat() *idlecat.Store {
	idleCatOnce.Do(func() {
		s, err := idlecat.OpenFromEnv()
		if err != nil {
			log.Printf("idlecat: native store unavailable: %v", err)
			return
		}
		idleCatStore = s
		log.Printf("idlecat: native MARKET_DATABASE_DSN connected")
	})
	return idleCatStore
}

func idleCatOK(c *gin.Context, fn func() (any, error)) {
	data, err := fn()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}

// registerIdleCatMarketRoutes 在 sub2api 管理面上挂闲猫增量路由。
// 路径与前端 resolvePath(/api/admin/...) 对齐：/api/v1/admin/market/<旧后缀>
func registerIdleCatMarketRoutes(admin *gin.RouterGroup) {
	if store := idleCat(); store != nil {
		registerNativeIdleCat(admin, store)
		return
	}
	if mountMarketUpstream(admin) {
		return
	}
	admin.GET("/market/status", idleCatMarketStatusStub)
}

func registerNativeIdleCat(admin *gin.RouterGroup, store *idlecat.Store) {
	type getter func(c *gin.Context) (any, error)
	bind := func(path string, fn getter) {
		admin.GET("/market/"+path, func(c *gin.Context) {
			idleCatOK(c, func() (any, error) { return fn(c) })
		})
	}
	bind("status", func(c *gin.Context) (any, error) { return store.Status(c.Request.Context()) })
	bind("vendors", func(c *gin.Context) (any, error) { return store.ListVendors(c.Request.Context()) })
	bind("supply/accounts", func(c *gin.Context) (any, error) { return store.ListSupplyAccounts(c.Request.Context()) })
	bind("supply/groups", func(c *gin.Context) (any, error) { return store.ListSupplyGroups(c.Request.Context()) })
	bind("pools", func(c *gin.Context) (any, error) { return store.ListPools(c.Request.Context()) })
	bind("proxies", func(c *gin.Context) (any, error) { return store.ListProxies(c.Request.Context()) })
	bind("upstreams", func(c *gin.Context) (any, error) { return store.ListUpstreams(c.Request.Context()) })
	bind("recharges", func(c *gin.Context) (any, error) { return store.ListRecharges(c.Request.Context()) })
	bind("refunds", func(c *gin.Context) (any, error) { return store.ListRefunds(c.Request.Context()) })
	bind("withdrawals", func(c *gin.Context) (any, error) { return store.ListWithdrawals(c.Request.Context()) })
	bind("appeals", func(c *gin.Context) (any, error) { return store.ListAppeals(c.Request.Context()) })
	// 以下路径与 frontend/src/market-admin 一致；已去掉 policies/providers/categories/quality/payments/hot-searches 别名。
	bind("detection-policies", func(c *gin.Context) (any, error) { return store.ListPolicies(c.Request.Context()) })
	bind("detection-providers", func(c *gin.Context) (any, error) { return store.ListProviders(c.Request.Context()) })
	bind("probe-items", func(c *gin.Context) (any, error) { return store.ListProbeItems(c.Request.Context()) })
	bind("probe-runs", func(c *gin.Context) (any, error) { return store.ListProbeRuns(c.Request.Context()) })
	bind("market-categories", func(c *gin.Context) (any, error) { return store.ListCategories(c.Request.Context()) })
	bind("quality/overview", func(c *gin.Context) (any, error) { return store.QualityOverview(c.Request.Context()) })
	// payments/*、settings、market-discovery 读/写均走 relay（market 已扁平 Defaults）。
	bind("identities", func(c *gin.Context) (any, error) { return store.ListIdentities(c.Request.Context()) })

	// PUT vendors/:platform、PUT pools/:id、PUT detection-policies/:platform 走 relay；见 registerIdleCatRelay。
	admin.POST("/market/proxies/:id/disable", func(c *gin.Context) { idleCatProxy(c, store, true) })
	admin.POST("/market/proxies/:id/enable", func(c *gin.Context) { idleCatProxy(c, store, false) })
	admin.POST("/market/probe-items/:id/retire", func(c *gin.Context) {
		id, ok := idleCatID(c)
		if !ok {
			return
		}
		if err := store.RetireProbeItem(c.Request.Context(), id); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, gin.H{"id": id, "status": "retired"})
	})
	admin.POST("/market/identities/ensure", func(c *gin.Context) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			response.BadRequest(c, "invalid json")
			return
		}
		id, err := store.EnsureEngineUser(c.Request.Context(), in.Email, in.Password, in.Role)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, gin.H{"email": in.Email, "engine_user_id": id})
	})

	admin.POST("/market/recharges/:id/confirm", idleCatConfirmRecharge)
	admin.POST("/market/recharges/:id/refund", idleCatRefundRecharge)
	admin.POST("/market/refunds/:id/paid", idleCatRefundPaid)
	admin.POST("/market/withdrawals/:id/review", idleCatReviewWithdrawal)
	admin.POST("/market/withdrawals/:id/paid", idleCatWithdrawalPaid)
	admin.POST("/market/appeals/:id/resolve", idleCatResolveAppeal)
	registerIdleCatRelay(admin)
}

// registerIdleCatRelay 把尚未单独实现的管理接口交给 market 原处理函数。
// 系统升级不在此列，仍由 market-api 本进程处理。
func registerIdleCatRelay(admin *gin.RouterGroup) {
	paths := []string{
		"POST identities/bind",
		"GET inspection/settings",
		"PUT inspection/settings",
		"GET inspection/checks",
		"PUT inspection/checks/:key",
		"POST market-categories",
		"PUT market-categories/:id",
		"DELETE market-categories/:id",
		"PUT vendors/:platform",
		"POST proxies",
		"POST proxies/:id/sync",
		"POST proxies/:id/test",
		"POST proxies/:id/quality-check",
		"POST pools",
		"PUT pools/:id",
		"POST pools/:id/sync",
		"PUT detection-providers/:name",
		"PUT detection-policies/:platform",
		"GET third-party-checks",
		"PUT upstreams/:id/detection-override",
		"DELETE upstreams/:id/detection-override",
		"POST upstreams/:id/probe",
		"GET upstreams/:id/probe-runs",
		"GET probe-runs/:id",
		"GET probe-item-stats",
		"GET probe-items/:id/results",
		"GET probe-generator-templates",
		"POST probe-items",
		"PUT probe-items/:id",
		"GET reference-accounts",
		"POST reference-accounts",
		"DELETE reference-accounts/:id",
		"POST upstreams/:id/suspend",
		"POST upstreams/:id/unsuspend",
		"DELETE upstreams/:id",
		"GET kyc",
		"POST kyc/:id/review",
		"POST users/:id/status",
		"POST recharges/:id/sync",
		"GET payments/status",
		"GET payments/merchants",
		"PUT payments/merchants/:provider",
		"POST payments/merchants/:provider/test",
		"GET settings",
		"PUT settings",
		"GET market-discovery",
		"PUT market-discovery",
		"POST refund-payouts/:id/retry",
		"POST refund-payouts/:id/manual",
		"GET anomalies",
		"POST anomalies/:id/resolve",
		"POST adjustments",
		"GET risk-events",
		"POST risk-events/:id/resolve",
		"GET ledger",
		"GET audit-logs",
		"GET org/units",
		"POST org/units",
		"PUT org/units/:id",
		"DELETE org/units/:id",
		"GET org/resources",
		"PUT org/resources/:id",
		"GET org/roles",
		"POST org/roles",
		"PUT org/roles/:id",
		"DELETE org/roles/:id",
		"GET org/roles/:id/resources",
		"PUT org/roles/:id/resources",
		"GET org/roles/:id/members",
		"PUT org/roles/:id/members",
		"GET org/staff",
		"POST org/staff",
		"GET org/users",
		"PUT org/staff/:id",
	}
	for _, spec := range paths {
		method, path, _ := strings.Cut(spec, " ")
		admin.Handle(method, "/market/"+path, idleCatRelay)
	}
}

func idleCatRelay(c *gin.Context) {
	full := c.Request.URL.Path
	i := strings.Index(full, "/market/")
	if i < 0 {
		response.NotFound(c, "not found")
		return
	}
	rest := full[i+len("/market/"):]
	body, _ := io.ReadAll(c.Request.Body)
	status, payload, err := enginehost.ServeAdmin(c.Request.Context(), c.Request.Method, rest, c.Request.URL.RawQuery, body, idleCatAdminID(c))
	if err != nil {
		response.Error(c, 503, "market ledger unavailable")
		return
	}
	c.Data(status, "application/json; charset=utf-8", payload)
}

func idleCatAdminID(c *gin.Context) int64 {
	if s, ok := middleware.GetAuthSubjectFromContext(c); ok {
		return s.UserID
	}
	return 0
}

func idleCatWriteErr(c *gin.Context, err error) {
	var he *enginehost.APIError
	if errors.As(err, &he) {
		response.ErrorWithDetails(c, he.Status, he.Message, he.Code, nil)
		return
	}
	response.ErrorFrom(c, err)
}

func idleCatConfirmRecharge(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		ProviderRef string `json:"provider_ref"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	var by *int64
	if admin := idleCatAdminID(c); admin > 0 {
		by = &admin
	}
	order, err := enginehost.ConfirmRecharge(c.Request.Context(), id, in.ProviderRef, by)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Success(c, order)
}

func idleCatRefundRecharge(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	ref, err := enginehost.RefundRecharge(c.Request.Context(), id, in.Amount, in.Note)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Created(c, ref)
}

func idleCatRefundPaid(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		PayoutRef string `json:"payout_ref"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	ref, err := enginehost.MarkRefundPaid(c.Request.Context(), id, in.PayoutRef)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Success(c, ref)
}

func idleCatReviewWithdrawal(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	wd, err := enginehost.ReviewWithdrawal(c.Request.Context(), idleCatAdminID(c), id, in.Approve, in.Reason)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Success(c, wd)
}

func idleCatWithdrawalPaid(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		PayoutRef string `json:"payout_ref"`
		Failed    bool   `json:"failed"`
		Reason    string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	wd, err := enginehost.MarkWithdrawalPaid(c.Request.Context(), idleCatAdminID(c), id, in.PayoutRef, in.Failed, in.Reason)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Success(c, wd)
}

func idleCatResolveAppeal(c *gin.Context) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid json")
		return
	}
	appeal, err := enginehost.ResolveAppeal(c.Request.Context(), id, in.Action, in.Note)
	if err != nil {
		idleCatWriteErr(c, err)
		return
	}
	response.Success(c, appeal)
}

func idleCatID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid id")
		return 0, false
	}
	return id, true
}

func idleCatProxy(c *gin.Context, store *idlecat.Store, disabled bool) {
	id, ok := idleCatID(c)
	if !ok {
		return
	}
	if err := store.SetProxyDisabled(c.Request.Context(), id, disabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	status := "available"
	if disabled {
		status = "disabled"
	}
	response.Success(c, gin.H{"id": id, "status": status})
}

func mountMarketUpstream(admin *gin.RouterGroup) bool {
	upstream := strings.TrimSpace(os.Getenv("MARKET_UPSTREAM_URL"))
	if upstream == "" {
		return false
	}
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return false
	}
	// MARKET_UPSTREAM_URL is operator-controlled env, not request input.
	proxy := httputil.NewSingleHostReverseProxy(target) //nolint:gosec // G704: trusted deploy env URL
	admin.Any("/market/*path", func(c *gin.Context) {
		rest := strings.TrimPrefix(c.Param("path"), "/")
		if rest == "" || rest == "status" {
			rest = "sub2api/status"
		}
		req := c.Request.Clone(c.Request.Context())
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/api/admin/" + rest
		req.URL.RawPath = ""
		req.Host = target.Host
		proxy.ServeHTTP(c.Writer, req)
	})
	return true
}

func idleCatMarketStatusStub(c *gin.Context) {
	response.Success(c, gin.H{
		"product": "idle-cat",
		"host":    "sub2api",
		"mode":    "native-stub",
	})
}
