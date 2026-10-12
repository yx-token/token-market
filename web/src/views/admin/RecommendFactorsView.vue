<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { RecommendConfig, RecommendFactor } from '@/api/types'
import PageHeader from '@/components/PageHeader.vue'
import { useRecommendConfig } from '@/composables/useRecommendConfig'

const { load, save, saving } = useRecommendConfig()
const cfg = ref<RecommendConfig | null>(null)
const factors = ref<RecommendFactor[]>([])

onMounted(async () => {
  cfg.value = await load(true)
  factors.value = (cfg.value.factors ?? []).map((f) => ({ ...f }))
})

async function onSave() {
  if (!cfg.value) return
  const next = { ...cfg.value, factors: factors.value.map((f) => ({ ...f })) }
  // 新启用的因子：给未配置权重的推荐类型补默认权重
  for (const p of next.profiles) {
    if (!p.weights) p.weights = {}
    for (const f of next.factors) {
      if (f.enabled && !f.reserved && p.weights[f.id] === undefined) {
        p.weights[f.id] = f.default_weight
      }
    }
  }
  const res = await save(next, '推荐因子已保存')
  if (res) {
    cfg.value = res
    factors.value = res.factors.map((f) => ({ ...f }))
  }
}

const groupLabel: Record<string, string> = {
  supply: '闲置TOKEN侧',
  demand: '需求侧',
  platform: '平台目标',
}
</script>

<template>
  <div v-if="cfg">
    <PageHeader
      title="推荐因子"
      description="第一层：系统可用的打分维度。启用后可在「推荐类型」里配权重；预留因子暂不参与打分。"
    >
      <RouterLink to="/console/ops/recommend/profiles" class="btn-ghost">下一步：推荐类型 →</RouterLink>
      <button type="button" class="btn-primary" :disabled="saving" @click="onSave">保存</button>
    </PageHeader>

    <div class="card mb-4 border-slate-200 bg-slate-50/80 text-sm leading-relaxed text-slate-600">
      <b class="text-slate-800">怎么用</b>：这里只管理「有哪些因子、默认权重、开不开」。真正决定四格推什么货，在
      <RouterLink class="font-semibold underline" to="/console/ops/recommend/profiles">推荐类型</RouterLink>
      里把因子组合成一套算法，再在
      <RouterLink class="font-semibold underline" to="/console/ops/recommend/tiles">首页四格</RouterLink>
      里每格选一种类型。
    </div>

    <div class="card overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead>
          <tr class="border-b text-slate-500">
            <th class="py-2 pr-2">启用</th>
            <th class="py-2 pr-2">因子</th>
            <th class="py-2 pr-2">分组</th>
            <th class="py-2 pr-2">说明</th>
            <th class="py-2 pr-2">默认权重</th>
            <th class="py-2">备注</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in factors" :key="f.id" class="border-b border-slate-50" :class="{ 'opacity-50': f.reserved }">
            <td class="py-2.5 pr-2">
              <input v-model="f.enabled" type="checkbox" :disabled="f.reserved" />
            </td>
            <td class="py-2.5 pr-2">
              <input v-model="f.name" class="input !py-1" />
              <div class="mt-0.5 font-mono text-[11px] text-slate-400">{{ f.id }}</div>
            </td>
            <td class="py-2.5 pr-2 text-slate-600">{{ groupLabel[f.group] || f.group }}</td>
            <td class="py-2.5 pr-2 min-w-[200px]">
              <input v-model="f.desc" class="input !py-1" />
            </td>
            <td class="py-2.5 pr-2 w-28">
              <input v-model.number="f.default_weight" type="number" step="0.1" min="0" max="10" class="input !py-1" :disabled="f.reserved" />
            </td>
            <td class="py-2.5 text-[12px] text-slate-400">
              {{ f.reserved ? '预留 · 无埋点' : '可进推荐类型' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
