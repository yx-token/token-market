import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { resourcesForConsolePath } from '@/router/consolePerms'

const adminMeta = { auth: true, admin: true } as const

const consoleChildren: RouteRecordRaw[] = [
  { path: '', name: 'console', component: () => import('@/views/admin/DashboardView.vue') },
  { path: 'settings', name: 'console-settings', component: () => import('@/views/admin/SettingsView.vue') },
  // 运营 · 推荐运营
  { path: 'ops/recommend/factors', name: 'console-rec-factors', component: () => import('@/views/admin/RecommendFactorsView.vue') },
  { path: 'ops/recommend/profiles', name: 'console-rec-profiles', component: () => import('@/views/admin/RecommendProfilesView.vue') },
  { path: 'ops/recommend/tiles', name: 'console-rec-tiles', component: () => import('@/views/admin/RecommendTilesView.vue') },
  { path: 'ops/categories', name: 'console-ops-categories', component: () => import('@/views/admin/CategoriesView.vue') },
  { path: 'ops/hot-searches', name: 'console-ops-hot', component: () => import('@/views/admin/MarketHotSearchView.vue') },
  { path: 'ops/extras', name: 'console-ops-extras', component: () => import('@/views/admin/MarketExtrasView.vue') },
  // 旧路径兼容
  { path: 'ops/discovery', redirect: '/console/ops/extras' },
  { path: 'market-recommend', redirect: '/console/ops/recommend/tiles' },
  { path: 'market-categories', redirect: '/console/ops/categories' },
  { path: 'market-discovery', redirect: '/console/ops/extras' },
  { path: 'system-update', name: 'console-system-update', component: () => import('@/views/admin/SystemUpdateView.vue') },
  { path: 'vendors', name: 'console-vendors', component: () => import('@/views/admin/VendorsView.vue') },
  { path: 'proxies', name: 'console-proxies', component: () => import('@/views/admin/ProxiesView.vue') },
  { path: 'pools', name: 'console-pools', component: () => import('@/views/admin/PoolsView.vue') },
  { path: 'detection', name: 'console-detection', component: () => import('@/views/admin/DetectionView.vue') },
  { path: 'inspection-checks', name: 'console-inspection-checks', component: () => import('@/views/admin/InspectionChecksView.vue') },
  { path: 'third-party', name: 'console-third-party', component: () => import('@/views/admin/ThirdPartyView.vue') },
  { path: 'probe-items', name: 'console-probe-items', component: () => import('@/views/admin/ProbeItemsView.vue') },
  { path: 'probe-records', name: 'console-probe-records', component: () => import('@/views/admin/ProbeRecordsView.vue') },
  { path: 'quality', name: 'console-quality', component: () => import('@/views/admin/QualityView.vue') },
  { path: 'upstreams', name: 'console-upstreams', component: () => import('@/views/admin/UpstreamsView.vue') },
  { path: 'users', name: 'console-users', component: () => import('@/views/admin/UsersView.vue') },
  { path: 'payments', name: 'console-payments', component: () => import('@/views/admin/PaymentsView.vue') },
  { path: 'funds', name: 'console-funds', component: () => import('@/views/admin/FundsView.vue') },
  { path: 'billing', name: 'console-billing', component: () => import('@/views/admin/BillingView.vue') },
  { path: 'risk', name: 'console-risk', component: () => import('@/views/admin/RiskView.vue') },
  { path: 'audit', name: 'console-audit', component: () => import('@/views/admin/AuditView.vue') },
  { path: 'org/units', name: 'console-org-units', component: () => import('@/views/admin/OrgUnitsView.vue') },
  { path: 'org/roles', name: 'console-org-roles', component: () => import('@/views/admin/OrgRolesView.vue') },
  { path: 'org/menus', redirect: '/console/org/roles' },
  { path: 'org/staff', name: 'console-org-staff', component: () => import('@/views/admin/OrgStaffView.vue') },
]

