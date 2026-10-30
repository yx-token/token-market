<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useConsolePage } from '@/stores/consolePage'
import ToastHost from '@/components/ToastHost.vue'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import NetworkStatus from '@/components/common/NetworkStatus.vue'
import ConsoleNav from '@/components/console/ConsoleNav.vue'
import ConsolePageTabs from '@/components/console/ConsolePageTabs.vue'
import type { NavNode } from '@/components/console/navTypes'

const STORAGE_KEY = 'console.sidebar.collapsed'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const page = useConsolePage()
const collapsed = ref(false)
onMounted(async () => {
  try {
    collapsed.value = localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    /* ignore */
  }
  if (!auth.consoleAccess) await auth.loadConsoleAccess()
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
      { key: 'overview', text: '概览', to: '/admin', icon: 'layout-dashboard' },
      { key: 'funds', text: '充值 / 退款 / 提现', to: '/admin/funds', icon: 'wallet' },
      { key: 'quality', text: '质量与申诉', to: '/admin/quality', icon: 'badge-check' },
      { key: 'risk', text: '风控', to: '/admin/risk', icon: 'shield-alert' },
      { key: 'billing', text: '计费异常', to: '/admin/billing', icon: 'circle-alert' },
    ],
  },
  {
    key: 'supply',
    text: '闲置TOKEN',
    icon: 'server',
    children: [
      { key: 'upstreams', text: '上游', to: '/admin/upstreams', icon: 'server' },
      { key: 'pools', text: '默认分组', to: '/admin/pools', icon: 'layers' },
      { key: 'vendors', text: '厂商', to: '/admin/vendors', icon: 'building-2' },
      { key: 'proxies', text: '代理', to: '/admin/proxies', icon: 'globe' },
    ],
  },
  {
    key: 'detect',
    text: '检测',
    icon: 'flask-conical',
    children: [
      { key: 'detection', text: '检测策略', to: '/admin/detection', icon: 'flask-conical' },
      { key: 'inspection-checks', text: '质检项', to: '/admin/inspection-checks', icon: 'badge-check' },
      { key: 'probe-records', text: '检测记录', to: '/admin/probe-records', icon: 'activity' },
      { key: 'third-party', text: '第三方检测', to: '/admin/third-party', icon: 'scan-search' },
      { key: 'probe-items', text: '题库与基线', to: '/admin/probe-items', icon: 'book-open' },
    ],
  },
  {
    key: 'ops',
    text: '运营',
    icon: 'layout-dashboard',
    children: [
      { key: 'market-hot', text: '热搜与筛选', to: '/admin/ops/hot-searches', icon: 'search' },
      { key: 'market-categories', text: '市场分类', to: '/admin/ops/categories', icon: 'filter' },
      { key: 'market-extras', text: '货架开关', to: '/admin/ops/extras', icon: 'settings' },
      { key: 'support', text: '官方客服', to: '/admin/ops/support', icon: 'activity' },
    ],
  },
  {
    key: 'govern',
    text: '治理',
    icon: 'settings',
    children: [
      { key: 'users', text: '用户与实名', to: '/admin/users', icon: 'users' },
      { key: 'payments', text: '支付配置', to: '/admin/payments', icon: 'credit-card' },
      { key: 'settings', text: '系统设置', to: '/admin/settings', icon: 'settings' },
      { key: 'system-update', text: '系统更新', to: '/admin/system-update', icon: 'refresh-cw' },
      { key: 'audit', text: '审计日志', to: '/admin/audit', icon: 'scroll-text' },
    ],
  },
  {
    key: 'org',
    text: '组织管理',
    icon: 'users',
    children: [
      { key: 'org-units', text: '组织架构', to: '/admin/org/units', icon: 'building-2' },
      { key: 'org-roles', text: '角色管理', to: '/admin/org/roles', icon: 'shield' },
      { key: 'org-staff', text: '员工授权', to: '/admin/org/staff', icon: 'users' },
    ],
  },
  {
    key: 'engine',
    text: '引擎 sub2api',
    icon: 'globe',
    children: [
      { key: 'engine-home', text: '引擎概览', to: '/admin/open-engine?to=/admin/dashboard', icon: 'layout-dashboard' },
      { key: 'engine-accounts', text: '引擎账号', to: '/admin/open-engine?to=/admin/accounts', icon: 'server' },
      { key: 'engine-groups', text: '引擎分组', to: '/admin/open-engine?to=/admin/groups', icon: 'layers' },
      { key: 'engine-proxies', text: '引擎代理', to: '/admin/open-engine?to=/admin/proxies', icon: 'globe' },
      { key: 'engine-users', text: '引擎用户', to: '/admin/open-engine?to=/admin/users', icon: 'users' },
      { key: 'engine-usage', text: '用量', to: '/admin/open-engine?to=/admin/usage', icon: 'activity' },
      { key: 'engine-channels', text: '渠道', to: '/admin/open-engine?to=/admin/channels', icon: 'filter' },
      { key: 'engine-settings', text: '引擎设置', to: '/admin/open-engine?to=/admin/settings', icon: 'settings' },
    ],
  },
]

