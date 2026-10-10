<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { RecommendConfig, RecommendProfile } from '@/api/types'
import PageHeader from '@/components/PageHeader.vue'
import { filterOpts, useRecommendConfig } from '@/composables/useRecommendConfig'

const { load, save, saving } = useRecommendConfig()
const cfg = ref<RecommendConfig | null>(null)
const profiles = ref<RecommendProfile[]>([])
const editing = ref<string | null>(null)

const activeFactors = computed(() => (cfg.value?.factors ?? []).filter((f) => f.enabled && !f.reserved))

onMounted(async () => {
  cfg.value = await load(true)
  profiles.value = (cfg.value.profiles ?? []).map(cloneProfile)
})

function cloneProfile(p: RecommendProfile): RecommendProfile {
  return { ...p, weights: { ...(p.weights || {}) } }
}

function filterKey(p: RecommendProfile) {
  return `${p.filter_kind}:${p.filter_val}`
}

function setFilter(p: RecommendProfile, key: string) {
  const [kind, val] = key.split(':')
  p.filter_kind = kind
  p.filter_val = val
}

function nextId(base: string) {
  let id = base.replace(/[^a-z0-9_]/gi, '_').toLowerCase() || 'algo'
  if (!profiles.value.some((p) => p.id === id)) return id
  let n = 2
  while (profiles.value.some((p) => p.id === `${id}_${n}`)) n++
  return `${id}_${n}`
}

function addProfile() {
  const weights: Record<string, number> = {}
  for (const f of activeFactors.value) weights[f.id] = f.default_weight
  const p: RecommendProfile = {
    id: nextId('algo_new'),
    name: '新推荐类型',
    desc: '说明这套算法偏什么',
    enabled: true,
    filter_kind: 'tag',
    filter_val: 'low_rate',
    weights,
    low_rate_max_multiplier: 0.55,
    per_tile_sku: 3,
    fresh_days: 7,
    diversity_per_plat: 2,
    min_sku_to_show_tile: 1,
  }
  profiles.value.push(p)
  editing.value = p.id
}

function removeProfile(idx: number) {
  const p = profiles.value[idx]
  const used = (cfg.value?.tiles ?? []).filter((t) => t.profile_id === p.id)
  if (used.length) {
    alert(`还有 ${used.length} 个首页格子在用「${p.name}」，请先在「首页四格」改掉再删。`)
    return
  }
  if (!confirm(`删除推荐类型「${p.name}」？`)) return
  profiles.value.splice(idx, 1)
  if (editing.value === p.id) editing.value = null
}

async function onSave() {
  if (!cfg.value) return
  for (const p of profiles.value) {
    if (!p.id.trim() || !p.name.trim()) {
      alert('每个推荐类型需要 ID 和名称')
      return
    }
  }
  const ids = profiles.value.map((p) => p.id.trim())
  if (new Set(ids).size !== ids.length) {
    alert('推荐类型 ID 不能重复')
    return
  }
  const res = await save({ ...cfg.value, profiles: profiles.value.map(cloneProfile) }, '推荐类型已保存')
  if (res) {
    cfg.value = res
    profiles.value = res.profiles.map(cloneProfile)
  }
}

function weightBar(w: number) {
  return Math.min(100, Math.round((Number(w) || 0) * 40))
}
</script>

