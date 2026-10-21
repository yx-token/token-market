//go:build integration

package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

type modelWhitelistUpstream struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	ModelWhitelist []string   `json:"model_whitelist"`
	ModelCatalog   []string   `json:"model_catalog"`
	ModelCatalogAt *time.Time `json:"model_catalog_at"`
	PrivacyOK      *bool      `json:"privacy_ok"`
}

type modelWhitelistCatalog struct {
	Catalog   []string   `json:"catalog"`
	Whitelist []string   `json:"whitelist"`
	FetchedAt *time.Time `json:"fetched_at"`
}

const whitelistSonnet = "claude-sonnet-4-5"
const whitelistOpus = "claude-opus-4-5"

func prepareModelWhitelistSeller(e *env, name string, proxies int) *client {
	e.t.Helper()
	admin := e.login("admin@market.test", "admin-password-1")
	for _, platform := range []string{"anthropic", "kimi"} {
		admin.must(http.MethodPut, "/api/admin/vendors/"+platform, map[string]bool{"enabled": true}, nil)
	}
	for n := 0; n < proxies; n++ {
		admin.must(http.MethodPost, "/api/admin/proxies", map[string]any{
			"name": fmt.Sprintf("模型白名单代理%d", n), "protocol": "http", "host": "10.10.8.1", "port": 8100 + n,
		}, nil)
	}
	return e.newUser(name + "@market.test")
}

func modelWhitelistAccountID(e *env, upstreamID int64) int64 {
	e.t.Helper()
	var accountID int64
	mustScan(e.t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", upstreamID), &accountID)
	return accountID
}

func assertModelWhitelistMapping(e *env, accountID int64, models []string) {
	e.t.Helper()
	got, err := e.fake.AccountModelMapping(accountID)
	if err != nil {
		e.t.Fatal(err)
	}
	want := make(map[string]string, len(models))
	for _, model := range models {
		want[model] = model
	}
	if !reflect.DeepEqual(got, want) {
		e.t.Fatalf("挂售白名单应写入恒等映射：got=%v want=%v", got, want)
	}
}

// 三种卖家接入方式先缓存完整目录；选模型后可直接挂售，检测由卖家主动发起。
func TestModelWhitelistOnboardingPaths(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-paths", 3)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	paths := []struct {
		name     string
		platform string
		authType string
		suffix   string
		body     any
	}{
		{"官方授权", "anthropic", "oauth", "/oauth/complete", map[string]string{"code": "whitelist-oauth"}},
		{"Coding Key", "kimi", "coding", "/coding-key", map[string]string{"api_key": "sk-kimi-whitelist-0123456789"}},
		{"账号导入", "anthropic", "oauth", "/import", map[string]any{"data": map[string]any{
			"platform": "anthropic", "type": "oauth", "credentials": map[string]any{
				"access_token": "whitelist-import-access", "refresh_token": "whitelist-import-refresh",
				"model_mapping": map[string]string{whitelistSonnet: "不可信别名"},
			},
		}}},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			var upstream modelWhitelistUpstream
			seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": path.platform, "auth_type": path.authType}, &upstream)
			base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
			if path.suffix == "/oauth/complete" {
				seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
			}
			if status, raw := seller.call(http.MethodPost, base+path.suffix, path.body, &upstream); status != http.StatusOK {
				t.Fatalf("接入失败：http %d %s", status, raw)
			}
			if upstream.Status != "selecting_models" || upstream.ModelWhitelist != nil ||
				!slices.Equal(upstream.ModelCatalog, []string{whitelistSonnet, whitelistOpus}) || upstream.ModelCatalogAt == nil {
				t.Fatalf("接入后应等待选择模型并缓存完整目录：%+v", upstream)
			}
			accountID := modelWhitelistAccountID(e, upstream.ID)
			creds, err := e.fake.AccountCredentials(accountID)
			if err != nil {
				t.Fatal(err)
			}
			if mapping, exists := creds["model_mapping"]; exists && mapping != nil {
				t.Fatalf("选择之前不得应用导出文件中的模型别名：%v", mapping)
			}
			var runs int
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
			if runs != 0 {
				t.Fatalf("选择模型之前不应入队上架检测：runs=%d", runs)
			}
			seller.expect(http.StatusConflict, http.MethodPost, base+"/listing", map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}})
			var catalog modelWhitelistCatalog
			seller.must(http.MethodGet, base+"/models", nil, &catalog)
			if !slices.Equal(catalog.Catalog, upstream.ModelCatalog) || catalog.Whitelist != nil || catalog.FetchedAt == nil {
				t.Fatalf("目录接口与上游详情字段不一致：%+v", catalog)
			}
			seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
			if upstream.Status != "ready" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistSonnet}) {
				t.Fatalf("选择模型后应进入未检测的可挂售状态：%+v", upstream)
			}
			assertModelWhitelistMapping(e, accountID, []string{whitelistSonnet})
			creds, err = e.fake.AccountCredentials(accountID)
			if err != nil || (creds["access_token"] == nil && creds["api_key"] == nil) {
				t.Fatalf("写入模型映射应保留登录凭证：%v %v", creds, err)
			}
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 0 {
				t.Fatalf("白名单提交不应自动入队检测：runs=%d", runs)
			}
			seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 0 {
				t.Fatalf("重复提交白名单也不应自动入队检测：runs=%d", runs)
			}
			seller.must(http.MethodPost, base+"/retry-verification", nil, &upstream)
			if upstream.Status != "verifying" {
				t.Fatalf("卖家主动检测才应进入检测状态：%+v", upstream)
			}
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 1 {
				t.Fatalf("主动检测应入队一次：runs=%d", runs)
			}
		})
	}
}

