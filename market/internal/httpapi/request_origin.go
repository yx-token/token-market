package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/yx-token/token-market/market/internal/platform/clientip"
)

// requestOrigin uses the same connection trust boundary as client IP resolution.
func requestOrigin(r *http.Request, trustedProxies []string) string {
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if clientip.New(trustedProxies).Trusts(r.RemoteAddr) {
		if v := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); v != "" {
			scheme = v
		}
		if v := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); v != "" {
			host = v
		}
	}
	if origin, ok := matchingOrigin(r.Header.Get("Origin"), host); ok {
		scheme, host = origin.Scheme, origin.Host
	}
	if (scheme != "http" && scheme != "https") || host == "" {
		return ""
	}
	u, err := url.Parse(scheme + "://" + host)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func matchingOrigin(raw, host string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") || host == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Host != host {
		return nil, false
	}
	return u, true
}
