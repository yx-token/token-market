<script setup lang="ts">
import { computed, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import ToastHost from '@/components/ToastHost.vue'
import MarketMasthead from '@/components/MarketMasthead.vue'
import LoginModal from '@/components/LoginModal.vue'

const auth = useAuth()
const loginModal = useLoginModal()
const route = useRoute()
const router = useRouter()

interface NavItem {
  to: string
  text: string
}
const sections: Record<string, { title: string; items: NavItem[] }> = {
  buyer: {
    title: '买家中心',
    items: [
      { to: '/buyer/keys', text: 'API Key' },
      { to: '/buyer/merchants', text: '商家清单' },
      { to: '/buyer/store-credits', text: '店铺活动包' },
      { to: '/buyer/wallet', text: '钱包' },
      { to: '/buyer/usage', text: '用量明细' },
      { to: '/buyer/bills', text: '账单' },
    ],
  },
  seller: {
    title: '卖家中心',
    items: [
      { to: '/seller/publish', text: '发布闲置TOKEN' },
      { to: '/seller/listings', text: '我的闲置TOKEN' },
      { to: '/seller/credits', text: '店铺活动额度' },
      { to: '/seller/upstreams', text: '我的上游' },
      { to: '/seller/proxies', text: '代理 IP' },
      { to: '/seller/usage', text: '用量' },
      { to: '/seller/earnings', text: '收益与提现' },
    ],
  },
}

const isConsole = computed(() => route.path.startsWith('/admin'))
const top = computed(() => route.path.split('/')[1] || 'market')
const section = computed(() => (isConsole.value ? undefined : sections[top.value]))
const isActive = (to: string) => route.path === to || route.path.startsWith(`${to}/`)
/** 市场首页：全宽闲鱼布局，无侧栏 */
const isMarketBrowse = computed(() => route.path === '/' || route.path.startsWith('/market'))

watch(
  isConsole,
  (on) => {
    document.documentElement.classList.toggle('is-console', on)
    document.documentElement.classList.toggle('is-market', !on)
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', on ? '#0555ce' : '#ffffff')
  },
  { immediate: true },
)

// 进站未登录：自动弹出登录窗（对齐闲鱼进站体验）
watch(
  () => auth.loaded,
  (loaded) => {
    if (!loaded || isConsole.value || auth.loggedIn) return
    loginModal.show({ redirect: route.path.startsWith('/login') ? '/' : route.fullPath })
  },
  { immediate: true },
)

function logout() {
  auth.logout()
  router.push('/')
  loginModal.show({ redirect: '/' })
}
</script>

<template>
  <!-- 挂在根上：避免进站 show() 早于市场壳挂载时猫动画 watch 漏触发 -->
  <LoginModal />
  <RouterView v-if="isConsole" />

  <div v-else class="xm-market flex min-h-screen flex-col" :class="{ 'is-browse': isMarketBrowse }">
    <MarketMasthead />

    <!-- 市场浏览：全宽一屏（无页脚），中间列表自滚 -->
    <main v-if="isMarketBrowse" class="min-w-0 flex-1 overflow-hidden">
      <RouterView v-slot="{ Component, route: r }">
        <div :key="r.path" class="page-enter h-full">
          <component :is="Component" />
        </div>
      </RouterView>
    </main>

    <!-- 买卖中心：一体通高工作台（侧栏 + 主区同框铺满） -->
    <div v-else class="xm-center-shell mx-auto w-full max-w-[1480px] flex-1 px-4 py-5 md:px-8 lg:px-10">
      <div class="xm-center-frame">
        <aside v-if="section" class="xm-center-side" aria-label="分区导航">
          <div class="xm-center-side__head">
            <strong>{{ section.title }}</strong>
            <span>{{ auth.user?.email || '已登录' }}</span>
          </div>
          <nav class="xm-center-side__nav">
            <RouterLink
              v-for="item in section.items"
              :key="item.to"
              :to="item.to"
              class="xm-center-side__link"
              :class="{ 'is-on': isActive(item.to) }"
            >
              {{ item.text }}
            </RouterLink>
          </nav>
          <div class="xm-center-side__foot">
            <RouterLink to="/" class="xm-center-side__link muted">回市场首页</RouterLink>
            <button type="button" class="xm-center-side__logout" @click="logout">退出登录</button>
          </div>
        </aside>
        <main class="xm-center-main">
          <nav v-if="section" class="xm-center-mobile-nav lg:hidden" aria-label="分区导航">
            <RouterLink
              v-for="item in section.items"
              :key="item.to"
              :to="item.to"
              :class="['xm-center-mobile-nav__link', { 'is-on': isActive(item.to) }]"
            >
              {{ item.text }}
            </RouterLink>
          </nav>
          <RouterView v-slot="{ Component, route: r }">
            <div :key="r.path" class="xm-center-page page-enter">
              <component :is="Component" />
            </div>
          </RouterView>
        </main>
      </div>
    </div>

    <footer v-if="!isMarketBrowse" class="shrink-0 border-t border-black/5 bg-white">
      <div class="flex w-full flex-wrap items-center gap-x-6 gap-y-2 px-4 py-4 text-[11px] text-[#888] md:px-6 lg:px-7">
        <span class="font-extrabold text-[#303030]">闲猫 · 订阅额度集市</span>
        <span>按官方价 × 倍率计费 · 1 元 = 1 个余额单位</span>
        <span class="ml-auto">质量检测 · 降智即停</span>
      </div>
    </footer>
    <ToastHost />
  </div>
</template>
