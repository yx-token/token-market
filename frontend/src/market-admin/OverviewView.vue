<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet } from './api'

const status = ref<Record<string, unknown> | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    status.value = await marketGet(`${MARKET_API_PREFIX}/status`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  }
})
</script>

<template>
  <section class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">市场状态</h2>
        <p class="y-page-desc">二手 TOKEN 市场与引擎执行层的联通状态（非独立外挂模块）</p>
      </div>
    </header>
    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <dl v-else-if="status" class="grid grid-cols-2 gap-3 py-4 text-sm">
        <div class="card p-4">
          <dt class="text-xs text-gray-500">引擎版本</dt>
          <dd class="mt-1 font-medium">{{ status.sub2api_version || '—' }}</dd>
        </div>
        <div class="card p-4">
          <dt class="text-xs text-gray-500">运行模式</dt>
          <dd class="mt-1 font-medium">{{ status.mode || '—' }}</dd>
        </div>
        <div class="card p-4">
          <dt class="text-xs text-gray-500">写保护</dt>
          <dd class="mt-1 font-medium">{{ status.guard_state || '—' }}</dd>
        </div>
        <div class="card p-4">
          <dt class="text-xs text-gray-500">宿主</dt>
          <dd class="mt-1 font-medium">{{ status.host || '—' }}</dd>
        </div>
        <div class="card col-span-2 p-4">
          <dt class="text-xs text-gray-500">兼容指纹</dt>
          <dd class="mt-1 break-all font-mono text-xs">{{ status.migration_fingerprint || '—' }}</dd>
        </div>
      </dl>
    </div>
  </section>
</template>
