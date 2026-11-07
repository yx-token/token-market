<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useMarketBoard } from '@/stores/marketBoard'
import { usd, multiplier } from '@/utils/format'
import {
  applyMult,
  buildModelSquare,
  type ModelCapability,
  type ModelChannelGroup,
  type ModelRateRow,
} from '@/utils/modelSquare'

const board = useMarketBoard()
const q = ref('')
const channelFilter = ref<string | 'all'>('all')
const capabilityFilter = ref<ModelCapability | 'all'>('all')
const loading = ref(true)

const CAPABILITY_TABS: { id: ModelCapability | 'all'; label: string }[] = [
  { id: 'all', label: '全部能力' },
  { id: 'text', label: '文本' },
  { id: 'image', label: '图像' },
  { id: 'video', label: '视频' },
  { id: 'other', label: '其他' },
]

onMounted(async () => {
  board.start()
  await board.load()
  loading.value = false
})

const groups = computed(() => buildModelSquare(board.prices, board.listings, board.pools))

const capabilityCounts = computed(() => {
  const counts: Record<ModelCapability | 'all', number> = { all: 0, text: 0, image: 0, video: 0, other: 0 }
  for (const g of groups.value) {
    for (const r of g.rows) {
      counts.all += 1
      counts[r.capability] += 1
    }
  }
  return counts
})

const filtered = computed(() => {
  const needle = q.value.trim().toLowerCase()
  return groups.value
    .filter((g) => channelFilter.value === 'all' || g.id === channelFilter.value)
    .map((g) => ({
      ...g,
      rows: g.rows.filter((r) => {
        if (capabilityFilter.value !== 'all' && r.capability !== capabilityFilter.value) return false
        if (
          needle &&
          !r.model.toLowerCase().includes(needle) &&
          !(r.official.provider ?? '').toLowerCase().includes(needle) &&
          !g.title.toLowerCase().includes(needle)
        ) {
          return false
        }
        return true
      }),
    }))
    .filter((g) => g.rows.length > 0)
})

const totalModels = computed(() => filtered.value.reduce((n, g) => n + g.rows.length, 0))
const catalogTotal = computed(() => board.prices.length)
const channelTotal = computed(() => groups.value.length)

function goMarket(g: ModelChannelGroup, row?: ModelRateRow) {
  const query: Record<string, string> = {}
  if (g.plat) query.plat = g.plat
  if (row) query.q = row.model
  return { path: '/', query }
}

function paid(official: number, mult: number | null) {
  return applyMult(official, mult)
}

function cacheCell(write: number, read: number, mult: number | null, paidMode: boolean) {
  const w = paidMode ? paid(write, mult) : write
  const r = paidMode ? paid(read, mult) : read
  if ((w === null || w === 0) && (r === null || r === 0)) return '—'
  const fmt = (v: number | null) => (v === null ? '—' : usd(v))
  return `写 ${fmt(w)} / 读 ${fmt(r)}`
}

function ioPair(input: number, output: number, mult: number | null = null) {
  const i = mult === null ? input : paid(input, mult)
  const o = mult === null ? output : paid(output, mult)
  return `${usd(i ?? undefined)} / ${usd(o ?? undefined)}`
}

</script>

