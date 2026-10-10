<script setup lang="ts">
/**
 * 控制台多级侧栏：任意深度 children；收缩时仅图标 + 浮层子菜单。
 */
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import type { NavNode } from './navTypes'
import ConsoleNavItem from './ConsoleNavItem.vue'

const props = defineProps<{ items: NavNode[]; collapsed?: boolean }>()
const route = useRoute()

function pathActive(to: string) {
  return to === '/console' ? route.path === '/console' : route.path === to || route.path.startsWith(`${to}/`)
}

function nodeActive(node: NavNode): boolean {
  if (node.to && pathActive(node.to)) return true
  return node.children?.some(nodeActive) ?? false
}

function collectOpenKeys(nodes: NavNode[], acc: Set<string> = new Set()): Set<string> {
  for (const n of nodes) {
    if (n.children?.length && n.children.some(nodeActive)) {
      acc.add(n.key)
      collectOpenKeys(n.children, acc)
    }
  }
  return acc
}

function initialOpen(): Set<string> {
  const keys = new Set(props.items.filter((n) => n.children?.length).map((n) => n.key))
  for (const k of collectOpenKeys(props.items)) keys.add(k)
  return keys
}

const openKeys = ref<Set<string>>(initialOpen())

watch(
  () => route.path,
  () => {
    const auto = collectOpenKeys(props.items)
    openKeys.value = new Set([...openKeys.value, ...auto])
  },
)

function toggle(key: string) {
  const next = new Set(openKeys.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  openKeys.value = next
}
</script>

<template>
  <nav class="console-nav" :class="{ 'is-collapsed': collapsed }" aria-label="控制台导航">
    <ul class="console-nav-list" role="list">
      <ConsoleNavItem
        v-for="node in items"
        :key="node.key"
        :node="node"
        :depth="0"
        :open-keys="openKeys"
        :collapsed="!!collapsed"
        @toggle="toggle"
      />
    </ul>
  </nav>
</template>
