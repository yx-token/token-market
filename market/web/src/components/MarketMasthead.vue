<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import type { MarketAction } from '@/market/config'

const auth = useAuth()
const loginModal = useLoginModal()
const route = useRoute()
const router = useRouter()
const q = ref(typeof route.query.q === 'string' ? route.query.q : '')

watch(
  () => route.query.q,
  (v) => {
    q.value = typeof v === 'string' ? v : ''
  },
)

const initial = computed(() => (auth.user?.email ?? '猫').slice(0, 1).toUpperCase())
const displayName = computed(() => {
  const email = auth.user?.email || ''
  const at = email.indexOf('@')
  return at > 0 ? email.slice(0, at) : email || '用户'
})

type NavItem = { id: string; label: string; to: string; auth?: boolean }

const navItems: NavItem[] = [
  { id: 'market', label: '市场', to: '/' },
  { id: 'publish', label: '发布', to: '/seller/publish', auth: true },
  { id: 'orders', label: '我的订单', to: '/buyer/bills', auth: true },
  { id: 'help', label: '帮助中心', to: '/#guide' },
]

function navOn(item: NavItem) {
  if (item.id === 'market') return route.path === '/' || route.path.startsWith('/market')
  if (item.id === 'publish') return route.path.startsWith('/seller/publish')
  if (item.id === 'orders') return route.path.startsWith('/buyer')
  return false
}

function goNav(item: NavItem) {
  if (item.id === 'help') {
    if (route.path === '/') {
      document.getElementById('guide')?.scrollIntoView({ behavior: 'smooth' })
      return
    }
    router.push('/#guide')
    return
  }
  if (item.auth && !auth.loggedIn) {
    loginModal.show({ redirect: item.to })
    return
  }
  router.push(item.to)
}

function search() {
  applyAction({ q: q.value.trim() })
}

function applyAction(action: MarketAction) {
  if (action.href) {
    router.push(action.href)
    return
  }
  const query: Record<string, string> = {}
  if (action.kindTab && action.kindTab !== 'all') query.kind = action.kindTab
  if (action.plat !== undefined && action.plat !== '') query.plat = action.plat
  if (action.tag) query.tag = action.tag
  if (action.q) query.q = action.q
  router.push({ path: '/', query })
}

function logout() {
  auth.logout()
  router.push('/')
}

function goPublish() {
  if (auth.loggedIn) router.push('/seller/publish')
  else loginModal.show({ redirect: '/seller/publish' })
}
</script>

<template>
  <header class="mh">
    <div class="mh-row">
      <RouterLink class="mh-logo" to="/" aria-label="闲猫首页">
        <span class="mh-logo-mark" aria-hidden="true">
          <img src="/assets/xianmao-pet.svg" alt="" width="36" height="32" />
        </span>
        <span class="mh-logo-text">
          <span class="mh-logo-word">闲猫</span>
          <span class="mh-logo-tag">闲置 Token 交易市场</span>
        </span>
      </RouterLink>

      <nav class="mh-nav" aria-label="主导航">
        <button
          v-for="item in navItems"
          :key="item.id"
          type="button"
          class="mh-nav-link"
          :class="{ on: navOn(item) }"
          @click="goNav(item)"
        >{{ item.label }}</button>
      </nav>

      <div class="mh-right">
        <form class="mh-search" @submit.prevent="search">
          <input v-model="q" type="search" placeholder="搜索模型 / 供应商 / Token" aria-label="搜索" />
          <button type="submit" class="mh-search-btn" aria-label="搜索">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <circle cx="7" cy="7" r="4.5" stroke="currentColor" stroke-width="1.6" />
              <path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            </svg>
          </button>
        </form>

        <a v-if="auth.canConsole" class="mh-admin" href="/open-admin?to=/admin/dashboard" title="运营控制台">控制台</a>

        <div v-if="auth.loggedIn" class="user-menu">
          <button class="mh-user" type="button" aria-haspopup="menu">
            <span class="mh-avatar" aria-hidden="true">{{ initial }}</span>
            <span class="mh-uname">{{ displayName }}</span>
            <svg class="user-caret" viewBox="0 0 12 12" width="12" height="12" aria-hidden="true">
              <path d="M3 4.5L6 7.5L9 4.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            </svg>
          </button>
          <div class="user-menu-drop" role="menu">
            <RouterLink role="menuitem" to="/buyer/keys">买家中心</RouterLink>
            <button role="menuitem" type="button" @click="goPublish">发布闲置</button>
            <button role="menuitem" type="button" @click="logout">退出登录</button>
          </div>
        </div>
        <button v-else type="button" class="mh-login" @click="loginModal.show({ redirect: route.fullPath })">
          登录
        </button>
      </div>
    </div>
  </header>
</template>
