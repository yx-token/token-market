/** 控制台路径 → 所需菜单资源码（命中任一即可；超管旁路） */
export const consolePathResources: Record<string, string[]> = {
  '/console': ['console.duty.overview'],
  '/console/funds': ['console.duty.funds'],
  '/console/quality': ['console.duty.quality'],
  '/console/risk': ['console.duty.risk'],
  '/console/billing': ['console.duty.billing'],
  '/console/upstreams': ['console.supply.upstreams'],
  '/console/pools': ['console.supply.pools'],
  '/console/vendors': ['console.supply.vendors'],
  '/console/proxies': ['console.supply.proxies'],
  '/console/detection': ['console.detect.detection'],
  '/console/inspection-checks': ['console.detect.inspection'],
  '/console/probe-records': ['console.detect.probe_records'],
  '/console/third-party': ['console.detect.third_party'],
  '/console/probe-items': ['console.detect.probe_items'],
  '/console/ops/categories': ['console.ops.categories'],
  '/console/ops/hot-searches': ['console.ops.hot'],
  '/console/ops/extras': ['console.ops.extras'],
  '/console/users': ['console.govern.users'],
  '/console/payments': ['console.govern.payments'],
  '/console/settings': ['console.govern.settings'],
  '/console/system-update': ['console.govern.system_update'],
  '/console/audit': ['console.govern.audit'],
  '/console/org/units': ['console.org.units'],
  '/console/org/roles': ['console.org.roles', 'console.org.menus'],
  '/console/org/staff': ['console.org.staff'],
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
