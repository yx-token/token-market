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

</script>

<template>
  <header class="header">
    <div class="header-row">
      <RouterLink class="logo" to="/" aria-label="闲猫首页">
        <span class="logo-mark" aria-hidden="true">
          <img src="/assets/xianmao-pet.svg" alt="" width="44" height="38" />
        </span>
        <span class="logo-word">闲猫</span>
      </RouterLink>
      <form class="search" @submit.prevent="search">
        <span class="search-ico" aria-hidden="true">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <circle cx="7" cy="7" r="4.5" stroke="currentColor" stroke-width="1.5" />
            <path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
        </span>
        <input v-model="q" type="search" placeholder="搜 Claude、ChatGPT、Kimi、挂单号…" aria-label="搜索" />
        <button type="submit">搜索</button>
      </form>
      <div class="header-actions">
        <a
          class="header-link"
          href="/seller/publish"
          title="发布闲置TOKEN"
          @click.prevent="auth.loggedIn ? router.push('/seller/publish') : loginModal.show({ redirect: '/seller/publish' })"
        >
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M8 8.5H7.2A2.2 2.2 0 0 0 5 10.7v6.1A2.2 2.2 0 0 0 7.2 19h9.6a2.2 2.2 0 0 0 2.2-2.2v-6.1a2.2 2.2 0 0 0-2.2-2.2H16" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            <path d="M9 8.5l1.1-2.2h3.8L15 8.5" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
            <circle cx="12" cy="13.2" r="2.4" stroke="currentColor" stroke-width="1.6" />
          </svg>
          <span>我要卖闲置<span class="sell-extra"> TOKEN</span></span>
        </a>
        <RouterLink v-if="auth.canConsole" class="header-link admin" to="/console" title="运营控制台">控制台</RouterLink>
        <div v-if="auth.loggedIn" class="user-menu">
          <button class="user-chip" type="button" :aria-expanded="false" aria-haspopup="menu">
            <span class="avatar" aria-hidden="true">{{ initial }}</span>
            <span class="uname">{{ auth.user?.email }}</span>
            <svg class="user-caret" viewBox="0 0 12 12" width="12" height="12" aria-hidden="true">
              <path d="M3 4.5L6 7.5L9 4.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            </svg>
          </button>
          <div class="user-menu-drop" role="menu">
            <RouterLink role="menuitem" to="/buyer/keys">买家中心</RouterLink>
            <button role="menuitem" type="button" @click="logout">退出登录</button>
          </div>
        </div>
        <template v-else>
          <button type="button" class="header-link" @click="loginModal.show({ redirect: route.fullPath })">
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <circle cx="12" cy="8" r="3.25" stroke="currentColor" stroke-width="1.6" />
              <path d="M5.5 19.25c.9-3.2 3.1-4.75 6.5-4.75s5.6 1.55 6.5 4.75" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            </svg>
            <span>登录</span>
          </button>
        </template>
      </div>
    </div>
  </header>
</template>
