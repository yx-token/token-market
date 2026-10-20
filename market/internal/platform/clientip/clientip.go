// Package clientip 解析客户端真实 IP（Edge 的 IP 白名单、控制面的风控记录都依赖它）。
package clientip

import (
	"net"
	"strings"
)

// Resolver 解析客户端的真实 IP。只有直连方是受信任的代理（负载均衡）时才采信 X-Forwarded-For，
// 并从右往左取第一个不受信任的地址；否则直接用连接地址，防止买家伪造请求头绕过 IP 白名单。
type Resolver struct {
	trusted []*net.IPNet
}

func New(cidrs []string) Resolver {
	var r Resolver
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil && ip.To4() != nil {
				c += "/32"
			} else {
				c += "/128"
			}
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			r.trusted = append(r.trusted, n)
		}
	}
	return r
}

func (r Resolver) isTrusted(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range r.trusted {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func (r Resolver) Resolve(remoteAddr string, xff []string) string {
	remote, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		remote = remoteAddr
	}
	if !r.isTrusted(remote) {
		return remote
	}
	parts := strings.Split(strings.Join(xff, ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !r.isTrusted(ip) {
			return ip
		}
	}
	return remote
}
