<template>
  <!-- 管理端：闲猫 console 壳 -->
  <div
    v-if="useConsoleShell"
    class="console-app"
    :class="{ 'is-side-collapsed': sidebarCollapsed }"
  >
    <div class="console-mesh" aria-hidden="true" />
    <AppSidebar />
    <div class="console-main-col">
      <AppHeader />
      <MarketPageTabs />
      <main class="console-body-slot">
        <slot />
      </main>
    </div>
  </div>

  <!-- 用户端：保留社区原布局 -->
  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>
    <AppSidebar />
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <AppHeader />
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import '@/styles/xianmao-console.css'
import '@/styles/console-liquid.css'
import '@/styles/xianmao-admin-bridge.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import MarketPageTabs from './MarketPageTabs.vue'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

/** 管理路由用闲猫 console 壳；管理员逛 /shop 也保持同一壳，避免侧栏宽度样式丢失 */
const useConsoleShell = computed(
  () =>
    route.path !== '/admin/login' &&
    (route.path.startsWith('/admin/market') ||
      (isAdmin.value && (route.path.startsWith('/admin') || route.path.startsWith('/shop'))))
)

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>
