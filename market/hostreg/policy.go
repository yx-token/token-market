package hostreg

import (
	"regexp"
	"strings"

	"golang.org/x/net/publicsuffix"
)

var domainPattern = regexp.MustCompile(
	`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
)

// Decision 对某邮箱应用策略后的结论。
type Decision int

const (
	// Allow 允许注册（白名单为空，或域名在白名单内）。
	Allow Decision = iota
	// Deny 拒绝（白名单非空、域名不在白名单，且未开启限量放行）。
	Deny
	// Quota 非白名单域名可走限量：调用方需统计该根域名下账号数，已有则拒绝。
	Quota
)

// Decide 按策略判定邮箱是否可继续发码 / 注册。
func Decide(email string, p Policy) Decision {
	if len(p.Whitelist) == 0 {
		return Allow
	}
	if Allowed(email, p.Whitelist) {
		return Allow
	}
	if p.DomainQuotaEnabled {
		return Quota
	}
	return Deny
}

// Allowed 空白名单视为全放行。
func Allowed(email string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	_, domain, ok := splitEmail(email)
	if !ok {
		return false
	}
	suffix := "@" + domain
	for _, allowed := range whitelist {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if strings.HasPrefix(allowed, "@") && suffix == allowed {
			return true
		}
		if strings.HasPrefix(allowed, "*.") && domainMatchesWildcard(domain, allowed) {
			return true
		}
	}
	return false
}

// RegistrableDomain 返回可注册主域名（abc.com / abcd.abc.com → abc.com）。
func RegistrableDomain(email string) string {
	_, domain, ok := splitEmail(email)
	if !ok {
		return ""
	}
	domain = strings.TrimRight(domain, ".")
	if domain == "" {
		return ""
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return domain
	}
	return registrable
}

func domainMatchesWildcard(domain, allowed string) bool {
	base := strings.TrimPrefix(allowed, "*.")
	if !isValidDomain(base) {
		return false
	}
	return domain == base || strings.HasSuffix(domain, "."+base)
}

func isValidDomain(domain string) bool {
	return domain != "" && !strings.Contains(domain, "@") && domainPattern.MatchString(domain)
}

func splitEmail(raw string) (local, domain string, ok bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", "", false
	}
	domain = strings.TrimRight(domain, ".")
	if domain == "" {
		return "", "", false
	}
	return local, domain, true
}
