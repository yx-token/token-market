package listing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
)

// CheckProxy 的 sellerID 非空时必须先检查所有权，不能让卖家探测他人的代理。
func (s *ProxyService) CheckProxy(ctx context.Context, sellerID *int64, id int64, quality bool) (*Proxy, error) {
	var p *Proxy
	var err error
	if sellerID != nil {
		p, err = s.GetSeller(ctx, *sellerID, id)
	} else {
		p, err = s.Get(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	if p.Sub2APIProxyID == nil {
		return nil, httpx.Conflict("PROXY_NOT_SYNCED", "synchronize the proxy before testing it")
	}
	var report any
	var country, countryCode string
	if quality {
		var result *adminclient.ProxyQualityResult
		result, err = s.Admin.CheckProxyQuality(ctx, *p.Sub2APIProxyID)
		if result != nil {
			report, country, countryCode = result, result.Country, result.CountryCode
		}
	} else {
		var result *adminclient.ProxyTestResult
		result, err = s.Admin.TestProxy(ctx, *p.Sub2APIProxyID)
		if result != nil {
			report = result
			if result.Success {
				country, countryCode = result.Country, result.CountryCode
			}
		}
	}
	if err != nil {
		if adminclient.StatusOf(err) == 404 {
			return nil, httpx.Unavailable("PROXY_CHECK_UNAVAILABLE", "sub2api proxy or diagnostic endpoint is unavailable")
		}
		return nil, httpx.Unavailable("PROXY_CHECK_FAILED", "proxy diagnostic request failed; please retry")
	}
	// 出站服务的错误文本可能包含代理 URL，保存报告前清除密码。
	var encrypted []byte
	if err := s.DB.QueryRow(ctx, "SELECT password_enc FROM proxies WHERE id = $1", id).Scan(&encrypted); err != nil {
		return nil, err
	}
	if len(encrypted) > 0 {
		password, err := s.Keyring.OpenString(encrypted, fmt.Sprintf("proxy:%d", id))
		if err != nil {
			return nil, err
		}
		redact := func(text string) string {
			if password == "" {
				return text
			}
			for _, secret := range []string{url.QueryEscape(password), url.PathEscape(password), password} {
				text = strings.ReplaceAll(text, secret, "[redacted]")
			}
			return text
		}
		switch result := report.(type) {
		case *adminclient.ProxyTestResult:
			result.Message = redact(result.Message)
		case *adminclient.ProxyQualityResult:
			result.Summary = redact(result.Summary)
			for i := range result.Items {
				result.Items[i].Message = redact(result.Items[i].Message)
			}
		}
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(countryCode)
	if region == "" {
		region = strings.TrimSpace(country)
	}
	// SQL 字段名只由固定的两种检测类型决定。
	column := "connection"
	if quality {
		column = "quality"
	}
	_, err = s.DB.Exec(ctx, "UPDATE proxies SET "+column+"_report = $2, "+column+"_checked_at = now(), region = COALESCE(NULLIF($3, ''), region) WHERE id = $1", id, raw, region)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}
