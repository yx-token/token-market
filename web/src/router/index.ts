import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuth } from '@/stores/auth'

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
  {
    path: '/seller/upstreams/:id',
    name: 'seller-upstream',
    component: () => import('@/views/seller/UpstreamView.vue'),
    props: true,
    meta: { auth: true },
  },
  { path: '/seller/listings', name: 'seller-listings', component: () => import('@/views/seller/ListingsView.vue'), meta: { auth: true } },
  { path: '/seller/earnings', name: 'seller-earnings', component: () => import('@/views/seller/EarningsView.vue'), meta: { auth: true } },

  { path: '/admin', name: 'admin', component: () => import('@/views/admin/DashboardView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/settings', component: () => import('@/views/admin/SettingsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/vendors', component: () => import('@/views/admin/VendorsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/proxies', component: () => import('@/views/admin/ProxiesView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/pools', component: () => import('@/views/admin/PoolsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/detection', component: () => import('@/views/admin/DetectionView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/probe-items', component: () => import('@/views/admin/ProbeItemsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/quality', component: () => import('@/views/admin/QualityView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/upstreams', component: () => import('@/views/admin/UpstreamsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/users', component: () => import('@/views/admin/UsersView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/funds', component: () => import('@/views/admin/FundsView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/billing', component: () => import('@/views/admin/BillingView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/risk', component: () => import('@/views/admin/RiskView.vue'), meta: { auth: true, admin: true } },
  { path: '/admin/audit', component: () => import('@/views/admin/AuditView.vue'), meta: { auth: true, admin: true } },

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