func TestModelWhitelistValidationRefreshAndMarket(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-market", 1)
	other := e.newUser("whitelist-other@market.test")
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	if status, raw := seller.call(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-market"}, &upstream); status != http.StatusOK {
		t.Fatalf("授权失败：http %d %s", status, raw)
	}
	accountID := modelWhitelistAccountID(e, upstream.ID)
	for _, body := range []any{map[string]any{"models": []string{}}, map[string]any{"models": []string{" "}}, map[string]any{"models": []string{"未知模型"}}} {
		seller.expect(http.StatusBadRequest, http.MethodPut, base+"/models", body)
	}
	for _, request := range []struct {
		method string
		suffix string
		body   any
	}{
		{http.MethodGet, "/models", nil},
		{http.MethodPost, "/models/refresh", nil},
		{http.MethodPut, "/models", map[string]any{"models": []string{whitelistSonnet}}},
	} {
		other.expect(http.StatusNotFound, request.method, base+request.suffix, request.body)
		e.anon().expect(http.StatusUnauthorized, request.method, base+request.suffix, request.body)
	}
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
	if runs != 0 {
		t.Fatalf("校验失败不应开始上架检测：runs=%d", runs)
	}
	e.fake.FailNext(http.MethodPost, "/api/v1/admin/accounts/bulk-update", http.StatusBadGateway)
	if status, raw := seller.call(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, nil); status < 500 {
		t.Fatalf("sub2api 写入失败应反馈依赖错误：http %d %s", status, raw)
	}
	seller.must(http.MethodGet, base, nil, &upstream)
	if upstream.Status != "selecting_models" || upstream.ModelWhitelist != nil {
		t.Fatalf("写入失败应保留待选择状态与未声明白名单：%+v", upstream)
	}
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
	if runs != 0 {
		t.Fatalf("写入失败不得入队检测：runs=%d", runs)
	}
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
	seller.must(http.MethodPost, base+"/retry-verification", nil, &upstream)
	e.passOnboarding(upstream.ID)
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must(http.MethodPost, base+"/listing", map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}}, &created)
	var detail struct {
		Models []string `json:"models"`
	}
	e.anon().must(http.MethodGet, fmt.Sprintf("/api/market/listings/%d", created.Listing.ID), nil, &detail)
	if !slices.Equal(detail.Models, []string{whitelistSonnet}) {
		t.Fatalf("市场详情应显示挂售模型：%v", detail.Models)
	}
	marketModels := func(model string) []struct {
		ListingID int64    `json:"listing_id"`
		Models    []string `json:"models"`
	} {
		t.Helper()
		var items []struct {
			ListingID int64    `json:"listing_id"`
			Models    []string `json:"models"`
		}
		e.anon().must(http.MethodGet, "/api/market/listings?model="+url.QueryEscape(model), nil, &items)
		return items
	}
	if items := marketModels(whitelistSonnet); len(items) != 1 || items[0].ListingID != created.Listing.ID || !slices.Equal(items[0].Models, []string{whitelistSonnet}) {
		t.Fatalf("模型筛选应返回已挂售该模型的上游：%+v", items)
	}
	if items := marketModels(whitelistOpus); len(items) != 0 {
		t.Fatalf("模型筛选不应返回白名单外上游：%+v", items)
	}
	e.fake.FailNext(http.MethodPost, "/api/v1/admin/accounts/bulk-update", http.StatusBadGateway)
	if status, raw := seller.call(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}, nil); status < 500 {
		t.Fatalf("修改名单写入失败应反馈依赖错误：http %d %s", status, raw)
	}
	seller.must(http.MethodGet, base, nil, &upstream)
	if upstream.Status != "listed" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistSonnet}) {
		t.Fatalf("修改名单写入失败应保留旧名单与挂单状态：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistSonnet})
	// 存量未声明白名单的上游继续允许全部模型。
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE upstreams SET model_whitelist = NULL WHERE id = $1", upstream.ID); err != nil {
		t.Fatal(err)
	}
	seller.must(http.MethodGet, base+"/models", nil, new(modelWhitelistCatalog))
	if items := marketModels(whitelistOpus); len(items) != 1 || items[0].Models == nil || len(items[0].Models) != 0 {
		t.Fatalf("未声明白名单应匹配任意模型且返回空数组：%+v", items)
	}
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}, &upstream)
	if upstream.Status != "listed" {
		t.Fatalf("修改挂售模型应保持挂单状态：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistOpus})
	if items := marketModels(whitelistSonnet); len(items) != 0 {
		t.Fatalf("修改白名单后市场筛选应立即生效：%+v", items)
	}
	// 完整目录通过直接同步更新，刷新目录不改变已选白名单。
	e.fake.SetAccountModels(accountID, []string{whitelistSonnet, whitelistOpus, "claude-haiku-4-5"})
	var catalog modelWhitelistCatalog
	seller.must(http.MethodPost, base+"/models/refresh", nil, &catalog)
	if !slices.Contains(catalog.Catalog, "claude-haiku-4-5") || !slices.Equal(catalog.Whitelist, []string{whitelistOpus}) || catalog.FetchedAt == nil {
		t.Fatalf("刷新完整目录不应改白名单：%+v", catalog)
	}
	// 直接同步失败时使用查询目录，部分平台只能返回映射内模型。
	e.fake.FailAccountModelsSync(accountID, http.StatusBadGateway)
	seller.must(http.MethodPost, base+"/models/refresh", nil, &catalog)
	if !slices.Contains(catalog.Catalog, whitelistOpus) || !slices.Contains(catalog.Catalog, "claude-haiku-4-5") ||
		!slices.Equal(catalog.Whitelist, []string{whitelistOpus}) || catalog.FetchedAt == nil {
		t.Fatalf("目录回退查询不应改白名单：%+v", catalog)
	}
	syncCount, queryCount := e.fake.AccountModelRequests(accountID)
	if syncCount < 3 || queryCount != 1 {
		t.Fatalf("应先同步，失败才回退查询：sync=%d query=%d", syncCount, queryCount)
	}
	beforeFailure := catalog
	e.fake.FailAccountModelsQuery(accountID, http.StatusBadGateway)
	seller.expect(http.StatusServiceUnavailable, http.MethodPost, base+"/models/refresh", nil)
	seller.must(http.MethodGet, base+"/models", nil, &catalog)
	if !slices.Equal(catalog.Catalog, beforeFailure.Catalog) || !slices.Equal(catalog.Whitelist, beforeFailure.Whitelist) ||
		catalog.FetchedAt == nil || !catalog.FetchedAt.Equal(*beforeFailure.FetchedAt) {
		t.Fatalf("两种获取方式都失败时应保留目录与获取时间：before=%+v after=%+v", beforeFailure, catalog)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistOpus})
	// 重新授权整体替换登录凭证之后恢复已保存的恒等映射。
	seller.must(http.MethodPost, base+"/reauthorize", nil, nil)
	seller.must(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-market"}, &upstream)
	if upstream.Status != "listed" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistOpus}) {
		t.Fatalf("重新授权应保留状态与挂售模型：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistOpus})
	creds, err := e.fake.AccountCredentials(accountID)
	if err != nil || creds["access_token"] == nil || creds["account_scheduling_threshold"] != float64(100) {
		t.Fatalf("重新授权应同时保留登录凭证与窗口阈值：%v %v", creds, err)
	}
}

func TestModelWhitelistEmptyCatalogAndImportedSelection(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-empty-import", 2)
	e.fake.SetAccountModels(0, []string{})
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	if status, raw := seller.call(http.MethodPost, base+"/import", map[string]any{"data": map[string]string{"access_token": "whitelist-empty-access"}}, &upstream); status != http.StatusOK {
		t.Fatalf("导入失败：http %d %s", status, raw)
	}
	if upstream.Status != "selecting_models" || upstream.ModelCatalog == nil || len(upstream.ModelCatalog) != 0 {
		t.Fatalf("空目录仍应等待卖家手动填写：%+v", upstream)
	}
	seller.expect(http.StatusBadRequest, http.MethodPut, base+"/models", map[string]any{"models": []string{}})
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{"custom-model-2026"}}, &upstream)
	if upstream.Status != "ready" || !slices.Equal(upstream.ModelWhitelist, []string{"custom-model-2026"}) {
		t.Fatalf("目录为空时应允许任意非空模型 ID：%+v", upstream)
	}
	assertModelWhitelistMapping(e, modelWhitelistAccountID(e, upstream.ID), []string{"custom-model-2026"})

	// 接入重试时已有的卖家选择应写入新账号，不能被导出文件中的别名替代。
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var retry modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &retry)
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE upstreams SET model_whitelist = $2 WHERE id = $1", retry.ID, []string{whitelistSonnet}); err != nil {
		t.Fatal(err)
	}
	if status, raw := seller.call(http.MethodPost, fmt.Sprintf("/api/seller/upstreams/%d/import", retry.ID), map[string]any{"data": map[string]any{
		"credentials": map[string]any{"access_token": "whitelist-retry-import-access", "model_mapping": map[string]string{whitelistOpus: "不可信别名"}},
	}}, &retry); status != http.StatusOK {
		t.Fatalf("已有白名单的导入失败：http %d %s", status, raw)
	}
	if !slices.Equal(retry.ModelWhitelist, []string{whitelistSonnet}) || !slices.Equal(retry.ModelCatalog, []string{whitelistSonnet, whitelistOpus}) {
		t.Fatalf("导入应保留既有选择并缓存完整目录：%+v", retry)
	}
	assertModelWhitelistMapping(e, modelWhitelistAccountID(e, retry.ID), []string{whitelistSonnet})
}