const email = computed(() => auth.user?.email ?? '')
const initial = computed(() => (email.value || '?').slice(0, 1).toUpperCase())

/** 超管看全部；否则只保留 menu_paths 命中的叶子（及仍有子项的目录） */
function filterNav(nodes: NavNode[], paths: Set<string>): NavNode[] {
  const out: NavNode[] = []
  for (const n of nodes) {
    if (n.children?.length) {
      const kids = filterNav(n.children, paths)
      if (kids.length) out.push({ ...n, children: kids })
      continue
    }
    if (n.to && paths.has(n.to)) out.push(n)
  }
  return out
}

const visibleNav = computed(() => {
  const a = auth.consoleAccess
  if (!a || a.is_admin || auth.isAdmin) return navItems
  return filterNav(navItems, new Set(a.menu_paths ?? []))
})

function titleFromNav(nodes: NavNode[], path: string): string {
  for (const n of nodes) {
    if (n.to && (path === n.to || (n.to !== '/admin' && path.startsWith(`${n.to}/`)))) {
      return n.text
    }
    if (n.children?.length) {
      const hit = titleFromNav(n.children, path)
      if (hit) return hit
    }
  }
  return ''
}

const headerTitle = computed(() => page.title.value || titleFromNav(visibleNav.value, route.path) || '控制台')
const headerDesc = computed(() => page.description.value)

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
      <ConsoleNav :items="visibleNav" :collapsed="collapsed" />
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
        <div class="console-top-bar">
          <div class="console-top-title">
            <h1>{{ headerTitle }}</h1>
            <p v-if="headerDesc">{{ headerDesc }}</p>
          </div>
          <div class="console-top-actions">
            <RouterLink to="/" class="console-btn-ghost console-home-btn" title="返回主页">
              <ConsoleIcon name="house" size="sm" />
              <span class="console-home-label">返回主页</span>
            </RouterLink>
            <NetworkStatus />
            <div class="console-user-menu">
              <button class="console-user" type="button" :title="email" aria-haspopup="menu">
                <span class="console-avatar">{{ initial }}</span>
                <span class="console-email">{{ email }}</span>
                <ConsoleIcon name="chevron-down" size="sm" class="console-user-caret" />
              </button>
              <div class="console-user-drop" role="menu">
                <RouterLink role="menuitem" to="/buyer/keys">买家中心</RouterLink>
                <RouterLink role="menuitem" to="/">回市场首页</RouterLink>
                <button role="menuitem" type="button" @click="logout">退出登录</button>
              </div>
            </div>
          </div>
        </div>
        <ConsolePageTabs :nav-items="visibleNav" />
      </header>
      <main class="console-body">
        <RouterView v-slot="{ Component, route: r }">
          <div class="console-page">
            <KeepAlive :max="16">
              <component :is="Component" :key="r.path" />
            </KeepAlive>
          </div>
        </RouterView>
      </main>
    </div>
    <ToastHost />
  </div>
</template>
