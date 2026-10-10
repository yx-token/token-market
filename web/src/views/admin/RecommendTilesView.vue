<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { market } from '@/api'
import type { HomeTile, RecommendConfig, RecommendResult } from '@/api/types'
import PageHeader from '@/components/PageHeader.vue'
import { themeOpts, useRecommendConfig } from '@/composables/useRecommendConfig'
import { tileThemeIcon } from '@/market/config'

const { load, save, saving } = useRecommendConfig()
const cfg = ref<RecommendConfig | null>(null)
const tiles = ref<HomeTile[]>([])
const maxTiles = ref(4)
const hideEmpty = ref(true)
const preview = ref<RecommendResult | null>(null)

const profiles = computed(() => (cfg.value?.profiles ?? []).filter((p) => p.enabled))

onMounted(async () => {
  await reload()
})

async function reload() {
  cfg.value = await load(true)
  tiles.value = (cfg.value.tiles ?? []).map((t) => ({ ...t }))
  maxTiles.value = cfg.value.max_tiles
  hideEmpty.value = cfg.value.hide_empty_tiles
  await refreshPreview()
}

async function refreshPreview() {
  try {
    preview.value = await market.recommend()
  } catch {
    preview.value = null
  }
}

function profileName(id: string) {
  return cfg.value?.profiles.find((p) => p.id === id)?.name || id
}

function nextId(base: string) {
  let id = base.replace(/[^a-z0-9_]/gi, '_').toLowerCase() || 'tile'
  if (!tiles.value.some((t) => t.id === id)) return id
  let n = 2
  while (tiles.value.some((t) => t.id === `${id}_${n}`)) n++
  return `${id}_${n}`
}

function addTile() {
  const pid = profiles.value[0]?.id || cfg.value?.profiles[0]?.id || ''
  if (!pid) {
    alert('请先在「推荐类型」里至少建一种算法')
    return
  }
  tiles.value.push({
    id: nextId('tile'),
    title: '新专场',
    desc: '点进货架看看',
    eng: 'NEW',
    theme: 'deal',
    enabled: true,
    sort: (tiles.value.length + 1) * 10,
    boost: 1,
    profile_id: pid,
  })
}

function removeTile(idx: number) {
  if (!confirm(`删除格子「${tiles.value[idx].title}」？`)) return
  tiles.value.splice(idx, 1)
}

function moveTile(idx: number, dir: -1 | 1) {
  const j = idx + dir
  if (j < 0 || j >= tiles.value.length) return
  const tmp = tiles.value[idx]
  tiles.value[idx] = tiles.value[j]
  tiles.value[j] = tmp
}

async function onSave() {
  if (!cfg.value) return
  for (const t of tiles.value) {
    if (!t.id.trim() || !t.title.trim()) {
      alert('每个格子需要 ID 和标题')
      return
    }
    if (!t.profile_id) {
      alert(`「${t.title}」还没选推荐类型`)
      return
    }
  }
  const sorted = tiles.value.map((t, i) => ({ ...t, sort: (i + 1) * 10 }))
  const res = await save(
    {
      ...cfg.value,
      tiles: sorted,
      max_tiles: Number(maxTiles.value),
      hide_empty_tiles: hideEmpty.value,
    },
    '首页四格已保存',
  )
  if (res) {
    cfg.value = res
    tiles.value = res.tiles.map((t) => ({ ...t }))
    await refreshPreview()
  }
}

async function resetDefaults() {
  if (!confirm('恢复默认：因子 + 四种推荐类型 + 四个首页格子？')) return
  const res = await save(
    {
      version: 2,
      factors: [],
      profiles: [],
      tiles: [],
      max_tiles: 4,
      hide_empty_tiles: true,
    },
    '已恢复默认推荐配置',
  )
  if (res) {
    cfg.value = res
    tiles.value = res.tiles.map((t) => ({ ...t }))
    maxTiles.value = res.max_tiles
    hideEmpty.value = res.hide_empty_tiles
    await refreshPreview()
  }
}

function tileStatus(t: HomeTile) {
  if (!t.enabled) return { tone: 'off' as const, text: '已关闭' }
  const hit = preview.value?.tiles?.find((x) => x.id === t.id)
  if (hit) return { tone: 'on' as const, text: `展示中 · ${hit.items?.length ?? 0} 条` }
  return { tone: 'empty' as const, text: hideEmpty.value ? '候选无货 · 已隐藏' : '未进前 N 名' }
}

const homeTiles = computed(() => preview.value?.tiles ?? [])
</script>