<template>
  <div class="msq">
    <section class="msq-hero">
      <div class="msq-hero-inner">
        <p class="msq-kicker">模型价格</p>
        <h1 class="msq-title">官方价对照 · 市场实付一眼看清</h1>
        <p class="msq-lead">
          汇聚全球主流 API 渠道的官方价目（OpenAI、Anthropic、Google、Mistral、通义、火山、Bedrock、Groq、Together…），价格按官方美元标价 × 市场在售最低倍率估算。
        </p>
        <div class="msq-toolbar">
          <label class="msq-search">
            <span class="sr-only">搜索模型</span>
            <input v-model="q" type="search" placeholder="搜索模型或渠道，如 mistral、通义、bedrock…" />
          </label>
          <div class="msq-chips" role="tablist" aria-label="能力筛选">
            <button
              v-for="tab in CAPABILITY_TABS"
              :key="tab.id"
              type="button"
              class="msq-chip"
              :class="{ on: capabilityFilter === tab.id }"
              @click="capabilityFilter = tab.id"
            >
              {{ tab.label }}
              <span v-if="!loading" class="msq-chip-n">{{ capabilityCounts[tab.id] }}</span>
            </button>
          </div>
          <div class="msq-chips" role="tablist" aria-label="渠道筛选">
            <button
              type="button"
              class="msq-chip"
              :class="{ on: channelFilter === 'all' }"
              @click="channelFilter = 'all'"
            >全部渠道 <span class="msq-chip-n">{{ channelTotal }}</span></button>
            <button
              v-for="g in groups"
              :key="g.id"
              type="button"
              class="msq-chip"
              :class="{ on: channelFilter === g.id }"
              :style="channelFilter === g.id ? { '--chip': g.accent } : undefined"
              @click="channelFilter = g.id"
            >{{ g.title }} <span class="msq-chip-n">{{ g.rows.length }}</span></button>
          </div>
        </div>
        <p class="msq-meta">
          <template v-if="loading">加载价目中…</template>
          <template v-else>
            当前筛选 {{ totalModels }} 个 · {{ channelTotal }} 个渠道 · 价目共 {{ catalogTotal }} 个模型
            <span v-if="board.updatedAt"> · 更新于 {{ board.updatedAt.toLocaleTimeString() }}</span>
          </template>
        </p>
      </div>
    </section>

    <div class="msq-body">
      <p v-if="!loading && !filtered.length" class="msq-empty">
        暂无匹配的官方价目。可先到
        <RouterLink to="/">市场</RouterLink>
        看看在售挂单，或稍后再刷新。
      </p>

      <article
        v-for="g in filtered"
        :id="`family-${g.id}`"
        :key="g.id"
        class="msq-card"
        :style="{ '--accent': g.accent, '--tint': g.tint }"
      >
        <header class="msq-card-head">
          <div class="msq-card-brand">
            <img v-if="g.icon" class="msq-card-icon" :src="g.icon" :alt="g.title" width="28" height="28" />
            <span v-else class="msq-card-mark" aria-hidden="true">{{ g.title.slice(0, 1) }}</span>
            <div>
              <h2 class="msq-card-title">{{ g.title }}</h2>
              <p class="msq-card-hint">{{ g.hint }}</p>
            </div>
          </div>
          <div class="msq-card-side">
            <span v-if="g.bestMult !== null" class="msq-badge">市场低至 {{ multiplier(g.bestMult) }}</span>
            <span v-else class="msq-badge muted">暂无在售倍率</span>
            <RouterLink class="msq-go" :to="goMarket(g)">去市场</RouterLink>
          </div>
        </header>

        <!-- 桌面紧凑表：入/出合并，避免右侧被裁切 -->
        <div class="msq-table-wrap">
          <p class="msq-scroll-hint">价格单位：美元 / 百万 token · 较窄时可左右滑动</p>
          <table class="msq-table">
            <thead>
              <tr>
                <th class="col-model">模型</th>
                <th class="col-off">官方价（入 / 出）</th>
                <th class="col-paid">实付估算（入 / 出）</th>
                <th class="col-cache">缓存（写 / 读）</th>
                <th class="col-mult">倍率</th>
                <th class="col-act"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in g.rows" :key="row.model">
                <td class="col-model">
                  <code>{{ row.model }}</code>
                  <span v-if="row.official.provider" class="msq-prov">{{ row.official.provider }}</span>
                </td>
                <td class="col-off">{{ ioPair(row.official.input_per_1m, row.official.output_per_1m) }}</td>
                <td class="col-paid strong">{{ row.bestMult === null ? '—' : ioPair(row.official.input_per_1m, row.official.output_per_1m, row.bestMult) }}</td>
                <td class="col-cache">{{ cacheCell(row.official.cache_write_per_1m, row.official.cache_read_per_1m, row.bestMult, true) }}</td>
                <td class="col-mult">
                  <span v-if="row.bestMult !== null" class="msq-mult">{{ multiplier(row.bestMult) }}</span>
                  <span v-else class="msq-mult none">—</span>
                </td>
                <td class="col-act">
                  <RouterLink class="msq-row-go" :to="goMarket(g, row)">选购</RouterLink>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- H5：每模型一张卡 -->
        <ul class="msq-mobile">
          <li v-for="row in g.rows" :key="`m-${row.model}`" class="msq-mcard">
            <div class="msq-mcard-top">
              <code>{{ row.model }}</code>
              <span v-if="row.bestMult !== null" class="msq-mult">{{ multiplier(row.bestMult) }}</span>
              <span v-else class="msq-mult none">无货</span>
            </div>
            <div class="msq-mcard-grid">
              <div class="msq-mblock paid">
                <span class="msq-mblock-label">实付估算</span>
                <div class="msq-mpairs">
                  <span>输入 <b>{{ usd(paid(row.official.input_per_1m, row.bestMult) ?? undefined) }}</b></span>
                  <span>输出 <b>{{ usd(paid(row.official.output_per_1m, row.bestMult) ?? undefined) }}</b></span>
                </div>
                <p class="msq-mcache">{{ cacheCell(row.official.cache_write_per_1m, row.official.cache_read_per_1m, row.bestMult, true) }}</p>
              </div>
              <div class="msq-mblock off">
                <span class="msq-mblock-label">官方价</span>
                <div class="msq-mpairs">
                  <span>输入 {{ usd(row.official.input_per_1m) }}</span>
                  <span>输出 {{ usd(row.official.output_per_1m) }}</span>
                </div>
                <p class="msq-mcache">{{ cacheCell(row.official.cache_write_per_1m, row.official.cache_read_per_1m, null, false) }}</p>
              </div>
            </div>
            <RouterLink class="msq-mcard-go" :to="goMarket(g, row)">去市场看同款</RouterLink>
          </li>
        </ul>
      </article>
    </div>
  </div>
