<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { useMarketDiscovery } from '@/stores/marketDiscovery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import ActionFields from '@/components/console/ActionFields.vue'
import type { MarketCategory } from '@/api/types'
import {
  marketConfigDefaults,
  mergeDiscovery,
  type MarketAction,
  type MarketDiscoveryConfig,
  type Slot,
} from '@/market/config'
import '@/styles/console-liquid.css'

const toast = useToast()
const { busy, run } = useAction()
const tagOpts = ref<{ id: string; name: string }[]>([])
const base = ref<MarketDiscoveryConfig>(structuredClone(marketConfigDefaults))
const form = reactive({
  hot_bar: true,
  pin_pool_first: true,
  banner_enabled: true,
  banner_name: '主横幅',
  banner_title: '按量通道\n选用即开通',
  banner_desc: '起步金可 0 · 用多少扣多少',
  banner_action: {} as MarketAction,
})

onMounted(load)

function flattenPickable(nodes: MarketCategory[], out: { id: string; name: string }[] = []) {
  for (const n of nodes) {
    if (n.seller_pickable && n.enabled !== false) out.push({ id: n.id, name: n.name })
    if (n.children?.length) flattenPickable(n.children, out)
  }
  return out
}

async function load() {
  try {
    const [remote, cats] = await Promise.all([
      admin.marketDiscovery(),
      admin.marketCategories().catch(() => [] as MarketCategory[]),
    ])
    base.value = mergeDiscovery(marketConfigDefaults, remote)
    const b = base.value.slots.find((s) => s.id === 'banner')
    form.hot_bar = base.value.hot_bar?.enabled !== false
    form.pin_pool_first = !!base.value.feed?.pin_pool_first
    form.banner_enabled = b?.enabled !== false
    form.banner_name = b?.name || '主横幅'
    form.banner_title = b?.title || ''
    form.banner_desc = b?.desc || ''
    form.banner_action = { ...(b?.action || {}) }
    tagOpts.value = flattenPickable(cats ?? [])
  } catch (e) {
    toast.error(e)
  }
}

async function save() {
  const banner: Slot = {
    id: 'banner',
    name: form.banner_name.trim() || '主横幅',
    enabled: form.banner_enabled,
    title: form.banner_title,
    desc: form.banner_desc,
    action: { ...form.banner_action },
  }
  const otherSlots = (base.value.slots || []).filter((s) => s.id !== 'banner')
  const body = {
    ...JSON.parse(JSON.stringify(base.value)),
    hot_bar: { enabled: form.hot_bar },
    feed: { pin_pool_first: form.pin_pool_first },
    slots: [banner, ...otherSlots],
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
    title="其他配置"
    description="首页横幅、热搜条开关与货架排序等零散运营开关。类目导航请维护「市场分类」，四宫格请到「首页四格」。"
    plain
  >
    <template #actions>
      <button type="button" class="btn btn-primary" :disabled="busy" @click="save">保存</button>
    </template>

    <div class="space-y-4 max-w-2xl">
      <div class="lg-card extras-card">
        <h2 class="extras-title">显示开关</h2>
        <label class="extras-check"><input v-model="form.hot_bar" type="checkbox" /> 显示顶栏热搜条</label>
        <label class="extras-check"><input v-model="form.pin_pool_first" type="checkbox" /> 信息流优先置顶保障池</label>
      </div>

      <div class="lg-card extras-card">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="extras-title !mb-0">主横幅</h2>
          <label class="extras-check !mb-0"><input v-model="form.banner_enabled" type="checkbox" /> 启用</label>
        </div>
        <label class="extras-field"><span>运营名</span><input v-model="form.banner_name" class="input" /></label>
        <label class="extras-field"><span>标题（可用换行）</span><textarea v-model="form.banner_title" class="input" rows="2" /></label>
        <label class="extras-field"><span>副文案</span><input v-model="form.banner_desc" class="input" /></label>
        <div>
          <p class="extras-label">点击跳转</p>
          <ActionFields v-model="form.banner_action" :tags="tagOpts" />
        </div>
      </div>
    </div>
  </ConsoleListPage>
</template>

<style scoped>
.extras-card {
  display: grid;
  gap: 0.75rem;
  padding: 1rem 1.1rem;
}
.extras-title {
  margin: 0 0 0.25rem;
  font-size: 15px;
  font-weight: 700;
  color: #1e293b;
}
.extras-check {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  margin-bottom: 0.15rem;
  font-size: 14px;
  color: #334155;
}
.extras-field {
  display: grid;
  gap: 0.3rem;
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
}
.extras-label {
  margin: 0 0 0.35rem;
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
}
</style>