// 首跳账号拒绝白名单外的模型时，Edge 将 503 记为可重试并转到支持该模型的下一挂单。
func TestModelWhitelistEdgeFailover(t *testing.T) {
	e := setup(t)
	sellerFirst := prepareModelWhitelistSeller(e, "whitelist-edge-first", 2)
	sellerSecond := e.newUser("whitelist-edge-second@market.test")
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	createListing := func(seller *client, code, model string) (int64, int64) {
		t.Helper()
		var upstream modelWhitelistUpstream
		seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
		base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
		seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
		seller.must(http.MethodPost, base+"/oauth/complete", map[string]string{"code": code}, &upstream)
		e.passOnboarding(upstream.ID)
		// 完成上架后由卖家详情修改挂售模型，验证修改立即影响真正的转发。
		seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{model}}, &upstream)
		accountID := modelWhitelistAccountID(e, upstream.ID)
		assertModelWhitelistMapping(e, accountID, []string{model})
		var created struct {
			Listing idOnly `json:"listing"`
		}
		seller.must(http.MethodPost, base+"/listing", map[string]any{
			"mode": "consignment", "multiplier": 0.5, "join_default_pool": false, "categories": []string{"coding"},
		}, &created)
		return created.Listing.ID, accountID
	}
	listingFirst, accountFirst := createListing(sellerFirst, "whitelist-edge-first", whitelistOpus)
	listingSecond, accountSecond := createListing(sellerSecond, "whitelist-edge-second", whitelistSonnet)

	admin := e.login("admin@market.test", "admin-password-1")
	buyer := e.newUser("whitelist-edge-buyer@market.test")
	var recharge struct {
		Order idOnly `json:"order"`
	}
	buyer.must(http.MethodPost, "/api/buyer/recharge", map[string]any{"amount": 10}, &recharge)
	admin.must(http.MethodPost, fmt.Sprintf("/api/admin/recharges/%d/confirm", recharge.Order.ID), map[string]string{"provider_ref": "白名单回退测试充值"}, nil)
	e.drain()
	var key struct {
		Key    string `json:"key"`
		Detail idOnly `json:"detail"`
	}
	buyer.must(http.MethodPost, "/api/buyer/keys", map[string]any{
		"name": "挂售模型自动回退", "route": map[string]any{"type": "chain", "hops": []map[string]any{
			{"listing_id": listingFirst}, {"listing_id": listingSecond},
		}},
	}, &key)
	e.schedule()
	servedAccounts := make(chan int64, 4)
	e.fake.SetResponder(func(accountID int64, _ string) string {
		servedAccounts <- accountID
		return "模型白名单回退成功"
	})
	requestsBefore := len(e.fake.GatewayRequests())
	ed := e.startEdge()
	status, headers := ed.call(key.Key)
	if status != http.StatusOK || headers.Get("X-TM-Hop") != "2" || headers.Get("X-TM-Listing") != fmt.Sprintf("listing:%d", listingSecond) {
		t.Fatalf("白名单不匹配应转到第二挂单：http %d headers=%v", status, headers)
	}
	if got := len(e.fake.GatewayRequests()) - requestsBefore; got != 2 {
		t.Fatalf("应实际请求两跳 sub2api 分组：requests=%d", got)
	}
	if len(servedAccounts) != 1 {
		t.Fatalf("只有匹配白名单的第二账号应生成回复：served=%d", len(servedAccounts))
	}
	if got := <-servedAccounts; got != accountSecond || got == accountFirst {
		t.Fatalf("实际服务账号应为第二账号：got=%d first=%d second=%d", got, accountFirst, accountSecond)
	}
	ed.flushAttempts()
	rows, err := e.app.DB.Query(e.ctx, `SELECT edge_request_id, hop, listing_id, sub2api_request_id, http_status, committed, billable, end_reason
		FROM edge_attempts WHERE buyer_key_id = $1 ORDER BY hop`, key.Detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		edgeRequestID string
		hop           int
		listingID     int64
		requestID     string
		httpStatus    int
		committed     bool
		billable      bool
		endReason     string
	}
	var attempts []attempt
	for rows.Next() {
		var a attempt
		if err := rows.Scan(&a.edgeRequestID, &a.hop, &a.listingID, &a.requestID, &a.httpStatus, &a.committed, &a.billable, &a.endReason); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		attempts = append(attempts, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("同一次调用应记录两跳：%+v", attempts)
	}
	first, second := attempts[0], attempts[1]
	// billable 标识计费类请求路径；失败是否发生费用由底层用量记录确认。
	if first.hop != 1 || first.listingID != listingFirst || first.httpStatus != http.StatusServiceUnavailable ||
		first.endReason != "retry" || first.committed || !first.billable || first.requestID == "" {
		t.Fatalf("白名单外模型的首跳应为 503 可重试且未提交响应：%+v", first)
	}
	if second.hop != 2 || second.listingID != listingSecond || second.httpStatus != http.StatusOK ||
		second.endReason != "completed" || !second.committed || !second.billable || second.requestID == "" ||
		first.edgeRequestID != second.edgeRequestID {
		t.Fatalf("第二跳应完成同一请求并计费：%+v", second)
	}
	var rejectedUsage int
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT count(*) FROM usage_logs WHERE request_id = $1", "client:"+first.requestID), &rejectedUsage)
	if rejectedUsage != 0 {
		t.Fatalf("白名单不匹配的失败首跳不能产生用量：%d", rejectedUsage)
	}
	var servedAccountID int64
	var servedModel string
	mustScan(t, e.replica.QueryRow(e.ctx, "SELECT account_id, model FROM usage_logs WHERE request_id = $1", "client:"+second.requestID), &servedAccountID, &servedModel)
	if servedAccountID != accountSecond || servedModel != whitelistSonnet {
		t.Fatalf("底层用量应只归第二账号的请求模型：account=%d model=%s", servedAccountID, servedModel)
	}
}

