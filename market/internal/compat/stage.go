package compat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/contract"
)

// CheckContractVersion 检查本系统部署的契约是否已经登记了目标版本与升级后的迁移指纹。
// 升级顺序要求先更新并部署本系统（契约中加入新版本），再升级 sub2api；否则升级后运行时自检
// 会进入安全模式、暂停所有写入。升级后的迁移集合 = 当前已执行 ∪ 目标版本全部迁移。
func CheckContractVersion(spec *contract.Spec, version string, current Applied, target *MigrationSet) []Finding {
	names := map[string]bool{}
	for _, n := range current.Names() {
		names[n] = true
	}
	for _, n := range target.Names {
		names[n] = true
	}
	all := make([]string, 0, len(names))
	for n := range names {
		all = append(all, n)
	}
	sort.Strings(all)
	upgraded := contract.MigrationFingerprint(all)
	fresh := contract.MigrationFingerprint(target.Names)

	v, ok := spec.Verified(version)
	if !ok {
		return []Finding{{
			Severity: SeverityConflict,
			Rule:     "contract_version_missing",
			Detail: fmt.Sprintf("契约未登记 sub2api %s：请先验证兼容性，在 contract.yaml 的 verified_versions 中加入该版本"+
				"（升级后指纹 %s，全新安装指纹 %s），发布本系统后再升级 sub2api", version, upgraded, fresh),
		}}
	}
	for _, fp := range v.MigrationFingerprints {
		if fp == upgraded {
			return nil
		}
	}
	return []Finding{{
		Severity: SeverityConflict,
		Rule:     "contract_fingerprint_missing",
		Detail: fmt.Sprintf("契约中的 %s 没有登记本环境升级后的迁移指纹 %s（已登记 %s）；升级后运行时自检会进入安全模式",
			version, upgraded, strings.Join(v.MigrationFingerprints, ", ")),
	}}
}

// RouteResult 是一次接口探测的结果。
type RouteResult struct {
	API     string `json:"api"` // admin / user / gateway
	Method  string `json:"method"`
	Path    string `json:"path"`
	Status  int    `json:"status"`
	Present bool   `json:"present"`
	Note    string `json:"note,omitempty"`
}

// examplePath 把路径参数替换成探测用的值。
func examplePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		switch {
		case seg == ":model":
			parts[i] = "gemini-2.5-pro:generateContent"
		case strings.HasPrefix(seg, ":"):
			parts[i] = "0"
		}
	}
	return strings.Join(parts, "/")
}

// ProbeRoutes 不带任何凭证请求契约中的全部接口，确认它们在目标 sub2api 上仍然注册：
// 已注册的接口会被鉴权或参数校验拦下（401/400/423 等 JSON 响应），未注册的接口返回 gin 默认的纯文本 404。
// 不带凭证的请求不会产生任何写入，可以对预发环境直接执行。同时检查网关响应是否带有契约要求的响应头。
func ProbeRoutes(ctx context.Context, baseURL string, spec *contract.Spec, hc *http.Client) ([]RouteResult, []Finding, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(baseURL, "/")
	groups := []struct {
		name string
		eps  []contract.EndpointSpec
	}{{"admin", spec.AdminAPI}, {"user", spec.UserAPI}, {"gateway", spec.GatewayAPI}}
	var results []RouteResult
	var findings []Finding
	headerSeen := map[string]bool{}
	for _, g := range groups {
		for _, ep := range g.eps {
			var body io.Reader
			if ep.Method != http.MethodGet && ep.Method != http.MethodDelete {
				body = strings.NewReader("{}")
			}
			req, err := http.NewRequestWithContext(ctx, ep.Method, base+examplePath(ep.Path), body)
			if err != nil {
				return nil, nil, err
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := hc.Do(req)
			if err != nil {
				return nil, nil, fmt.Errorf("%s %s: %w", ep.Method, ep.Path, err)
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			r := RouteResult{API: g.name, Method: ep.Method, Path: ep.Path, Status: resp.StatusCode, Present: true}
			if resp.StatusCode == http.StatusNotFound && !strings.Contains(resp.Header.Get("Content-Type"), "json") {
				r.Present = false
				r.Note = "route not registered"
				findings = append(findings, Finding{Severity: SeverityConflict, Rule: "route_missing",
					Detail: fmt.Sprintf("%s 接口 %s %s 在目标 sub2api 上不存在", g.name, ep.Method, ep.Path)})
			}
			if g.name == "gateway" {
				for h := range spec.ResponseHeaders {
					if resp.Header.Get(h) != "" {
						headerSeen[h] = true
					}
				}
			}
			results = append(results, r)
		}
	}
	if len(spec.GatewayAPI) > 0 {
		for h := range spec.ResponseHeaders {
			if !headerSeen[h] {
				findings = append(findings, Finding{Severity: SeverityConflict, Rule: "response_header_missing",
					Detail: fmt.Sprintf("网关响应没有返回 %s，Edge 无法与 usage_logs 对账", h)})
			}
		}
	}
	return results, findings, nil
}

// RoutesMarkdown 渲染接口探测结果。
func RoutesMarkdown(results []RouteResult, findings []Finding) string {
	var b strings.Builder
	b.WriteString("# sub2api 接口探测\n\n")
	if len(findings) == 0 {
		b.WriteString("结论：**通过**，契约中的接口全部存在。\n\n")
	} else {
		fmt.Fprintf(&b, "结论：**阻止升级**，%d 项问题。\n\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "- [%s] %s：%s\n", f.Severity, f.Rule, f.Detail)
		}
		b.WriteString("\n")
	}
	b.WriteString("| 类别 | 方法 | 路径 | 状态码 | 结果 |\n|---|---|---|---|---|\n")
	for _, r := range results {
		res := "存在"
		if !r.Present {
			res = "**缺失**"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | %d | %s |\n", r.API, r.Method, r.Path, r.Status, res)
	}
	return b.String()
}
