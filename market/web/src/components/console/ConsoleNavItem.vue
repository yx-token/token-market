<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import type { NavNode } from './navTypes'
import ConsoleNavItem from './ConsoleNavItem.vue'

const props = defineProps<{
  node: NavNode
  depth: number
  openKeys: Set<string>
  collapsed?: boolean
}>()

const emit = defineEmits<{ toggle: [key: string] }>()
const route = useRoute()
const flyout = ref(false)

const hasKids = computed(() => !!(props.node.children && props.node.children.length))
const open = computed(() => props.openKeys.has(props.node.key))

function pathActive(to: string) {
  const path = to.split('?')[0]
  return path === '/admin' ? route.path === '/admin' : route.path === path || route.path.startsWith(`${path}/`)
}

function hrefActive(href: string) {
  return typeof window !== 'undefined' && window.location.pathname.startsWith(href)
}

function nodeActive(node: NavNode): boolean {
  if (node.to && pathActive(node.to)) return true
  if (node.href && hrefActive(node.href)) return true
  return node.children?.some(nodeActive) ?? false
}

const branchActive = computed(() => nodeActive(props.node))
const leafOn = computed(
  () =>
    !!(props.node.to && pathActive(props.node.to)) ||
    !!(props.node.href && hrefActive(props.node.href)),
)
const padStyle = computed(() =>
  props.collapsed ? undefined : { paddingLeft: `${10 + props.depth * 12}px` },
)

function onBranchClick() {
  if (props.collapsed) {
    flyout.value = !flyout.value
    return
  }
  emit('toggle', props.node.key)
}
</script>

<template>
  <li
    class="console-nav-node"
    :class="{ 'is-flyout-open': collapsed && flyout }"
    @mouseleave="collapsed && (flyout = false)"
  >
    <button
      v-if="hasKids"
      type="button"
      class="console-nav-branch"
      :class="{ 'is-active': branchActive }"
      :style="padStyle"
      :title="collapsed ? node.text : undefined"
      :aria-expanded="collapsed ? flyout : open"
      @click="onBranchClick"
      @mouseenter="collapsed && (flyout = true)"
    >
      <ConsoleIcon v-if="node.icon" :name="node.icon" size="sm" class="console-link-icon" />
      <span v-if="!collapsed" class="console-nav-text">{{ node.text }}</span>
      <ConsoleIcon
        v-if="!collapsed"
        name="chevron-right"
        size="xs"
        class="console-nav-chevron"
        :class="{ 'is-open': open }"
      />
    </button>
    <a
      v-else-if="node.href"
      :href="node.href"
      class="console-link"
      :class="{ 'is-on': leafOn }"
      :style="padStyle"
      :title="collapsed ? node.text : undefined"
    >
      <ConsoleIcon v-if="node.icon" :name="node.icon" size="sm" class="console-link-icon" />
      <span v-else-if="!collapsed" class="console-nav-dot" aria-hidden="true" />
      <span v-if="!collapsed" class="console-nav-text">{{ node.text }}</span>
    </a>
    <RouterLink
      v-else-if="node.to"
      :to="node.to"
      class="console-link"
      :class="{ 'is-on': leafOn }"
      :style="padStyle"
      :title="collapsed ? node.text : undefined"
    >
      <ConsoleIcon v-if="node.icon" :name="node.icon" size="sm" class="console-link-icon" />
      <span v-else-if="!collapsed" class="console-nav-dot" aria-hidden="true" />
      <span v-if="!collapsed" class="console-nav-text">{{ node.text }}</span>
    </RouterLink>

    <!-- 展开态：树形子级 -->
    <ul v-if="hasKids && open && !collapsed" class="console-nav-list" role="list">
      <ConsoleNavItem
        v-for="child in node.children"
        :key="child.key"
        :node="child"
        :depth="depth + 1"
        :open-keys="openKeys"
        :collapsed="false"
        @toggle="emit('toggle', $event)"
      />
    </ul>

    <!-- 收缩态：浮层子菜单 -->
    <div v-if="hasKids && collapsed && flyout" class="console-nav-flyout" role="menu">
      <div class="console-nav-flyout-title">{{ node.text }}</div>
      <RouterLink
        v-for="child in node.children"
        :key="child.key"
        :to="child.to || '#'"
        class="console-nav-flyout-link"
        :class="{ 'is-on': child.to && pathActive(child.to) }"
        role="menuitem"
        @click="flyout = false"
      >
        <ConsoleIcon v-if="child.icon" :name="child.icon" size="sm" />
        <span>{{ child.text }}</span>
      </RouterLink>
    </div>
  </li>
</template>