type modelWhitelistTransport func(*http.Request) (*http.Response, error)

func (tr modelWhitelistTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return tr(req)
}

type modelWhitelistHTTPResult struct {
	status int
	raw    string
}

// 在准确的出站请求上设屏障，避免靠延时猜测目录或白名单写入的执行位置。
func blockModelWhitelistAdmin(e *env, match func(*http.Request) bool) (<-chan struct{}, func()) {
	e.t.Helper()
	entered, allowed := make(chan struct{}), make(chan struct{})
	var enteredOnce, allowedOnce sync.Once
	release := func() { allowedOnce.Do(func() { close(allowed) }) }
	e.t.Cleanup(release)
	transport := modelWhitelistTransport(func(req *http.Request) (*http.Response, error) {
		if match(req) {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-allowed:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		return http.DefaultTransport.RoundTrip(req)
	})
	e.app.Listings.Admin = adminclient.New(e.fake.URL(), e.fake.AdminKey, 15*time.Second,
		adminclient.WithHTTPClient(&http.Client{Transport: transport, Timeout: 15 * time.Second}))
	return entered, release
}

func waitModelWhitelistRequest(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("等待模型出站请求进入屏障超时")
	}
}

func waitModelWhitelistResult(t *testing.T, done <-chan modelWhitelistHTTPResult) modelWhitelistHTTPResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("等待模型操作完成超时")
		return modelWhitelistHTTPResult{}
	}
}

