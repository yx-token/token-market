<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import ToastHost from '@/components/ToastHost.vue'
import TickerTape from '@/components/TickerTape.vue'
import SealMark from '@/components/SealMark.vue'

const auth = useAuth()
const route = useRoute()
const router = useRouter()

interface NavItem {
  to: string
  text: string
}
const sections: Record<string, { title: string; en: string; items: NavItem[] }> = {
  buyer: {
    title: '买家中心',
    en: 'BUYER',
    items: [
      { to: '/buyer/wallet', text: '钱包' },
      { to: '/buyer/keys', text: 'API Key' },
      { to: '/buyer/usage', text: '用量明细' },
      { to: '/buyer/bills', text: '账单' },
    ],
  },
  seller: {
    title: '卖家中心',
    en: 'SELLER',
    items: [
      { to: '/seller/upstreams', text: '我的上游' },
      { to: '/seller/proxies', text: '代理 IP' },
      { to: '/seller/usage', text: '用量' },
      { to: '/seller/listings', text: '挂单' },
      { to: '/seller/earnings', text: '收益与提现' },
    ],
  },
  admin: {
    title: '管理后台',
    en: 'ADMIN',
    items: [
      { to: '/admin', text: '概览' },
      { to: '/admin/upstreams', text: '上游' },
      { to: '/admin/quality', text: '质量与申诉' },
      { to: '/admin/detection', text: '检测策略' },
      { to: '/admin/third-party', text: '第三方检测' },
      { to: '/admin/probe-items', text: '题库与基线账号' },
      { to: '/admin/pools', text: '默认分组' },
      { to: '/admin/vendors', text: '厂商' },
      { to: '/admin/proxies', text: '代理' },
      { to: '/admin/funds', text: '充值、退款、提现' },
      { to: '/admin/payments', text: '支付配置' },
      { to: '/admin/billing', text: '计费异常与调账' },
      { to: '/admin/users', text: '用户与实名' },
      { to: '/admin/risk', text: '风控' },
      { to: '/admin/settings', text: '系统设置' },
      { to: '/admin/system-update', text: '系统更新' },
      { to: '/admin/audit', text: '审计日志' },
    ],
  },
}
const top = computed(() => route.path.split('/')[1] || 'market')
const section = computed(() => sections[top.value])
const mainNav = computed(() => {
  const items = [{ key: 'market', to: '/', text: '市场' }]
  if (auth.loggedIn) {
    items.push({ key: 'buyer', to: '/buyer', text: '买家中心' }, { key: 'seller', to: '/seller', text: '卖家中心' })
    if (auth.isAdmin) items.push({ key: 'admin', to: '/admin', text: '管理后台' })
  }
  return items
})
const isActive = (to: string) => route.path === to || (to !== '/admin' && route.path.startsWith(`${to}/`))
const pad = (n: number) => String(n + 1).padStart(2, '0')
const initial = computed(() => (auth.user?.email ?? '?').slice(0, 1).toUpperCase())

function logout() {
  auth.logout()
  router.push('/')
}
</script>

