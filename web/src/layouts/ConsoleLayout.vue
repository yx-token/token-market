<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterView, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import ToastHost from '@/components/ToastHost.vue'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import NetworkStatus from '@/components/common/NetworkStatus.vue'
import ConsoleNav from '@/components/console/ConsoleNav.vue'
import type { NavNode } from '@/components/console/navTypes'

const STORAGE_KEY = 'console.sidebar.collapsed'

const auth = useAuth()
const router = useRouter()
const collapsed = ref(false)

onMounted(() => {
  try {
    collapsed.value = localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    /* ignore */
  }
})

watch(collapsed, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    /* ignore */
  }
})

function toggleSide() {
  collapsed.value = !collapsed.value
}

/** 多级菜单树：后续加项只需往 children 挂节点 */
const navItems: NavNode[] = [
  {
    key: 'duty',
    text: '值班',
    icon: 'activity',
    children: [
      { key: 'overview', text: '概览', to: '/console', icon: 'layout-dashboard' },
      { key: 'funds', text: '充值 / 退款 / 提现', to: '/console/funds', icon: 'wallet' },
      { key: 'quality', text: '质量与申诉', to: '/console/quality', icon: 'badge-check' },
      { key: 'risk', text: '风控', to: '/console/risk', icon: 'shield-alert' },
      { key: 'billing', text: '计费异常', to: '/console/billing', icon: 'circle-alert' },
    ],
  },
  {
    key: 'supply',
    text: '供给',
    icon: 'server',
    children: [
      { key: 'upstreams', text: '上游', to: '/console/upstreams', icon: 'server' },
      { key: 'pools', text: '默认分组', to: '/console/pools', icon: 'layers' },
      { key: 'vendors', text: '厂商', to: '/console/vendors', icon: 'building-2' },
      { key: 'proxies', text: '代理', to: '/console/proxies', icon: 'globe' },
    ],
  },
  {
    key: 'detect',
    text: '检测',
    icon: 'flask-conical',
    children: [
      { key: 'detection', text: '检测策略', to: '/console/detection', icon: 'flask-conical' },
      { key: 'third-party', text: '第三方检测', to: '/console/third-party', icon: 'scan-search' },
      { key: 'probe-items', text: '题库与基线', to: '/console/probe-items', icon: 'book-open' },
    ],
  },
  {
    key: 'govern',
    text: '治理',
    icon: 'settings',
    children: [
      { key: 'users', text: '用户与实名', to: '/console/users', icon: 'users' },
      { key: 'payments', text: '支付配置', to: '/console/payments', icon: 'credit-card' },
      { key: 'settings', text: '系统设置', to: '/console/settings', icon: 'settings' },
      { key: 'system-update', text: '系统更新', to: '/console/system-update', icon: 'refresh-cw' },
      { key: 'audit', text: '审计日志', to: '/console/audit', icon: 'scroll-text' },
    ],
  },
]

const email = computed(() => auth.user?.email ?? '')
const initial = computed(() => (email.value || '?').slice(0, 1).toUpperCase())

function logout() {
  auth.logout()
  router.push('/')
}
</script>

<template>
  <div class="console-app" :class="{ 'is-side-collapsed': collapsed }">
    <div class="console-mesh" aria-hidden="true" />

    <aside class="console-side" aria-label="控制台导航">
      <div class="console-brand">
        <div class="console-brand-name">{{ collapsed ? 'C' : 'Console' }}</div>
        <div v-if="!collapsed" class="console-brand-sub">运营控制台</div>
      </div>
      <ConsoleNav :items="navItems" :collapsed="collapsed" />
      <div class="console-side-foot">
        <button
          type="button"
          class="console-collapse-btn"
          :title="collapsed ? '展开菜单' : '收起菜单'"
          :aria-label="collapsed ? '展开菜单' : '收起菜单'"
          :aria-expanded="!collapsed"
          @click="toggleSide"
        >
          <ConsoleIcon :name="collapsed ? 'panel-left-open' : 'panel-left-close'" size="sm" />
          <span v-if="!collapsed">收起菜单</span>
        </button>
      </div>
    </aside>

    <div class="console-main">
      <header class="console-top">
        <div class="console-top-actions">
          <NetworkStatus />
          <span class="console-user" :title="email">
            <span class="console-avatar">{{ initial }}</span>
            <span class="console-email">{{ email }}</span>
          </span>
          <button type="button" class="console-btn-ghost" @click="logout">
            <ConsoleIcon name="log-out" size="sm" />
            <span>退出</span>
          </button>
        </div>
      </header>
      <main class="console-body">
        <RouterView v-slot="{ Component, route: r }">
          <div :key="r.path" class="console-page">
            <component :is="Component" />
          </div>
        </RouterView>
      </main>
    </div>
    <ToastHost />
  </div>
</template>