func TestModelWhitelistConcurrentOnboarding(t *testing.T) {
	for _, path := range []struct {
		name     string
		platform string
		authType string
		suffix   string
		body     any
	}{
		{"官方授权", "anthropic", "oauth", "/oauth/complete", map[string]string{"code": "whitelist-lock-oauth"}},
		{"Coding Key", "kimi", "coding", "/coding-key", map[string]string{"api_key": "sk-kimi-whitelist-lock-0123456789"}},
		{"账号导入", "anthropic", "oauth", "/import", map[string]any{"data": map[string]string{"access_token": "whitelist-lock-import-access"}}},
	} {
		t.Run(path.name, func(t *testing.T) {
			e := setup(t)
			seller := prepareModelWhitelistSeller(e, "whitelist-lock", 1)
			e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
			var upstream modelWhitelistUpstream
			seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": path.platform, "auth_type": path.authType}, &upstream)
			base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
			if path.suffix == "/oauth/complete" {
				seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
			}
			entered, release := blockModelWhitelistAdmin(e, func(req *http.Request) bool {
				return req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/models/sync-upstream")
			})
			done := make(chan modelWhitelistHTTPResult, 1)
			go func() {
				status, raw := seller.call(http.MethodPost, base+path.suffix, path.body, &upstream)
				done <- modelWhitelistHTTPResult{status, raw}
			}()
			waitModelWhitelistRequest(t, entered)
			if raw := seller.expect(http.StatusConflict, http.MethodPut, base+"/models", map[string]any{"models": []string{"outside-catalog"}}); !strings.Contains(raw, "UPSTREAM_BUSY") {
				t.Fatalf("目录未完成时提交应被共同的接入锁拒绝：%s", raw)
			}
			accountID := modelWhitelistAccountID(e, upstream.ID)
			assertModelWhitelistMapping(e, accountID, nil)
			var whitelist []string
			var catalogAt *time.Time
			var runs int
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT model_whitelist, model_catalog_at FROM upstreams WHERE id = $1", upstream.ID), &whitelist, &catalogAt)
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
			if whitelist != nil || catalogAt != nil || runs != 0 {
				t.Fatalf("目录请求期间不得写白名单或检测：models=%v catalog_at=%v runs=%d", whitelist, catalogAt, runs)
			}
			release()
			if result := waitModelWhitelistResult(t, done); result.status != http.StatusOK {
				t.Fatalf("放行目录后接入应完成：http %d %s", result.status, result.raw)
			}
			if upstream.Status != "selecting_models" || !slices.Equal(upstream.ModelCatalog, []string{whitelistSonnet, whitelistOpus}) {
				t.Fatalf("接入完成后应使用完整目录等待选择：%+v", upstream)
			}
			if raw := seller.expect(http.StatusBadRequest, http.MethodPut, base+"/models", map[string]any{"models": []string{"outside-catalog"}}); !strings.Contains(raw, "MODEL_NOT_IN_CATALOG") {
				t.Fatalf("放行后目录外模型仍应被校验拒绝：%s", raw)
			}
			assertModelWhitelistMapping(e, accountID, nil)
		})
	}
}

