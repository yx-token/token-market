<script setup lang="ts">
/**
 * 控制台图标（Lucide Static v0.468.0，ISC）
 * SVG 落盘于 src/assets/icons，以 currentColor 上色。
 */
import { computed } from 'vue'
import activity from '@/assets/icons/activity.svg?raw'
import arrowLeft from '@/assets/icons/arrow-left.svg?raw'
import arrowRight from '@/assets/icons/arrow-right.svg?raw'
import badgeCheck from '@/assets/icons/badge-check.svg?raw'
import calendar from '@/assets/icons/calendar.svg?raw'
import chevronDown from '@/assets/icons/chevron-down.svg?raw'
import chevronLeft from '@/assets/icons/chevron-left.svg?raw'
import chevronsLeft from '@/assets/icons/chevrons-left.svg?raw'
import chevronsRight from '@/assets/icons/chevrons-right.svg?raw'
import clock from '@/assets/icons/clock.svg?raw'
import banknote from '@/assets/icons/banknote.svg?raw'
import bookOpen from '@/assets/icons/book-open.svg?raw'
import building2 from '@/assets/icons/building-2.svg?raw'
import chevronRight from '@/assets/icons/chevron-right.svg?raw'
import circleAlert from '@/assets/icons/circle-alert.svg?raw'
import creditCard from '@/assets/icons/credit-card.svg?raw'
import filter from '@/assets/icons/filter.svg?raw'
import flaskConical from '@/assets/icons/flask-conical.svg?raw'
import globe from '@/assets/icons/globe.svg?raw'
import handCoins from '@/assets/icons/hand-coins.svg?raw'
import layers from '@/assets/icons/layers.svg?raw'
import layoutDashboard from '@/assets/icons/layout-dashboard.svg?raw'
import logIn from '@/assets/icons/log-in.svg?raw'
import logOut from '@/assets/icons/log-out.svg?raw'
import panelLeftClose from '@/assets/icons/panel-left-close.svg?raw'
import panelLeftOpen from '@/assets/icons/panel-left-open.svg?raw'
import plus from '@/assets/icons/plus.svg?raw'
import receipt from '@/assets/icons/receipt.svg?raw'
import refreshCw from '@/assets/icons/refresh-cw.svg?raw'
import scale from '@/assets/icons/scale.svg?raw'
import scanSearch from '@/assets/icons/scan-search.svg?raw'
import scrollText from '@/assets/icons/scroll-text.svg?raw'
import search from '@/assets/icons/search.svg?raw'
import server from '@/assets/icons/server.svg?raw'
import settings from '@/assets/icons/settings.svg?raw'
import shield from '@/assets/icons/shield.svg?raw'
import shieldAlert from '@/assets/icons/shield-alert.svg?raw'
import slidersHorizontal from '@/assets/icons/sliders-horizontal.svg?raw'
import trendingUp from '@/assets/icons/trending-up.svg?raw'
import users from '@/assets/icons/users.svg?raw'
import wallet from '@/assets/icons/wallet.svg?raw'
import x from '@/assets/icons/x.svg?raw'

const icons = {
  activity,
  'arrow-left': arrowLeft,
  'arrow-right': arrowRight,
  'badge-check': badgeCheck,
  calendar,
  'chevron-down': chevronDown,
  'chevron-left': chevronLeft,
  'chevrons-left': chevronsLeft,
  'chevrons-right': chevronsRight,
  clock,
  banknote,
  'book-open': bookOpen,
  'building-2': building2,
  'chevron-right': chevronRight,
  'circle-alert': circleAlert,
  'credit-card': creditCard,
  filter,
  'flask-conical': flaskConical,
  globe,
  'hand-coins': handCoins,
  layers,
  'layout-dashboard': layoutDashboard,
  'log-in': logIn,
  'log-out': logOut,
  'panel-left-close': panelLeftClose,
  'panel-left-open': panelLeftOpen,
  plus,
  receipt,
  'refresh-cw': refreshCw,
  scale,
  'scan-search': scanSearch,
  'scroll-text': scrollText,
  search,
  server,
  settings,
  shield,
  'shield-alert': shieldAlert,
  'sliders-horizontal': slidersHorizontal,
  'trending-up': trendingUp,
  users,
  wallet,
  x,
} as const

export type ConsoleIconName = keyof typeof icons

const props = withDefaults(
  defineProps<{
    name: ConsoleIconName
    size?: 'xs' | 'sm' | 'md' | 'lg'
  }>(),
  { size: 'md' },
)

const svg = computed(() => icons[props.name])
const sizeClass = computed(
  () =>
    ({
      xs: 'console-icon--xs',
      sm: 'console-icon--sm',
      md: 'console-icon--md',
      lg: 'console-icon--lg',
    })[props.size],
)
</script>

<template>
  <span
    class="console-icon"
    :class="sizeClass"
    aria-hidden="true"
    v-html="svg"
  />
</template>

<style scoped>
.console-icon {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  line-height: 0;
  color: inherit;
}
.console-icon :deep(svg) {
  width: 100%;
  height: 100%;
  display: block;
}
.console-icon--xs {
  width: 0.875rem;
  height: 0.875rem;
}
.console-icon--sm {
  width: 1rem;
  height: 1rem;
}
.console-icon--md {
  width: 1.25rem;
  height: 1.25rem;
}
.console-icon--lg {
  width: 1.5rem;
  height: 1.5rem;
}
</style>