<template>
  <div v-if="cfg">
    <PageHeader
      title="首页四格"
      description="第三层：首页右边每一格选一种「推荐类型」。类型决定捞什么货、怎么打分。"
    >
      <RouterLink to="/console/ops/recommend/profiles" class="btn-ghost">← 推荐类型</RouterLink>
      <button type="button" class="btn-ghost" :disabled="saving" @click="refreshPreview">刷新预览</button>
      <button type="button" class="btn-ghost" :disabled="saving" @click="resetDefaults">恢复默认</button>
      <button type="button" class="btn-primary" :disabled="saving" @click="onSave">保存</button>
    </PageHeader>

    <div class="card mb-5">
      <div class="mb-3 flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 class="card-title">买家现在看到的</h2>
          <p class="hint">按各格所选推荐类型打分后的结果。</p>
        </div>
        <label class="flex items-center gap-2 text-sm">
          <input v-model="hideEmpty" type="checkbox" />
          没货就不显示该格
        </label>
      </div>
      <div v-if="!homeTiles.length" class="rounded-xl border border-dashed border-slate-200 bg-slate-50 py-8 text-center text-sm text-slate-500">
        暂无可展示格子（多半是候选池没货）。
      </div>
      <div v-else class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <div v-for="t in homeTiles" :key="t.id" class="overflow-hidden rounded-xl border border-black/10">
          <div
            class="flex items-start gap-2 p-3"
            :style="{ background: themeOpts.find((l) => l.value === t.theme)?.swatch || '#eee' }"
          >
            <img :src="tileThemeIcon[t.theme] || tileThemeIcon.deal" alt="" class="h-10 w-10 rounded-lg object-cover" />
            <div class="min-w-0">
              <div class="text-[11px] font-bold text-black/50">{{ t.eng }}</div>
              <div class="truncate font-bold">{{ t.title }}</div>
              <div class="truncate text-[11px] text-black/55">算法：{{ profileName(t.profile_id || '') }}</div>
            </div>
          </div>
          <ul class="divide-y divide-slate-100 bg-white text-xs">
            <li v-for="(it, i) in t.items" :key="it.key" class="flex justify-between gap-2 px-3 py-2">
              <span class="truncate">{{ i + 1 }}. {{ it.plat }} · {{ it.price }}</span>
              <span class="font-mono text-slate-400">{{ it.score.toFixed(2) }}</span>
            </li>
          </ul>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="card-title">格子列表</h2>
          <p class="hint">每格必须选择一种推荐类型。列表越靠上越优先（最多 {{ maxTiles }} 格）。</p>
        </div>
        <button type="button" class="btn-primary" :disabled="saving" @click="addTile">＋ 新增格子</button>
      </div>

      <div class="mb-4 grid max-w-md grid-cols-2 gap-3">
        <div>
          <label class="label">首页最多几格</label>
          <input v-model.number="maxTiles" type="number" min="1" max="8" class="input" />
        </div>
      </div>

      <div class="space-y-3">
        <div
          v-for="(t, idx) in tiles"
          :key="t.id + '-' + idx"
          class="rounded-2xl border border-slate-200 p-4"
          :class="{ 'opacity-60': !t.enabled }"
        >
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <label class="flex items-center gap-1.5 text-sm font-semibold">
              <input v-model="t.enabled" type="checkbox" />
              参与首页
            </label>
            <span
              class="rounded-full px-2 py-0.5 text-[11px]"
              :class="{
                'bg-emerald-100 text-emerald-800': tileStatus(t).tone === 'on',
                'bg-slate-200 text-slate-600': tileStatus(t).tone === 'off',
                'bg-amber-100 text-amber-900': tileStatus(t).tone === 'empty',
              }"
            >{{ tileStatus(t).text }}</span>
            <div class="ml-auto flex gap-1">
              <button type="button" class="btn-ghost btn-sm" :disabled="idx === 0" @click="moveTile(idx, -1)">上移</button>
              <button type="button" class="btn-ghost btn-sm" :disabled="idx >= tiles.length - 1" @click="moveTile(idx, 1)">下移</button>
              <button type="button" class="btn-ghost btn-sm text-red-600" @click="removeTile(idx)">删除</button>
            </div>
          </div>

          <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div>
              <label class="label">标题</label>
              <input v-model="t.title" class="input" />
            </div>
            <div>
              <label class="label">说明</label>
              <input v-model="t.desc" class="input" />
            </div>
            <div>
              <label class="label">推荐类型（算法）</label>
              <select v-model="t.profile_id" class="input">
                <option v-for="p in cfg.profiles" :key="p.id" :value="p.id" :disabled="!p.enabled">
                  {{ p.name }}{{ p.enabled ? '' : '（已停用）' }}
                </option>
              </select>
              <p class="hint">在「推荐类型」里配置权重与候选池</p>
            </div>
            <div>
              <label class="label">格子样子</label>
              <select v-model="t.theme" class="input">
                <option v-for="l in themeOpts" :key="l.value" :value="l.value">{{ l.label }}</option>
              </select>
            </div>
            <div>
              <label class="label">角标英文</label>
              <input v-model="t.eng" class="input font-mono" />
            </div>
            <div>
              <label class="label">曝光加权</label>
              <input v-model.number="t.boost" type="number" step="0.1" min="0" max="10" class="input" />
            </div>
          </div>
          <details class="mt-2 text-xs text-slate-500">
            <summary class="cursor-pointer">内部 ID</summary>
            <input v-model="t.id" class="input mt-2 max-w-xs font-mono text-xs" />
          </details>
        </div>
      </div>

      <div class="mt-4 flex justify-end border-t border-slate-100 pt-4">
        <button type="button" class="btn-primary" :disabled="saving" @click="onSave">保存到首页</button>
      </div>
    </div>
  </div>
</template>