func TestModelWhitelistConcurrentRetry(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-retry-lock", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	seller.must(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-retry-lock"}, &upstream)
	e.passOnboarding(upstream.ID)
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
	accountID := modelWhitelistAccountID(e, upstream.ID)
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE upstreams SET status = 'verify_failed', failure_reason = '并发重试测试' WHERE id = $1", upstream.ID); err != nil {
		t.Fatal(err)
	}
	entered, release := blockModelWhitelistAdmin(e, func(req *http.Request) bool {
		return req.Method == http.MethodPost && req.URL.Path == "/api/v1/admin/accounts/bulk-update"
	})
	done := make(chan modelWhitelistHTTPResult, 1)
	go func() {
		status, raw := seller.call(http.MethodPost, base+"/retry-verification", nil, &upstream)
		done <- modelWhitelistHTTPResult{status, raw}
	}()
	waitModelWhitelistRequest(t, entered)
	if raw := seller.expect(http.StatusConflict, http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}); !strings.Contains(raw, "UPSTREAM_BUSY") {
		t.Fatalf("重试同步与修改白名单必须共用锁：%s", raw)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistSonnet})
	release()
	if result := waitModelWhitelistResult(t, done); result.status != http.StatusOK {
		t.Fatalf("放行后上架重试应成功：http %d %s", result.status, result.raw)
	}
	if upstream.Status != "verifying" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistSonnet}) {
		t.Fatalf("重试应保留原白名单并进入检测：%+v", upstream)
	}
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}, &upstream)
	if !slices.Equal(upstream.ModelWhitelist, []string{whitelistOpus}) {
		t.Fatalf("重试结束后白名单修改应生效：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistOpus})
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
	if runs != 2 {
		t.Fatalf("只应存在初次检测与本次重试两条运行：runs=%d", runs)
	}
}

type modelWhitelistQualityFunc func(context.Context, int64, string, string) error

func (hook modelWhitelistQualityFunc) StartOnboardingCheck(ctx context.Context, upstreamID int64, platform, authType string) error {
	return hook(ctx, upstreamID, platform, authType)
}

// 显式复用依赖并创建新服务，避免复制服务内部的并发锁与 sync.Once。
func newModelWhitelistListingService(e *env) *listing.Service {
	e.t.Helper()
	s := e.app.Listings
	return &listing.Service{DB: s.DB, Admin: s.Admin, Prov: s.Prov, Keyring: s.Keyring,
		Quality: s.Quality, Kicker: s.Kicker, Settings: s.Settings, Log: s.Log, Now: s.Now}
}

func TestModelWhitelistCanceledVerificationRecovery(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-canceled", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var sellerInfo idOnly
	seller.must(http.MethodGet, "/api/me", nil, &sellerInfo)
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	if status, raw := seller.call(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-canceled"}, &upstream); status != http.StatusOK {
		t.Fatalf("授权失败：http %d %s", status, raw)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	service := newModelWhitelistListingService(e)
	if _, err := service.SetModels(e.ctx, sellerInfo.ID, upstream.ID, []string{whitelistSonnet}); err != nil {
		t.Fatalf("未检测挂售前保存模型选择：%v", err)
	}
	service.Quality = modelWhitelistQualityFunc(func(context.Context, int64, string, string) error {
		cancel()
		return context.Canceled
	})
	if _, err := service.RetryVerification(ctx, sellerInfo.ID, upstream.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("主动检测入队取消应返回取消错误：%v", err)
	}
	u, err := listing.GetSellerUpstream(e.ctx, e.app.DB, sellerInfo.ID, upstream.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != listing.StatusVerifyFailed || !slices.Equal(u.ModelWhitelist, []string{whitelistSonnet}) || u.FailureReason == nil {
		t.Fatalf("请求取消后应保留选择并清理为可重试状态：%+v", u)
	}
	assertModelWhitelistMapping(e, modelWhitelistAccountID(e, upstream.ID), []string{whitelistSonnet})
	seller.must(http.MethodPost, base+"/retry-verification", nil, &upstream)
	if upstream.Status != "verifying" {
		t.Fatalf("取消失败后应可通过正常入口重新开始检测：%+v", upstream)
	}
}

func TestModelWhitelistLostWriteResponseCompensation(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-response-lost", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	seller.must(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-response-lost"}, &upstream)
	e.passOnboarding(upstream.ID)
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
	accountID := modelWhitelistAccountID(e, upstream.ID)
	var dropOnce sync.Once
	written := make(chan map[string]string, 1)
	transport := modelWhitelistTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || req.Method != http.MethodPost || req.URL.Path != "/api/v1/admin/accounts/bulk-update" {
			return resp, err
		}
		drop := false
		dropOnce.Do(func() { drop = true })
		if !drop {
			return resp, nil
		}
		// 先读完替身已提交事务的成功响应，再模拟客户端未收到响应。
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		mapping, err := e.fake.AccountModelMapping(accountID)
		if err != nil {
			return nil, err
		}
		written <- mapping
		return nil, errors.New("模拟白名单已写入但响应丢失")
	})
	e.app.Listings.Admin = adminclient.New(e.fake.URL(), e.fake.AdminKey, 15*time.Second,
		adminclient.WithHTTPClient(&http.Client{Transport: transport, Timeout: 15 * time.Second}))
	if status, raw := seller.call(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}, nil); status < 500 {
		t.Fatalf("丢失写入响应应反馈依赖失败：http %d %s", status, raw)
	}
	select {
	case mapping := <-written:
		if !reflect.DeepEqual(mapping, map[string]string{whitelistOpus: whitelistOpus}) {
			t.Fatalf("应确实完成新白名单写入后才丢响应：%v", mapping)
		}
	default:
		t.Fatal("未模拟到成功写入后响应丢失")
	}
	seller.must(http.MethodGet, base, nil, &upstream)
	if upstream.Status != "ready" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistSonnet}) {
		t.Fatalf("响应丢失后本地应保留原状态和白名单：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistSonnet})
	seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistOpus}}, &upstream)
	if !slices.Equal(upstream.ModelWhitelist, []string{whitelistOpus}) {
		t.Fatalf("补偿后应可再次正常修改：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, []string{whitelistOpus})
}

func TestModelWhitelistLegacyRetryRemainsUnrestricted(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-legacy-retry", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	seller.must(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-legacy-retry"}, &upstream)
	e.passOnboarding(upstream.ID)
	accountID := modelWhitelistAccountID(e, upstream.ID)
	if _, err := e.app.Admin.BulkUpdateAccounts(e.ctx, adminclient.BulkUpdateAccountsInput{
		AccountIDs: []int64{accountID}, Credentials: map[string]any{"model_mapping": map[string]string{}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE upstreams SET status = 'verify_failed', model_whitelist = NULL, failure_reason = '存量重试测试' WHERE id = $1", upstream.ID); err != nil {
		t.Fatal(err)
	}
	seller.must(http.MethodPost, base+"/retry-verification", nil, &upstream)
	if upstream.Status != "verifying" || upstream.ModelWhitelist != nil {
		t.Fatalf("存量未声明名单重试应继续不限模型检测：%+v", upstream)
	}
	assertModelWhitelistMapping(e, accountID, nil)
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
	if runs != 2 {
		t.Fatalf("存量重试应正常入队：runs=%d", runs)
	}
}

func TestModelWhitelistSmallPoolOperationSlots(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-small-pool", 2)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var sellerInfo idOnly
	seller.must(http.MethodGet, "/api/me", nil, &sellerInfo)
	var upstreams [2]modelWhitelistUpstream
	for n := range upstreams {
		u := &upstreams[n]
		seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, u)
		base := fmt.Sprintf("/api/seller/upstreams/%d", u.ID)
		seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
		if status, raw := seller.call(http.MethodPost, base+"/oauth/complete", map[string]string{"code": fmt.Sprintf("whitelist-small-pool-%d", n)}, u); status != http.StatusOK {
			t.Fatalf("授权失败：http %d %s", status, raw)
		}
	}
	poolConfig := e.app.DB.Config()
	poolConfig.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(e.ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	entered, release := blockModelWhitelistAdmin(e, func(req *http.Request) bool {
		return req.Method == http.MethodPost && req.URL.Path == "/api/v1/admin/accounts/bulk-update"
	})
	service := newModelWhitelistListingService(e)
	service.DB = pool
	type result struct {
		upstream *listing.Upstream
		err      error
	}
	done := make(chan result, 1)
	go func() {
		u, err := service.SetModels(e.ctx, sellerInfo.ID, upstreams[0].ID, []string{whitelistSonnet})
		done <- result{u, err}
	}()
	waitModelWhitelistRequest(t, entered)
	busyCtx, busyCancel := context.WithTimeout(e.ctx, time.Second)
	_, err = service.SetModels(busyCtx, sellerInfo.ID, upstreams[1].ID, []string{whitelistOpus})
	busyCancel()
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusConflict || httpErr.Code != "UPSTREAM_BUSY" {
		t.Fatalf("不同上游操作槽满时应立即拒绝，不耗尽连接：%v", err)
	}
	queryCtx, queryCancel := context.WithTimeout(e.ctx, time.Second)
	var available int
	err = pool.QueryRow(queryCtx, "SELECT 1").Scan(&available)
	queryCancel()
	if err != nil || available != 1 {
		t.Fatalf("操作槽应为普通数据库查询保留连接：value=%d err=%v", available, err)
	}
	release()
	select {
	case first := <-done:
		if first.err != nil || first.upstream.Status != listing.StatusReady {
			t.Fatalf("放行后首个操作应完成：upstream=%+v err=%v", first.upstream, first.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("小连接池首个白名单操作未完成")
	}
	second, err := service.SetModels(e.ctx, sellerInfo.ID, upstreams[1].ID, []string{whitelistOpus})
	if err != nil || second.Status != listing.StatusReady || !slices.Equal(second.ModelWhitelist, []string{whitelistOpus}) {
		t.Fatalf("操作槽释放后第二上游应可继续：upstream=%+v err=%v", second, err)
	}
	assertModelWhitelistMapping(e, modelWhitelistAccountID(e, upstreams[0].ID), []string{whitelistSonnet})
	assertModelWhitelistMapping(e, modelWhitelistAccountID(e, upstreams[1].ID), []string{whitelistOpus})
}

func TestModelWhitelistDeletionCompareAndSwap(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "whitelist-deletion-cas", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet, whitelistOpus})
	var sellerInfo idOnly
	seller.must(http.MethodGet, "/api/me", nil, &sellerInfo)
	var upstream modelWhitelistUpstream
	seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
	seller.must(http.MethodPost, base+"/oauth/start", nil, nil)
	if status, raw := seller.call(http.MethodPost, base+"/oauth/complete", map[string]string{"code": "whitelist-deletion-cas"}, &upstream); status != http.StatusOK {
		t.Fatalf("授权失败：http %d %s", status, raw)
	}
	accountID := modelWhitelistAccountID(e, upstream.ID)
	var changeOnce sync.Once
	written := make(chan map[string]string, 1)
	transport := modelWhitelistTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || req.Method != http.MethodPost || req.URL.Path != "/api/v1/admin/accounts/bulk-update" {
			return resp, err
		}
		var changeErr error
		changeOnce.Do(func() {
			mapping, err := e.fake.AccountModelMapping(accountID)
			if err != nil {
				changeErr = err
				return
			}
			written <- mapping
			// 模拟后台清理推进删除，账号写入成功后本地状态已发生变化。
			_, changeErr = e.app.DB.Exec(e.ctx, "UPDATE upstreams SET status = 'removing' WHERE id = $1", upstream.ID)
		})
		if changeErr != nil {
			_ = resp.Body.Close()
			return nil, changeErr
		}
		return resp, nil
	})
	e.app.Listings.Admin = adminclient.New(e.fake.URL(), e.fake.AdminKey, 15*time.Second,
		adminclient.WithHTTPClient(&http.Client{Transport: transport, Timeout: 15 * time.Second}))
	if _, err := e.app.Listings.SetModels(e.ctx, sellerInfo.ID, upstream.ID, []string{whitelistSonnet}); !errors.Is(err, listing.ErrBadState) {
		t.Fatalf("状态已变更时应拒绝白名单提交：%v", err)
	}
	select {
	case mapping := <-written:
		if !reflect.DeepEqual(mapping, map[string]string{whitelistSonnet: whitelistSonnet}) {
			t.Fatalf("删除竞态应发生在真实写入新映射后：%v", mapping)
		}
	default:
		t.Fatal("未进入账号写入成功后的删除竞态")
	}
	u, err := listing.GetSellerUpstream(e.ctx, e.app.DB, sellerInfo.ID, upstream.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != listing.StatusRemoving || u.ModelWhitelist != nil {
		t.Fatalf("CAS 失败不能恢复删除中的上游或保存新名单：%+v", u)
	}
	assertModelWhitelistMapping(e, accountID, nil)
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
	if runs != 0 {
		t.Fatalf("删除中的上游不能入队检测：runs=%d", runs)
	}
}

