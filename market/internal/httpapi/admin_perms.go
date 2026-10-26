package httpapi

import "strings"

// adminRouteResources 将 admin 路由模式映射到菜单资源码（命中任一即可）。
// 组织写操作仍走超管闸，不在此表。
func adminRouteResources(pattern string) []string {
	// 去掉 METHOD 前缀，按路径最长前缀匹配
	path := pattern
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		path = pattern[i+1:]
	}
	rules := []struct {
		prefix string
		ids    []string
	}{
		{"/api/admin/org/units", []string{"console.org.units", "console.org.roles", "console.org.staff"}},
		{"/api/admin/org/resources", []string{"console.org.roles", "console.org.menus"}},
		{"/api/admin/org/roles", []string{"console.org.roles"}},
		{"/api/admin/org/staff", []string{"console.org.staff"}},
		{"/api/admin/org/users", []string{"console.org.staff", "console.org.roles"}},

		{"/api/admin/supply/accounts", []string{"console.supply.upstreams"}},
		{"/api/admin/supply/groups", []string{"console.supply.pools"}},
		{"/api/admin/pools", []string{"console.supply.pools"}},
		{"/api/admin/proxies", []string{"console.supply.proxies"}},
		{"/api/admin/vendors", []string{"console.supply.vendors"}},
		{"/api/admin/upstreams", []string{"console.supply.upstreams"}},
		{"/api/admin/listings", []string{"console.supply.upstreams", "console.ops.categories"}},

		{"/api/admin/detection-policies", []string{"console.detect.detection"}},
		{"/api/admin/detection-providers", []string{"console.detect.third_party"}},
		{"/api/admin/third-party-checks", []string{"console.detect.third_party"}},
		{"/api/admin/inspection", []string{"console.detect.inspection"}},
		{"/api/admin/probe-runs", []string{"console.detect.probe_records"}},
		{"/api/admin/probe-item-stats", []string{"console.detect.probe_records"}},
		{"/api/admin/probe-items", []string{"console.detect.probe_items"}},
		{"/api/admin/reference-accounts", []string{"console.detect.probe_items"}},

		{"/api/admin/quality", []string{"console.duty.quality"}},
		{"/api/admin/appeals", []string{"console.duty.quality"}},
		{"/api/admin/recharges", []string{"console.duty.funds"}},
		{"/api/admin/payments", []string{"console.govern.payments", "console.duty.funds"}},
		{"/api/admin/refund", []string{"console.duty.funds"}},
		{"/api/admin/withdrawals", []string{"console.duty.funds"}},
		{"/api/admin/anomalies", []string{"console.duty.billing"}},
		{"/api/admin/adjustments", []string{"console.duty.billing"}},
		{"/api/admin/ledger", []string{"console.duty.billing"}},
		{"/api/admin/risk-events", []string{"console.duty.risk"}},

		{"/api/admin/market-categories", []string{"console.ops.categories"}},
		{"/api/admin/market-discovery", []string{"console.ops.extras", "console.ops.hot"}},

		{"/api/admin/kyc", []string{"console.govern.users"}},
		{"/api/admin/users", []string{"console.govern.users"}},
		{"/api/admin/settings", []string{"console.govern.settings"}},
		{"/api/admin/system", []string{"console.govern.system_update"}},
		{"/api/admin/audit-logs", []string{"console.govern.audit"}},
		{"/api/admin/sub2api", []string{"console.duty.overview", "console.govern.settings"}},
	}
	var best []string
	bestLen := -1
	for _, rule := range rules {
		if strings.HasPrefix(path, rule.prefix) && len(rule.prefix) > bestLen {
			best = rule.ids
			bestLen = len(rule.prefix)
		}
	}
	return best
}