// meta.auth：需要登录；meta.admin：需要管理员
const routes: RouteRecordRaw[] = [
  { path: '/', name: 'market', component: () => import('@/views/market/MarketView.vue') },
  { path: '/market/listings/:id', name: 'market-listing', component: () => import('@/views/market/ListingView.vue'), props: true },
  { path: '/login', name: 'login', component: () => import('@/views/auth/LoginView.vue') },
  { path: '/register', name: 'register', component: () => import('@/views/auth/RegisterView.vue') },
  { path: '/me', name: 'me', component: () => import('@/views/auth/ProfileView.vue'), meta: { auth: true } },

  { path: '/buyer', redirect: '/buyer/keys' },
  { path: '/buyer/wallet', name: 'buyer-wallet', component: () => import('@/views/buyer/WalletView.vue'), meta: { auth: true } },
  { path: '/buyer/keys', name: 'buyer-keys', component: () => import('@/views/buyer/KeysView.vue'), meta: { auth: true } },
  { path: '/buyer/usage', name: 'buyer-usage', component: () => import('@/views/buyer/UsageView.vue'), meta: { auth: true } },
  { path: '/buyer/bills', name: 'buyer-bills', component: () => import('@/views/buyer/BillsView.vue'), meta: { auth: true } },

  { path: '/seller', redirect: '/seller/publish' },
  { path: '/seller/publish', name: 'seller-publish', component: () => import('@/views/seller/PublishView.vue'), meta: { auth: true } },
  { path: '/seller/upstreams', name: 'seller-upstreams', component: () => import('@/views/seller/UpstreamsView.vue'), meta: { auth: true } },
  { path: '/seller/proxies', name: 'seller-proxies', component: () => import('@/views/seller/ProxiesView.vue'), meta: { auth: true } },
  {
    path: '/seller/upstreams/:id',
    name: 'seller-upstream',
    component: () => import('@/views/seller/UpstreamView.vue'),
    props: true,
    meta: { auth: true },
  },
  { path: '/seller/usage', name: 'seller-usage', component: () => import('@/views/seller/UsageView.vue'), meta: { auth: true } },
  { path: '/seller/listings', name: 'seller-listings', component: () => import('@/views/seller/ListingsView.vue'), meta: { auth: true } },
  { path: '/seller/earnings', name: 'seller-earnings', component: () => import('@/views/seller/EarningsView.vue'), meta: { auth: true } },

  {
    path: '/console',
    component: () => import('@/layouts/ConsoleLayout.vue'),
    meta: adminMeta,
    children: consoleChildren,
  },

  // 兼容旧书签：/admin → /console
  {
    path: '/admin/:pathMatch(.*)*',
    redirect: (to) => {
      const m = to.params.pathMatch
      const rest = Array.isArray(m) ? m.filter(Boolean).join('/') : m || ''
      return '/console' + (rest ? `/${rest}` : '')
    },
  },

  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue') },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to, from) => {
  const auth = useAuth()
  if (!auth.loaded) await auth.load()

  // 市场面登录 / 注册：弹窗，不进整页
  if (to.name === 'login' || to.name === 'register') {
    if (auth.loggedIn) {
      return typeof to.query.redirect === 'string' ? to.query.redirect : '/'
    }
    useLoginModal().show({
      redirect: typeof to.query.redirect === 'string' ? to.query.redirect : from.fullPath || '/',
      mode: to.name === 'register' ? 'register' : 'login',
    })
    // 直接打开 /login 时落到首页并弹窗；站内跳转则留在当前页
    if (!from.name) return { name: 'market' }
    return false
  }

  // 需登录的买卖页：弹窗并记下回跳
  if (to.meta.auth && !auth.loggedIn) {
    if (to.path.startsWith('/console')) {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
    useLoginModal().show({ redirect: to.fullPath })
    if (!from.name) return { name: 'market' }
    return false
  }

  if (to.meta.admin && !auth.canConsole) return { name: 'market' }

  // 控制台子页：无对应菜单资源则跳到首个有权限页（避免仅藏侧栏仍可直链）
  if (
    to.path.startsWith('/console') &&
    auth.canConsole &&
    !auth.isAdmin &&
    !auth.consoleAccess?.is_admin
  ) {
    const need = resourcesForConsolePath(to.path)
    if (need && !auth.canAccessConsolePath(to.path, need)) {
      const fallback = auth.consoleAccess?.menu_paths?.[0] || '/console'
      if (fallback !== to.path) return fallback
      return { name: 'market' }
    }
  }
  return true
})
