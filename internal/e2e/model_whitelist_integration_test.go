//go:build integration

package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"
)

type modelWhitelistUpstream struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	ModelWhitelist []string   `json:"model_whitelist"`
	ModelCatalog   []string   `json:"model_catalog"`
	ModelCatalogAt *time.Time `json:"model_catalog_at"`
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

// 三种卖家接入方式均先缓存完整目录，卖家选择之前不能创建上架检测。
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
			if upstream.Status != "verifying" || !slices.Equal(upstream.ModelWhitelist, []string{whitelistSonnet}) {
				t.Fatalf("选择模型后应开始上架检测：%+v", upstream)
			}
			assertModelWhitelistMapping(e, accountID, []string{whitelistSonnet})
			creds, err = e.fake.AccountCredentials(accountID)
			if err != nil || (creds["access_token"] == nil && creds["api_key"] == nil) {
				t.Fatalf("写入模型映射应保留登录凭证：%v %v", creds, err)
			}
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 1 {
				t.Fatalf("白名单提交应只入队一次上架检测：runs=%d", runs)
			}
			seller.must(http.MethodPut, base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &upstream)
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND trigger = 'onboarding'", upstream.ID), &runs)
			if runs != 1 {
				t.Fatalf("重复提交白名单不应重复入队：runs=%d", runs)
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
	if upstream.Status != "verifying" || !slices.Equal(upstream.ModelWhitelist, []string{"custom-model-2026"}) {
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
