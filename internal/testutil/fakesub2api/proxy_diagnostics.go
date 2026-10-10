package fakesub2api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/jackc/pgx/v5"
)

// 开发替身仅返回确定性报告，不连接用户填写的代理或真实 AI 站点。
func (s *Server) testProxy(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var host string
	err = s.DB.QueryRow(r.Context(), "SELECT host FROM proxies WHERE id = $1 AND deleted_at IS NULL", id).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &apiErr{404, "NOT_FOUND", "proxy not found"}
	}
	if err != nil {
		return nil, err
	}
	if strings.Contains(host, "unreachable") {
		return &adminclient.ProxyTestResult{Success: false, Message: "模拟代理连接超时"}, nil
	}
	return &adminclient.ProxyTestResult{Success: true, Message: "Proxy is accessible", LatencyMs: 120,
		IPAddress: "203.0.113.25", Country: "United States", CountryCode: "US", Region: "California", City: "Los Angeles"}, nil
}

func (s *Server) checkProxyQuality(w http.ResponseWriter, r *http.Request) (any, error) {
	raw, err := s.testProxy(w, r)
	if err != nil {
		return nil, err
	}
	base := raw.(*adminclient.ProxyTestResult)
	out := &adminclient.ProxyQualityResult{Score: 100, Grade: "A", Summary: "代理出口及 AI 站点连通正常（开发替身）", CheckedAt: time.Now().Unix(),
		ExitIP: base.IPAddress, Country: base.Country, CountryCode: base.CountryCode, BaseLatencyMs: base.LatencyMs}
	if !base.Success {
		out.Score, out.Grade, out.Summary, out.FailedCount = 0, "F", base.Message, 1
		out.Items = []adminclient.ProxyQualityItem{{Target: "base_connectivity", Status: "fail", Message: base.Message}}
		return out, nil
	}
	for _, target := range []string{"base_connectivity", "openai", "anthropic", "gemini", "grok"} {
		out.Items = append(out.Items, adminclient.ProxyQualityItem{Target: target, Status: "pass", HTTPStatus: 200, LatencyMs: 120, Message: "模拟检测通过"})
		out.PassedCount++
	}
	return out, nil
}
