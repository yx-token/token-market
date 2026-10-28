//go:build integration

package e2e

import (
	"fmt"
	"slices"
	"testing"
)

// 寄售上游覆盖 sub2api 0.2.14 支持的全部订阅类接入（决策 D22）：Gemini Google One 与 Code Assist（可填 GCP 项目）、
// 智谱 / MiniMax Coding Plan 国际站、智谱团队版、Antigravity、Grok；管理员按厂商选择开放哪些接入方式。
func TestVendorAuthTypeCoverage(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	for _, p := range []string{"gemini", "antigravity", "grok", "zhipu", "minimax"} {
		admin.must("PUT", "/api/admin/vendors/"+p, map[string]any{"enabled": true}, nil)
	}
	for i := 1; i <= 10; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "http", "host": "10.0.2.1", "port": 5000 + i, "region": "us"}, nil)
	}

	// 管理后台：每家已实现的接入方式，以及只能在其中选择开放哪些
	var vendors []struct {
		Platform  string   `json:"platform"`
		AuthTypes []string `json:"auth_types"`
		Supported []string `json:"supported_auth_types"`
	}
	admin.must("GET", "/api/admin/vendors", nil, &vendors)
	// supported_auth_types 按 authTypeOrder（oauth 在 coding 之前）排列；
	// 迁移按 array_append 追加，所以已开放列表里 oauth 排在既有值之后。
	wantSupported := map[string][]string{
		"anthropic": {"oauth", "setup-token"}, "openai": {"oauth"}, "gemini": {"google_one", "oauth"},
		"antigravity": {"oauth"}, "grok": {"oauth"},
		"kimi":    {"oauth", "coding"},
		"zhipu":   {"oauth", "oauth-intl", "coding", "coding-intl"},
		"minimax": {"coding", "coding-intl"}, "opencode_go": {"go"},
	}
	wantOpen := map[string][]string{
		"anthropic": {"oauth", "setup-token"}, "openai": {"oauth"}, "gemini": {"google_one", "oauth"},
		"antigravity": {"oauth"}, "grok": {"oauth"},
		"kimi":    {"coding", "oauth"},
		"zhipu":   {"coding", "coding-intl", "oauth", "oauth-intl"},
		"minimax": {"coding", "coding-intl"}, "opencode_go": {"go"},
	}
	for _, v := range vendors {
		if !slices.Equal(v.Supported, wantSupported[v.Platform]) || !slices.Equal(v.AuthTypes, wantOpen[v.Platform]) {
			t.Fatalf("%s: open %v, supported %v, want open %v / supported %v",
				v.Platform, v.AuthTypes, v.Supported, wantOpen[v.Platform], wantSupported[v.Platform])
		}
	}
	if len(vendors) != len(wantOpen) {
		t.Fatalf("vendors: %+v", vendors)
	}
	admin.expect(400, "PUT", "/api/admin/vendors/kimi", map[string]any{"auth_types": []string{"coding-intl"}})
	admin.expect(400, "PUT", "/api/admin/vendors/kimi", map[string]any{"auth_types": []string{}})

	seller := e.newUser("vendors@market.test")
	type upstream struct {
		ID           int64   `json:"id"`
		Status       string  `json:"status"`
		Tier         *string `json:"tier"`
		AccountEmail *string `json:"account_email"`
	}
	create := func(platform, authType string) upstream {
		t.Helper()
		var u upstream
		seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": platform, "auth_type": authType}, &u)
		return u
	}
	creds := func(upstreamID int64) map[string]any {
		t.Helper()
		var acc int64
		mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", upstreamID), &acc)
		c, err := e.fake.AccountCredentials(acc)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	oauth := func(u upstream, start map[string]any, code string) upstream {
		t.Helper()
		var st struct {
			AuthURL string `json:"auth_url"`
		}
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", u.ID), start, &st)
		state := st.AuthURL[len("https://auth.example.test/authorize?state="):]
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", u.ID), map[string]string{"code": "https://cb.test/cb?code=" + code + "&state=" + state}, &u)
		if u.Status != "verifying" {
			t.Fatalf("upstream %d after authorization: %+v", u.ID, u)
		}
		return u
	}

	// Gemini Google One：个人订阅，不接受项目 ID
	g1 := create("gemini", "google_one")
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", g1.ID), map[string]any{"project_id": "my-gcp-project"})
	g1 = oauth(g1, nil, "gina")
	if c := creds(g1.ID); c["oauth_type"] != "google_one" || c["project_id"] != "proj-gina" || g1.Tier == nil || *g1.Tier != "google_ai_pro" {
		t.Fatalf("google one: %+v %v", g1, c)
	}

	// Gemini Code Assist：卖家填写的 GCP 项目 ID 传给 sub2api
	ca := create("gemini", "oauth")
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", ca.ID), map[string]any{"project_id": "Bad Project"})
	ca = oauth(ca, map[string]any{"project_id": "my-gcp-project-1"}, "carl")
	if c := creds(ca.ID); c["oauth_type"] != "code_assist" || c["project_id"] != "my-gcp-project-1" || ca.Tier == nil || *ca.Tier != "gcp_standard" {
		t.Fatalf("code assist: %+v %v", ca, c)
	}

	// Antigravity、Grok OAuth
	ag := oauth(create("antigravity", "oauth"), nil, "amy")
	gk := oauth(create("grok", "oauth"), nil, "gus")
	if ag.Tier == nil || *ag.Tier != "pro" || gk.Tier == nil || *gk.Tier != "supergrok" {
		t.Fatalf("antigravity %+v grok %+v", ag, gk)
	}
	// 能拿到官方账号邮箱的平台记录邮箱；Gemini 的令牌信息里没有邮箱
	if ag.AccountEmail == nil || *ag.AccountEmail != "amy@example.test" || gk.AccountEmail == nil || *gk.AccountEmail != "gus@example.test" ||
		g1.AccountEmail != nil || ca.AccountEmail != nil {
		t.Fatalf("account emails: antigravity %+v grok %+v gemini %+v %+v", ag, gk, g1, ca)
	}

	codingKey := func(u upstream, body map[string]any) upstream {
		t.Helper()
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", u.ID), body, &u)
		if u.Status != "verifying" {
			t.Fatalf("coding plan upstream %d: %+v", u.ID, u)
		}
		return u
	}

	// 智谱国际站：使用内置的 z.ai 官方地址；国际站没有团队版
	zi := create("zhipu", "coding-intl")
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", zi.ID), map[string]any{"api_key": "zai-coding-key-0123456789", "organization": "org-1"})
	zi = codingKey(zi, map[string]any{"api_key": "zai-coding-key-0123456789"})
	if c := creds(zi.ID); c["base_url"] != "https://api.z.ai/api/coding/paas/v4" || c["account_mode"] != "coding" {
		t.Fatalf("zhipu intl credentials: %v", c)
	}
	e.passOnboarding(zi.ID)

	// 智谱国内站团队版：组织 / 项目 ID 写入凭证，不填地址
	zt := create("zhipu", "coding")
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", zt.ID), map[string]any{"api_key": "glm-team-key-0123456789", "project": "p1"})
	seller.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", zt.ID), map[string]any{"api_key": "glm-team-key-0123456789", "organization": "org 1"})
	zt = codingKey(zt, map[string]any{"api_key": "glm-team-key-0123456789", "organization": "org-123", "project": "proj_1"})
	if c := creds(zt.ID); c["zhipu_organization"] != "org-123" || c["zhipu_project"] != "proj_1" || c["base_url"] != nil {
		t.Fatalf("zhipu team credentials: %v", c)
	}

	// MiniMax 国际站
	mi := codingKey(create("minimax", "coding-intl"), map[string]any{"api_key": "minimax-intl-key-0123456789"})
	if c := creds(mi.ID); c["base_url"] != "https://api.minimax.io/v1" {
		t.Fatalf("minimax intl credentials: %v", c)
	}
	mc := codingKey(create("minimax", "coding"), map[string]any{"api_key": "minimax-cn-key-0123456789"})
	if c := creds(mc.ID); c["base_url"] != nil {
		t.Fatalf("minimax cn credentials: %v", c)
	}

	// 各接入方式都能通过上架检测（Gemini、Antigravity 走 Gemini 原生协议，其余按平台协议）
	for _, u := range []upstream{g1, ca, ag, gk, zt, mi, mc} {
		e.passOnboarding(u.ID)
	}

	// 管理员关闭某种接入方式后卖家不能再用它接入
	admin.must("PUT", "/api/admin/vendors/minimax", map[string]any{"auth_types": []string{"coding"}}, nil)
	seller.expect(400, "POST", "/api/seller/upstreams", map[string]string{"platform": "minimax", "auth_type": "coding-intl"})
	var mm struct {
		AuthTypes []string `json:"auth_types"`
	}
	var market []struct {
		Platform  string   `json:"platform"`
		AuthTypes []string `json:"auth_types"`
	}
	seller.must("GET", "/api/market/vendors", nil, &market)
	for _, v := range market {
		if v.Platform == "minimax" {
			mm.AuthTypes = v.AuthTypes
		}
	}
	if !slices.Equal(mm.AuthTypes, []string{"coding"}) {
		t.Fatalf("market vendors: %+v", market)
	}
}