<template>
  <div v-if="cfg">
    <PageHeader
      title="推荐类型"
      description="第二层：把因子组合成一套算法（候选池 + 权重）。首页每一格选一种类型。"
    >
      <RouterLink to="/console/ops/recommend/factors" class="btn-ghost">← 推荐因子</RouterLink>
      <RouterLink to="/console/ops/recommend/tiles" class="btn-ghost">下一步：首页四格 →</RouterLink>
      <button type="button" class="btn-primary" :disabled="saving" @click="onSave">保存</button>
    </PageHeader>

    <div class="mb-4 flex justify-end">
      <button type="button" class="btn-primary" :disabled="saving" @click="addProfile">＋ 新增推荐类型</button>
    </div>

    <div v-if="!profiles.length" class="card py-10 text-center text-sm text-slate-500">
      还没有推荐类型。点「新增」，或到因子页保存触发默认配置。
    </div>

    <div class="space-y-3">
      <div
        v-for="(p, idx) in profiles"
        :key="p.id"
        class="card !p-4"
        :class="{ 'opacity-60': !p.enabled }"
      >
        <div class="flex flex-wrap items-center gap-2">
          <label class="flex items-center gap-1.5 text-sm font-semibold">
            <input v-model="p.enabled" type="checkbox" />
            {{ p.name || '未命名' }}
          </label>
          <span class="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[11px] text-slate-600">{{ p.id }}</span>
          <span class="text-[12px] text-slate-500">
            {{ filterOpts.find((f) => f.kind === p.filter_kind && f.val === p.filter_val)?.label || p.filter_val }}
          </span>
          <div class="ml-auto flex gap-1">
            <button type="button" class="btn-ghost btn-sm" @click="editing = editing === p.id ? null : p.id">
              {{ editing === p.id ? '收起' : '编辑因子权重' }}
            </button>
            <button type="button" class="btn-ghost btn-sm text-red-600" @click="removeProfile(idx)">删除</button>
          </div>
        </div>

        <div class="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div>
            <label class="label">名称</label>
            <input v-model="p.name" class="input" />
          </div>
          <div>
            <label class="label">说明</label>
            <input v-model="p.desc" class="input" />
          </div>
          <div>
            <label class="label">候选池（捞什么货）</label>
            <select class="input" :value="filterKey(p)" @change="setFilter(p, ($event.target as HTMLSelectElement).value)">
              <option v-for="f in filterOpts" :key="`${f.kind}:${f.val}`" :value="`${f.kind}:${f.val}`">{{ f.label }}</option>
            </select>
          </div>
          <div>
            <label class="label">每格推几条</label>
            <input v-model.number="p.per_tile_sku" type="number" min="1" max="12" class="input" />
          </div>
        </div>

        <div v-if="editing === p.id" class="mt-4 border-t border-slate-100 pt-4">
          <p class="label mb-2">因子权重（来自「推荐因子」里已启用的项）</p>
          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="f in activeFactors" :key="f.id" class="rounded-xl border border-slate-200 p-3">
              <div class="flex items-baseline justify-between gap-2">
                <span class="text-sm font-medium">{{ f.name }}</span>
                <input
                  v-model.number="p.weights[f.id]"
                  type="number"
                  step="0.1"
                  min="0"
                  max="10"
                  class="input !w-20 !py-1 text-right"
                />
              </div>
              <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-100">
                <div class="h-full rounded-full bg-slate-800/70" :style="{ width: weightBar(p.weights[f.id]) + '%' }" />
              </div>
              <p class="mt-1 text-[11px] text-slate-500">{{ f.desc }}</p>
            </div>
          </div>
          <div class="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <label class="label">低倍率阈值</label>
              <input v-model.number="p.low_rate_max_multiplier" type="number" step="0.01" min="0.1" max="2" class="input" />
            </div>
            <div>
              <label class="label">新上架天数</label>
              <input v-model.number="p.fresh_days" type="number" min="0" max="90" class="input" />
            </div>
            <div>
              <label class="label">同厂商最多</label>
              <input v-model.number="p.diversity_per_plat" type="number" min="0" max="6" class="input" />
            </div>
            <div>
              <label class="label">最少货量</label>
              <input v-model.number="p.min_sku_to_show_tile" type="number" min="0" max="12" class="input" />
            </div>
          </div>
          <details class="mt-3 text-xs text-slate-500">
            <summary class="cursor-pointer">内部 ID</summary>
            <input v-model="p.id" class="input mt-2 max-w-xs font-mono text-xs" />
          </details>
        </div>
      </div>
    </div>
  </div>
</template>