func TestModelWhitelistPrivacyFailureRequiresSelection(t *testing.T) {
	for _, authPath := range []string{"官方授权", "账号导入"} {
		t.Run(authPath, func(t *testing.T) {
			e := setup(t)
			seller := prepareModelWhitelistSeller(e, "whitelist-privacy", 1)
			admin := e.login("admin@market.test", "admin-password-1")
			admin.must(http.MethodPut, "/api/admin/vendors/openai", map[string]bool{"enabled": true}, nil)
			models := []string{"gpt-6-astra", "gpt-6.1-sol"}
			e.fake.SetAccountModels(0, models)
			var upstream modelWhitelistUpstream
			seller.must(http.MethodPost, "/api/seller/upstreams", map[string]string{"platform": "openai", "auth_type": "oauth"}, &upstream)
			base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
			var privacyCalls atomic.Int32
			transport := modelWhitelistTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/set-privacy") && privacyCalls.Add(1) == 1 {
					e.fake.FailNext(req.Method, req.URL.Path, http.StatusServiceUnavailable)
				}
				return http.DefaultTransport.RoundTrip(req)
			})
			e.app.Listings.Admin = adminclient.New(e.fake.URL(), e.fake.AdminKey, 15*time.Second,
				adminclient.WithHTTPClient(&http.Client{Transport: transport, Timeout: 15 * time.Second}))
			var status int
			var raw string
			if authPath == "官方授权" {
				var start struct {
					AuthURL string `json:"auth_url"`
				}
				seller.must(http.MethodPost, base+"/oauth/start", nil, &start)
				authorizeURL, err := url.Parse(start.AuthURL)
				if err != nil {
					t.Fatal(err)
				}
				callback := "https://cb.test/cb?code=whitelist-privacy-oauth&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
				status, raw = seller.call(http.MethodPost, base+"/oauth/complete", map[string]string{"code": callback}, &upstream)
			} else {
				status, raw = seller.call(http.MethodPost, base+"/import", map[string]any{"data": map[string]any{
					"platform": "openai", "type": "oauth", "credentials": map[string]string{
						"access_token": "whitelist-privacy-import-access", "refresh_token": "whitelist-privacy-import-refresh",
						"chatgpt_account_id": "whitelist-privacy-import-id", "plan_type": "plus", "email": "whitelist-privacy@example.test",
					},
				}}, &upstream)
			}
			if status != http.StatusOK || upstream.Status != "selecting_models" || upstream.ModelWhitelist != nil ||
				!slices.Equal(upstream.ModelCatalog, models) || upstream.PrivacyOK != nil || privacyCalls.Load() != 0 {
				t.Fatalf("新账号必须先选模型再检查隐私：http %d raw=%s upstream=%+v privacy_calls=%d", status, raw, upstream, privacyCalls.Load())
			}
			accountID := modelWhitelistAccountID(e, upstream.ID)
			seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{"gpt-6-astra"}}, &upstream)
			if upstream.Status != "verify_failed" || !slices.Equal(upstream.ModelWhitelist, []string{"gpt-6-astra"}) ||
				upstream.PrivacyOK == nil || *upstream.PrivacyOK || privacyCalls.Load() != 1 {
				t.Fatalf("隐私失败应保留已选择模型并允许重试：upstream=%+v privacy_calls=%d", upstream, privacyCalls.Load())
			}
			assertModelWhitelistMapping(e, accountID, []string{"gpt-6-astra"})
			var runs int
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", upstream.ID), &runs)
			if runs != 0 {
				t.Fatalf("隐私设置失败之前不能入队检测：runs=%d", runs)
			}
			seller.must(http.MethodPost, base+"/retry-verification", nil, &upstream)
			if upstream.Status != "verifying" || !slices.Equal(upstream.ModelWhitelist, []string{"gpt-6-astra"}) ||
				upstream.PrivacyOK == nil || !*upstream.PrivacyOK || privacyCalls.Load() != 2 {
				t.Fatalf("隐私恢复后重试应保留选择并直接检测：upstream=%+v privacy_calls=%d", upstream, privacyCalls.Load())
			}
			assertModelWhitelistMapping(e, accountID, []string{"gpt-6-astra"})
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 1 {
				t.Fatalf("隐私恢复后仅入队一次上架检测：runs=%d", runs)
			}
		})
	}
}