<template>
  <div class="flex min-h-screen flex-col">
    <TickerTape />
    <header class="masthead sticky top-0 z-30 bg-paper/95 backdrop-blur-sm">
      <div class="mx-auto flex max-w-[1320px] flex-wrap items-end gap-x-4 gap-y-3 px-5 pb-3 pt-4 md:px-8 lg:gap-x-10">
        <RouterLink to="/" class="group flex items-center gap-3" aria-label="闲置算力交易所 首页">
          <SealMark :size="38" class="transition-transform duration-300 group-hover:rotate-0" />
          <span>
            <span class="block font-display text-[21px] font-bold leading-none tracking-[0.08em]">闲置算力交易所</span>
            <span class="kicker mt-1.5 block text-[9.5px] tracking-[0.28em]">Token Exchange<span class="hidden sm:inline"> · 订阅额度撮合</span></span>
          </span>
        </RouterLink>
        <nav class="order-3 flex w-full items-end gap-6 overflow-x-auto lg:order-2 lg:w-auto lg:overflow-visible" aria-label="主导航">
          <RouterLink
            v-for="(n, i) in mainNav"
            :key="n.key"
            :to="n.to"
            :class="[
              'flex shrink-0 items-baseline gap-1.5 border-b-2 pb-1 transition-colors',
              top === n.key ? 'border-brand-500 text-ink-900' : 'border-transparent text-ink-600 hover:border-ink-300 hover:text-ink-900',
            ]"
          >
            <span class="font-mono text-[10px] text-ink-400">{{ pad(i) }}</span>
            <span class="text-[14px] font-medium">{{ n.text }}</span>
          </RouterLink>
        </nav>
        <div class="order-2 ml-auto flex items-center gap-3 pb-0.5 text-sm lg:order-3">
          <template v-if="auth.loggedIn">
            <RouterLink to="/me" class="group flex items-center gap-2 text-ink-700 hover:text-ink-900" title="账号">
              <span class="grid h-7 w-7 place-items-center border border-ink-900 bg-white font-mono text-xs font-medium">{{ initial }}</span>
              <span class="hidden max-w-[180px] truncate font-mono text-xs lg:inline">{{ auth.user?.email }}</span>
            </RouterLink>
            <button class="btn-ghost btn-sm" @click="logout">退出</button>
          </template>
          <template v-else>
            <RouterLink to="/login" class="btn-ghost">登录</RouterLink>
            <RouterLink to="/register" class="btn-primary">注册账号</RouterLink>
          </template>
        </div>
      </div>
      <!-- 小屏：分区导航横向滚动 -->
      <nav v-if="section" class="flex gap-4 overflow-x-auto border-t border-ink-200 px-5 py-2 text-sm md:hidden" aria-label="分区导航">
        <RouterLink
          v-for="item in section.items"
          :key="item.to"
          :to="item.to"
          :class="['shrink-0 whitespace-nowrap', isActive(item.to) ? 'font-medium text-ink-900 underline decoration-brand-500 decoration-2 underline-offset-4' : 'text-ink-500']"
        >
          {{ item.text }}
        </RouterLink>
      </nav>
    </header>

    <div class="mx-auto flex w-full max-w-[1320px] flex-1 gap-10 px-5 py-8 md:px-8">
      <aside v-if="section" class="hidden w-52 shrink-0 md:block">
        <div class="scroll-thin sticky top-32 max-h-[calc(100vh-9rem)] overflow-y-auto pb-6 pr-1">
          <div class="mb-4 flex items-baseline justify-between border-b border-ink-900 pb-2">
            <span class="font-display text-[15px] font-bold tracking-wide">{{ section.title }}</span>
            <span class="kicker text-[9.5px]">{{ section.en }}</span>
          </div>
          <nav class="flex flex-col" aria-label="分区导航">
            <RouterLink
              v-for="(item, i) in section.items"
              :key="item.to"
              :to="item.to"
              :class="[
                'group relative flex items-baseline gap-3 border-b border-dashed border-ink-200 py-[7px] pl-3 text-[13.5px] transition-colors',
                isActive(item.to) ? 'font-medium text-ink-900' : 'text-ink-600 hover:text-ink-900',
              ]"
            >
              <span
                :class="['absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 transition-all', isActive(item.to) ? 'bg-brand-500' : 'bg-transparent group-hover:bg-ink-300']"
              />
              <span :class="['font-mono text-[10px]', isActive(item.to) ? 'text-brand-600' : 'text-ink-400']">{{ pad(i) }}</span>
              <span>{{ item.text }}</span>
            </RouterLink>
          </nav>
        </div>
      </aside>
      <main class="min-w-0 flex-1">
        <RouterView v-slot="{ Component, route: r }">
          <div :key="r.path" class="page-enter">
            <component :is="Component" />
          </div>
        </RouterView>
      </main>
    </div>

    <footer class="mt-auto border-t border-ink-900">
      <div class="mx-auto flex max-w-[1320px] flex-wrap items-center gap-x-8 gap-y-2 px-5 py-5 font-mono text-[11px] text-ink-500 md:px-8">
        <span class="flex items-center gap-2 text-ink-700"><SealMark :size="16" /> 闲置算力交易所</span>
        <span>按官方价 × 倍率计费 · 1 元 = 1 个余额单位</span>
        <span>失败的请求不计费</span>
        <span class="ml-auto">质量检测 · 降智即停调度</span>
      </div>
    </footer>
    <ToastHost />
  </div>
</template>
