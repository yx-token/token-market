/** 控制台路径 → 所需菜单资源码（命中任一即可；超管旁路） */
export const consolePathResources: Record<string, string[]> = {
  '/admin': ['console.duty.overview'],
  '/admin/funds': ['console.duty.funds'],
  '/admin/quality': ['console.duty.quality'],
  '/admin/risk': ['console.duty.risk'],
  '/admin/billing': ['console.duty.billing'],
  '/admin/upstreams': ['console.supply.upstreams'],
  '/admin/pools': ['console.supply.pools'],
  '/admin/vendors': ['console.supply.vendors'],
  '/admin/proxies': ['console.supply.proxies'],
  '/admin/detection': ['console.detect.detection'],
  '/admin/inspection-checks': ['console.detect.inspection'],
  '/admin/probe-records': ['console.detect.probe_records'],
  '/admin/third-party': ['console.detect.third_party'],
  '/admin/probe-items': ['console.detect.probe_items'],
  '/admin/ops/categories': ['console.ops.categories'],
  '/admin/ops/hot-searches': ['console.ops.hot'],
  '/admin/ops/extras': ['console.ops.extras'],
  '/admin/users': ['console.govern.users'],
  '/admin/payments': ['console.govern.payments'],
  '/admin/settings': ['console.govern.settings'],
  '/admin/system-update': ['console.govern.system_update'],
  '/admin/audit': ['console.govern.audit'],
  '/admin/org/units': ['console.org.units'],
  '/admin/org/roles': ['console.org.roles', 'console.org.menus'],
  '/admin/org/staff': ['console.org.staff'],
}

export function resourcesForConsolePath(path: string): string[] | null {
  const exact = consolePathResources[path]
  if (exact) return exact
  // 最长前缀
  let best: string[] | null = null
  let bestLen = -1
  for (const [p, ids] of Object.entries(consolePathResources)) {
    if (path === p || path.startsWith(p + '/')) {
      if (p.length > bestLen) {
        best = ids
        bestLen = p.length
      }
    }
  }
  return best
}
