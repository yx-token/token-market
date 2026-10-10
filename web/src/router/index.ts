import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuth } from '@/stores/auth'

const adminMeta = { auth: true, admin: true } as const

const consoleChildren: RouteRecordRaw[] = [
  { path: '', name: 'console', component: () => import('@/views/admin/DashboardView.vue') },
  { path: 'settings', name: 'console-settings', component: () => import('@/views/admin/SettingsView.vue') },
  { path: 'system-update', name: 'console-system-update', component: () => import('@/views/admin/SystemUpdateView.vue') },
  { path: 'vendors', name: 'console-vendors', component: () => import('@/views/admin/VendorsView.vue') },
  { path: 'proxies', name: 'console-proxies', component: () => import('@/views/admin/ProxiesView.vue') },
  { path: 'pools', name: 'console-pools', component: () => import('@/views/admin/PoolsView.vue') },
  { path: 'detection', name: 'console-detection', component: () => import('@/views/admin/DetectionView.vue') },
  { path: 'third-party', name: 'console-third-party', component: () => import('@/views/admin/ThirdPartyView.vue') },
  { path: 'probe-items', name: 'console-probe-items', component: () => import('@/views/admin/ProbeItemsView.vue') },
  { path: 'quality', name: 'console-quality', component: () => import('@/views/admin/QualityView.vue') },
  { path: 'upstreams', name: 'console-upstreams', component: () => import('@/views/admin/UpstreamsView.vue') },
  { path: 'users', name: 'console-users', component: () => import('@/views/admin/UsersView.vue') },
  { path: 'payments', name: 'console-payments', component: () => import('@/views/admin/PaymentsView.vue') },
  { path: 'funds', name: 'console-funds', component: () => import('@/views/admin/FundsView.vue') },
  { path: 'billing', name: 'console-billing', component: () => import('@/views/admin/BillingView.vue') },
  { path: 'risk', name: 'console-risk', component: () => import('@/views/admin/RiskView.vue') },
  { path: 'audit', name: 'console-audit', component: () => import('@/views/admin/AuditView.vue') },
]

// meta.auth：需要登录；meta.admin：需要管理员
const routes: RouteRecordRaw[] = [
  { path: '/', name: 'market', component: () => import('@/views/market/MarketView.vue') },
  { path: '/market/listings/:id', name: 'market-listing', component: () => import('@/views/market/ListingView.vue'), props: true },
  { path: '/login', name: 'login', component: () => import('@/views/auth/LoginView.vue') },
  { path: '/register', name: 'register', component: () => import('@/views/auth/RegisterView.vue') },
  { path: '/me', name: 'me', component: () => import('@/views/auth/ProfileView.vue'), meta: { auth: true } },

  { path: '/buyer', redirect: '/buyer/wallet' },
  { path: '/buyer/wallet', name: 'buyer-wallet', component: () => import('@/views/buyer/WalletView.vue'), meta: { auth: true } },
  { path: '/buyer/keys', name: 'buyer-keys', component: () => import('@/views/buyer/KeysView.vue'), meta: { auth: true } },
  { path: '/buyer/usage', name: 'buyer-usage', component: () => import('@/views/buyer/UsageView.vue'), meta: { auth: true } },
  { path: '/buyer/bills', name: 'buyer-bills', component: () => import('@/views/buyer/BillsView.vue'), meta: { auth: true } },

  { path: '/seller', redirect: '/seller/upstreams' },
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

router.beforeEach(async (to) => {
  const auth = useAuth()
  if (!auth.loaded) await auth.load()
  if (to.meta.auth && !auth.loggedIn) return { name: 'login', query: { redirect: to.fullPath } }
  if (to.meta.admin && !auth.isAdmin) return { name: 'market' }
  return true
})