</template>

<style scoped>
.msq {
  --msq-ink: #2f2f2f;
  --msq-muted: #8a8a8a;
  --msq-line: rgba(0, 0, 0, 0.06);
  color: var(--msq-ink);
  background:
    radial-gradient(1200px 420px at 10% -10%, rgba(255, 236, 120, 0.35), transparent 60%),
    radial-gradient(900px 380px at 90% 0%, rgba(255, 186, 140, 0.18), transparent 55%),
    #fafafa;
  min-height: 100%;
  padding-bottom: 48px;
}
.msq-hero {
  border-bottom: 1px solid var(--msq-line);
  background: linear-gradient(180deg, rgba(255, 252, 237, 0.9), rgba(250, 250, 250, 0.2));
}
.msq-hero-inner {
  margin: 0 auto;
  padding: 28px 0 22px;
}
.msq-kicker {
  margin: 0 0 8px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #a07a00;
}
.msq-title {
  margin: 0 0 10px;
  font-family: var(--display, "Instrument Sans", "DM Sans", "PingFang SC", sans-serif);
  font-size: clamp(22px, 4vw, 32px);
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1.2;
}
.msq-lead {
  margin: 0 0 18px;
  max-width: 52rem;
  font-size: 14px;
  line-height: 1.65;
  color: var(--msq-muted);
}
.msq-toolbar {
  display: grid;
  gap: 12px;
}
.msq-search input {
  width: 100%;
  max-width: 420px;
  height: 40px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid var(--msq-line);
  background: #fff;
  outline: none;
}
.msq-search input:focus {
  border-color: rgba(255, 196, 0, 0.8);
  box-shadow: 0 0 0 3px rgba(255, 212, 0, 0.25);
}
.msq-chips {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
  padding-bottom: 2px;
}
.msq-chips::-webkit-scrollbar { display: none; }
.msq-chip {
  flex: 0 0 auto;
  height: 32px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--msq-line);
  background: #fff;
  color: var(--msq-muted);
  font-size: 13px;
}
.msq-chip.on {
  color: #fff;
  border-color: transparent;
  background: var(--chip, #3a3a3a);
}
.msq-chip-n {
  margin-left: 4px;
  font-size: 11px;
  opacity: 0.75;
}
.msq-meta {
  margin: 12px 0 0;
  font-size: 12px;
  color: var(--msq-muted);
}
.msq-hero-inner,
.msq-body {
  width: min(1180px, calc(100% - 24px));
}
.msq-body {
  margin: 20px auto 0;
  display: grid;
  gap: 16px;
  min-width: 0;
}
.msq-empty {
  padding: 28px;
  text-align: center;
  color: var(--msq-muted);
  background: #fff;
  border: 1px solid var(--msq-line);
  border-radius: 16px;
}
.msq-empty a { color: #b8860b; text-decoration: underline; }

.msq-card {
  background: #fff;
  border: 1px solid var(--msq-line);
  border-radius: 18px;
  overflow: clip;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.04);
  min-width: 0;
  max-width: 100%;
}
.msq-card-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--msq-line);
  background: linear-gradient(90deg, var(--tint), transparent 70%);
}
.msq-card-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.msq-card-icon {
  width: 28px;
  height: 28px;
  object-fit: contain;
}
.msq-card-mark {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: var(--accent);
  color: #fff;
  font-size: 13px;
  font-weight: 700;
}
.msq-card-title {
  margin: 0;
  font-size: 17px;
  font-weight: 700;
}
.msq-card-hint {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--msq-muted);
}
.msq-card-side {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.msq-badge {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 10px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--accent) 14%, white);
  color: var(--accent);
  font-size: 12px;
  font-weight: 700;
}
.msq-badge.muted {
  background: #f3f3f5;
  color: var(--msq-muted);
  font-weight: 600;
}
.msq-go,
.msq-row-go,
.msq-mcard-go {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: #ffd400;
  color: #2f2f2f;
  font-size: 12px;
  font-weight: 700;
}
.msq-go:hover,
.msq-row-go:hover,
.msq-mcard-go:hover {
  background: #f0c200;
}

