<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { useMarketDiscovery } from '@/stores/marketDiscovery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import {
  marketConfigDefaults,
  mergeDiscovery,
  type MarketDiscoveryConfig,
} from '@/market/config'
import '@/styles/console-liquid.css'

const toast = useToast()
const { busy, run } = useAction()
const base = ref<MarketDiscoveryConfig>(structuredClone(marketConfigDefaults))
const form = reactive({
  pin_pool_first: true,
})

onMounted(load)

async function load() {
  try {
    const remote = await admin.marketDiscovery()
    base.value = mergeDiscovery(marketConfigDefaults, remote)
    form.pin_pool_first = !!base.value.feed?.pin_pool_first
  } catch (e) {
    toast.error(e)
  }
}

async function save() {
  const body = {
    hot_searches: base.value.hot_searches,
    hot_bar: { enabled: false },
    feed: { pin_pool_first: form.pin_pool_first },
    version: base.value.version,
  } as Record<string, unknown>
  const res = await run(() => admin.updateMarketDiscovery(body), '配置已保存')
  if (res) {
    base.value = mergeDiscovery(marketConfigDefaults, res as Record<string, unknown>)
    await useMarketDiscovery().reload()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-market-extras-page"
    title="货架开关"
    description="控制市场信息流的默认排序。"
    plain
  >
    <template #actions>
      <button type="button" class="btn btn-primary" :disabled="busy" @click="save">保存</button>
    </template>

    <div class="lg-card extras-card max-w-xl">
      <label class="extras-check"><input v-model="form.pin_pool_first" type="checkbox" /> 信息流默认优先置顶保障池</label>
    </div>
  </ConsoleListPage>
</template>

<style scoped>
.extras-card { padding: 1rem 1.1rem; }
.extras-check {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  margin-bottom: 0.55rem;
  font-size: 13px;
  color: #334155;
}
.extras-check:last-child { margin-bottom: 0; }
</style>
