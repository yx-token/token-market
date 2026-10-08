<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import ToastHost from '@/components/ToastHost.vue'

const auth = useAuth()
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
      { to: '/buyer/wallet', text: '钱包' },
      { to: '/buyer/keys', text: 'API Key' },
      { to: '/buyer/usage', text: '用量明细' },
      { to: '/buyer/bills', text: '账单' },
    ],
  },
  seller: {
    title: '卖家中心',
    items: [
      { to: '/seller/upstreams', text: '我的上游' },
      { to: '/seller/listings', text: '挂单' },
      { to: '/seller/earnings', text: '收益与提现' },
    ],
  },
  admin: {
    title: '管理后台',
    items: [
      { to: '/admin', text: '概览' },
      { to: '/admin/upstreams', text: '上游' },
      { to: '/admin/quality', text: '质量与申诉' },
      { to: '/admin/detection', text: '检测策略' },
      { to: '/admin/probe-items', text: '题库与基线账号' },
      { to: '/admin/pools', text: '默认分组' },
      { to: '/admin/vendors', text: '厂商' },
      { to: '/admin/proxies', text: '代理' },
      { to: '/admin/funds', text: '充值、退款、提现' },
      { to: '/admin/billing', text: '计费异常与调账' },
      { to: '/admin/users', text: '用户与实名' },
      { to: '/admin/risk', text: '风控' },
      { to: '/admin/settings', text: '系统设置' },
      { to: '/admin/audit', text: '审计日志' },
    ],
  },
}
const section = computed(() => sections[route.path.split('/')[1]])

function logout() {
  auth.logout()
  router.push('/')
}
</script>

<template>
  <div class="min-h-screen">
    <header class="border-b border-slate-200 bg-white">
      <div class="mx-auto flex h-14 max-w-7xl items-center gap-6 px-4">
        <RouterLink to="/" class="text-lg font-bold text-brand-700">Token 市场</RouterLink>
        <nav class="flex gap-4 text-sm">
          <RouterLink to="/" class="text-slate-600 hover:text-slate-900" exact-active-class="font-semibold text-slate-900">市场</RouterLink>
          <template v-if="auth.loggedIn">
            <RouterLink to="/buyer" class="text-slate-600 hover:text-slate-900" active-class="font-semibold text-slate-900">买家中心</RouterLink>
            <RouterLink to="/seller" class="text-slate-600 hover:text-slate-900" active-class="font-semibold text-slate-900">卖家中心</RouterLink>
            <RouterLink v-if="auth.isAdmin" to="/admin" class="text-slate-600 hover:text-slate-900" active-class="font-semibold text-slate-900">管理后台</RouterLink>
          </template>
        </nav>
        <div class="ml-auto flex items-center gap-3 text-sm">
          <template v-if="auth.loggedIn">
            <RouterLink to="/me" class="text-slate-600 hover:text-slate-900">{{ auth.user?.email }}</RouterLink>
            <button class="btn-secondary btn-sm" @click="logout">退出</button>
          </template>
          <template v-else>
            <RouterLink to="/login" class="btn-ghost">登录</RouterLink>
            <RouterLink to="/register" class="btn-primary">注册</RouterLink>
          </template>
        </div>
      </div>
    </header>
    <div class="mx-auto flex max-w-7xl gap-6 px-4 py-6">
      <aside v-if="section" class="w-44 shrink-0">
        <div class="mb-2 px-2 text-xs font-semibold uppercase tracking-wide text-slate-400">{{ section.title }}</div>
        <nav class="flex flex-col gap-0.5 text-sm">
          <RouterLink
            v-for="item in section.items"
            :key="item.to"
            :to="item.to"
            class="rounded px-2 py-1.5 text-slate-600 hover:bg-slate-100"
            exact-active-class="bg-brand-50 font-medium text-brand-700"
          >
            {{ item.text }}
          </RouterLink>
        </nav>
      </aside>
      <main class="min-w-0 flex-1">
        <RouterView />
      </main>
    </div>
    <ToastHost />
  </div>
</template>
