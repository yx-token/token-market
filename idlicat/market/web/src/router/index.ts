import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { resourcesForConsolePath } from '@/router/consolePerms'

const adminMeta = { auth: true, admin: true } as const

const routes: RouteRecordRaw[] = [
  { path: '/', name: 'market', component: () => import('@/views/market/MarketView.vue') },
  { path: '/market/listings/:id', name: 'market-listing', component: () => import('@/views/market/ListingView.vue'), props: true },
  { path: '/login', name: 'login', component: () => import('@/views/auth/LoginView.vue') },
  { path: '/register', name: 'register', component: () => import('@/views/auth/RegisterView.vue') },
  { path: '/me', name: 'me', component: () => import('@/views/auth/ProfileView.vue'), meta: { auth: true } },

  { path: '/buyer', redirect: '/buyer/keys' },
  { path: '/buyer/wallet', name: 'buyer-wallet', component: () => import('@/views/buyer/WalletView.vue'), meta: { auth: true } },
  { path: '/buyer/keys', name: 'buyer-keys', component: () => import('@/views/buyer/KeysView.vue'), meta: { auth: true } },
  { path: '/buyer/merchants', name: 'buyer-merchants', component: () => import('@/views/buyer/MerchantsView.vue'), meta: { auth: true } },
  { path: '/buyer/store-credits', name: 'buyer-store-credits', component: () => import('@/views/buyer/StoreCreditsView.vue'), meta: { auth: true } },
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
  { path: '/seller/credits', name: 'seller-credits', component: () => import('@/views/seller/StoreCreditsView.vue'), meta: { auth: true } },
  { path: '/seller/earnings', name: 'seller-earnings', component: () => import('@/views/seller/EarningsView.vue'), meta: { auth: true } },

  // 用闲猫会话换发管理端 token，再整页进入 /admin（sub2api 壳）
  {
    path: '/open-admin',
    name: 'open-admin',
    component: () => import('@/views/admin/OpenEngineView.vue'),
    meta: adminMeta,
  },
  {
    path: '/admin/:pathMatch(.*)*',
    name: 'admin-to-engine',
    component: { render: () => null },
    beforeEnter: (to) => {
      window.location.replace(to.fullPath)
      return false
    },
  },

  // 旧书签 /console → /admin
  {
    path: '/console/:pathMatch(.*)*',
    name: 'legacy-console',
    component: { render: () => null },
    beforeEnter: (to) => {
      const rest = to.path.replace(/^\/console/, '') || ''
      window.location.replace('/admin' + rest)
      return false
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

  if (to.name === 'login' || to.name === 'register') {
    if (auth.loggedIn) {
      return typeof to.query.redirect === 'string' ? to.query.redirect : '/'
    }
    useLoginModal().show({
      redirect: typeof to.query.redirect === 'string' ? to.query.redirect : from.fullPath || '/',
      mode: to.name === 'register' ? 'register' : 'login',
    })
    if (!from.name) return { name: 'market' }
    return false
  }

  if (to.meta.auth && !auth.loggedIn) {
    if (to.path.startsWith('/admin')) {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
    useLoginModal().show({ redirect: to.fullPath })
    if (!from.name) return { name: 'market' }
    return false
  }

  if (to.meta.admin && !auth.canConsole) return { name: 'market' }

  if (
    to.path.startsWith('/admin') &&
    auth.canConsole &&
    !auth.isAdmin &&
    !auth.consoleAccess?.is_admin
  ) {
    const need = resourcesForConsolePath(to.path)
    if (need && !auth.canAccessConsolePath(to.path, need)) {
      const fallback = auth.consoleAccess?.menu_paths?.[0] || '/admin'
      if (fallback !== to.path) return fallback
      return { name: 'market' }
    }
  }
  return true
})