.msq-table-wrap {
  overflow-x: auto;
  overscroll-behavior-x: contain;
  -webkit-overflow-scrolling: touch;
  max-width: 100%;
  scrollbar-gutter: stable;
}
.msq-scroll-hint {
  margin: 0;
  padding: 8px 14px 0;
  font-size: 11px;
  color: var(--msq-muted);
}
.msq-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  table-layout: auto;
}
.msq-table th,
.msq-table td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--msq-line);
  text-align: left;
  vertical-align: middle;
}
.msq-table thead th {
  font-size: 11px;
  font-weight: 700;
  color: var(--msq-muted);
  background: #fcfcfd;
  white-space: nowrap;
}
.msq-table .col-model {
  position: sticky;
  left: 0;
  z-index: 1;
  background: #fff;
  min-width: 11rem;
  max-width: 18rem;
}
.msq-table thead .col-model {
  background: #fcfcfd;
  z-index: 2;
}
.msq-table .col-paid {
  background: var(--tint);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.msq-table .col-off,
.msq-table .col-cache {
  color: #9a9a9a;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.msq-table td.strong {
  font-weight: 700;
  color: var(--msq-ink);
}
.msq-table .col-mult,
.msq-table .col-act {
  white-space: nowrap;
}
.msq-table code {
  display: inline-block;
  max-width: 100%;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
  white-space: normal;
  line-height: 1.35;
}
.msq-prov {
  display: block;
  margin-top: 2px;
  font-size: 11px;
  color: #9a9a9a;
  font-weight: 500;
}
.msq-mult {
  display: inline-flex;
  min-width: 3.2em;
  justify-content: center;
  padding: 2px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent) 12%, white);
  color: var(--accent);
  font-weight: 700;
  font-size: 12px;
}
.msq-mult.none {
  background: #f3f3f5;
  color: #aaa;
  font-weight: 500;
}

.msq-mobile { display: none; list-style: none; margin: 0; padding: 0; }

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  border: 0;
}

@media (max-width: 900px) {
  .msq-hero-inner { padding: 22px 0 16px; }
  .msq-table-wrap { display: none; }
  .msq-mobile {
    display: grid;
    gap: 10px;
    padding: 12px;
  }
  .msq-mcard {
    border: 1px solid var(--msq-line);
    border-radius: 14px;
    padding: 12px;
    background: #fff;
  }
  .msq-mcard-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 10px;
  }
  .msq-mcard-top code {
    font-size: 12px;
    word-break: break-all;
  }
  .msq-mcard-grid {
    display: grid;
    gap: 8px;
  }
  .msq-mblock {
    border-radius: 12px;
    padding: 10px 12px;
  }
  .msq-mblock.paid { background: var(--tint); }
  .msq-mblock.off { background: #f7f7f8; color: #8a8a8a; }
  .msq-mblock-label {
    display: block;
    font-size: 11px;
    font-weight: 700;
    margin-bottom: 6px;
    letter-spacing: 0.04em;
  }
  .msq-mpairs {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 16px;
    font-size: 13px;
  }
  .msq-mpairs b { color: var(--msq-ink); }
  .msq-mcache {
    margin: 6px 0 0;
    font-size: 11px;
    opacity: 0.85;
  }
  .msq-mcard-go {
    width: 100%;
    margin-top: 10px;
    height: 36px;
  }
  .msq-card-head { padding: 14px; }
}
</style>
