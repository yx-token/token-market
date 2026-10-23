<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet } from './api'

const overview = ref<Record<string, unknown> | null>(null)
const error = ref('')

const health = computed(() =>
  Array.isArray(overview.value?.upstream_health) ? (overview.value?.upstream_health as Record<string, unknown>[]) : []
)
const appeals = computed(() =>
  Array.isArray(overview.value?.appeals_by_status) ? (overview.value?.appeals_by_status as Record<string, unknown>[]) : []
)

onMounted(async () => {
  try {
    overview.value = await marketGet<Record<string, unknown>>(`${MARKET_API_PREFIX}/quality/overview`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  }
})
</script>

<template>
  <section class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">质量概览</h2>
        <p class="y-page-desc">上游健康分布与申诉统计</p>
      </div>
    </header>
    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <template v-else-if="overview">
        <h3 class="y-title py-3">上游健康</h3>
        <table>
          <thead>
            <tr>
              <th>平台</th>
              <th>健康</th>
              <th>等级</th>
              <th>数量</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!health.length">
              <td colspan="4">暂无上游</td>
            </tr>
            <tr v-for="(row, i) in health" :key="i">
              <td>{{ row.platform }}</td>
              <td>{{ row.health }}</td>
              <td>{{ row.grade }}</td>
              <td>{{ row.n }}</td>
            </tr>
          </tbody>
        </table>
        <h3 class="y-title py-3">申诉</h3>
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>数量</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!appeals.length">
              <td colspan="2">暂无申诉</td>
            </tr>
            <tr v-for="(row, i) in appeals" :key="i">
              <td>{{ row.status }}</td>
              <td>{{ row.n }}</td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>
  </section>
</template>
