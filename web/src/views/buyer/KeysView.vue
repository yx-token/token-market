<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { buyer, market } from '@/api'
import type { BuyerKey, Limits, MarketItem, PoolItem } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import RouteEditor from '@/components/RouteEditor.vue'
import { dateTime, money } from '@/utils/format'
import { toRoute, validateChain, type HopChoice } from '@/utils/route'

const route = useRoute()
const toast = useToast()
const cfg = useConfig()
const keys = useLoader(() => buyer.keys(), [] as BuyerKey[] | null)
const listings = ref<MarketItem[]>([])
const pools = ref<PoolItem[]>([])
const { busy, run } = useAction()

// 创建 / 编辑表单
const editing = ref<BuyerKey | null>(null)
const showForm = ref(false)
const form = reactive({
  name: '',
  type: 'default' as 'default' | 'chain',
  pool: '',
  hops: [] as HopChoice[],
  quota: '',
  h5: '',
  d1: '',
  d7: '',
  ips: '',
  expires: '',
})
const created = ref<{ key: string; name: string } | null>(null)

onMounted(async () => {
  keys.load()
  await cfg.ensure()
  const [ls, ps] = await Promise.all([market.listings({ limit: 500 }).catch(() => null), market.pools().catch(() => null)])
  listings.value = ls ?? []
  pools.value = ps ?? []
  const pre = Number(route.query.listing)
  if (pre) {
    const l = listings.value.find((x) => x.listing_id === pre)
    openCreate()
    form.type = 'chain'
    form.hops = [{ listingId: pre, platform: l?.platform ?? '' }]
    form.name = `指定卖家 #${pre}`
  }
})

function resetForm() {
  Object.assign(form, { name: '', type: 'default', pool: pools.value[0]?.name ?? '', hops: [], quota: '', h5: '', d1: '', d7: '', ips: '', expires: '' })
}

function openCreate() {
  editing.value = null
  resetForm()
  showForm.value = true
}

function openEdit(k: BuyerKey) {
  editing.value = k
  const hops: HopChoice[] = (k.hops ?? []).map((h) =>
    h.pool
      ? { pool: h.pool, platform: pools.value.find((p) => p.name === h.pool)?.platform ?? '' }
      : { listingId: h.listing_id, platform: listings.value.find((l) => l.listing_id === h.listing_id)?.platform ?? '' },
  )
  Object.assign(form, {
    name: k.name,
    type: k.route_type === 'chain' ? 'chain' : 'default',
    pool: k.route_type === 'default' ? (k.hops?.[0]?.pool ?? '') : (pools.value[0]?.name ?? ''),
    hops: k.route_type === 'chain' ? hops : [],
    quota: k.limits.quota_usd?.toString() ?? '',
    h5: k.limits.rate_limit_5h_usd?.toString() ?? '',
    d1: k.limits.rate_limit_1d_usd?.toString() ?? '',
    d7: k.limits.rate_limit_7d_usd?.toString() ?? '',
    ips: (k.limits.ip_whitelist ?? []).join(', '),
    expires: k.limits.expires_at ? k.limits.expires_at.slice(0, 10) : '',
  })
  showForm.value = true
}

function num(s: string): number | undefined {
  const n = Number(s)
  return s.trim() && n > 0 ? n : undefined
}

const limits = computed<Limits>(() => ({
  quota_usd: num(form.quota),
  rate_limit_5h_usd: num(form.h5),
  rate_limit_1d_usd: num(form.d1),
  rate_limit_7d_usd: num(form.d7),
  ip_whitelist: form.ips.split(/[,\s]+/).filter(Boolean),
  expires_at: form.expires ? new Date(`${form.expires}T23:59:59`).toISOString() : undefined,
}))

async function save() {
  if (!form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  const err = form.type === 'chain' ? validateChain(form.hops, cfg.config.max_hops) : form.pool ? null : '请选择默认分组'
  if (err) {
    toast.error(err)
    return
  }
  const routeInput = toRoute(form.type, form.pool, form.hops)
  if (editing.value) {
    const ok = await run(() => buyer.updateKey(editing.value!.id, { name: form.name, route: routeInput, limits: limits.value }), '已保存')
    if (ok) {
      showForm.value = false
      keys.load()
    }
    return
  }
  const res = await run(() => buyer.createKey({ name: form.name, route: routeInput, limits: limits.value }))
  if (res) {
    showForm.value = false
    created.value = { key: res.key, name: res.detail.name }
    keys.load()
  }
}

async function toggle(k: BuyerKey) {
  if (await run(() => buyer.setKeyEnabled(k.id, k.status !== 'active'), k.status === 'active' ? '已停用' : '已启用')) keys.load()
}

async function remove(k: BuyerKey) {
  if (!confirm(`删除 Key「${k.name}」？删除后立即失效，不能恢复。`)) return
  if (await run(() => buyer.deleteKey(k.id), '已删除')) keys.load()
}

function hopText(h: { listing_id?: number; pool?: string }) {
  return h.pool ? `默认分组 ${h.pool}` : `卖家挂单 #${h.listing_id}`
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success('已复制')
  } catch {
    toast.error('复制失败，请手动选择复制')
  }
}

const baseURL = computed(() => cfg.config.edge_url || window.location.origin)
</script>

<template>
  <div>
    <PageHeader title="API Key" description="每个 Key 可以走平台默认分组，也可以指定卖家并按顺序故障转移。">
      <button class="btn-primary" @click="openCreate">创建 Key</button>
    </PageHeader>

    <div class="card mb-5 text-sm">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-slate-500">调用地址</span>
        <code class="rounded bg-slate-100 px-2 py-0.5">{{ baseURL }}</code>
        <button class="btn-ghost btn-sm" @click="copy(baseURL)">复制</button>
      </div>
      <p class="hint">兼容官方协议：Anthropic Messages（/v1/messages）、OpenAI Chat Completions 与 Responses、Gemini。把 SDK 的 Base URL 换成上面的地址，Key 换成这里创建的 Key 即可。</p>
    </div>

    <div v-if="keys.loading.value && !keys.data.value?.length" class="py-10 text-center text-slate-400">加载中…</div>
    <div v-else-if="!keys.data.value?.length" class="card py-10 text-center text-slate-500">还没有 Key，先创建一个。</div>
    <div class="space-y-3">
      <div v-for="k in keys.data.value ?? []" :key="k.id" class="card">
        <div class="flex flex-wrap items-center gap-3">
          <div class="font-medium">{{ k.name }}</div>
          <code class="text-xs text-slate-500">{{ k.key_prefix }}…</code>
          <StatusBadge kind="keyStatus" :value="k.status" />
          <span v-if="k.limit_blocked_until" class="rounded bg-red-50 px-1.5 py-0.5 text-xs text-red-700">
            已限额：{{ k.limit_block_reason }}（至 {{ dateTime(k.limit_blocked_until) }}）
          </span>
          <span class="text-xs text-slate-400">创建于 {{ dateTime(k.created_at) }}</span>
          <div class="ml-auto flex gap-2">
            <button class="btn-secondary btn-sm" @click="openEdit(k)">编辑</button>
            <button class="btn-secondary btn-sm" :disabled="busy || k.status === 'provisioning'" @click="toggle(k)">{{ k.status === 'active' ? '停用' : '启用' }}</button>
            <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="remove(k)">删除</button>
          </div>
        </div>
        <ol class="mt-3 flex flex-wrap items-center gap-2 text-sm">
          <li v-for="h in k.hops ?? []" :key="h.position" class="flex items-center gap-2">
            <span v-if="h.position > 1" class="text-slate-300">→</span>
            <span class="rounded border border-slate-200 px-2 py-1">
              {{ hopText(h) }}
              <StatusBadge kind="hopState" :value="h.state" class="ml-1" />
            </span>
          </li>
        </ol>
        <div class="mt-2 flex flex-wrap gap-4 text-xs text-slate-500">
          <span v-if="k.limits.quota_usd">总额度 {{ money(k.limits.quota_usd) }}</span>
          <span v-if="k.limits.rate_limit_5h_usd">5 小时 {{ money(k.limits.rate_limit_5h_usd) }}</span>
          <span v-if="k.limits.rate_limit_1d_usd">每天 {{ money(k.limits.rate_limit_1d_usd) }}</span>
          <span v-if="k.limits.rate_limit_7d_usd">7 天 {{ money(k.limits.rate_limit_7d_usd) }}</span>
          <span v-if="k.limits.ip_whitelist?.length">IP 白名单 {{ k.limits.ip_whitelist.join(', ') }}</span>
          <span v-if="k.limits.expires_at">到期 {{ dateTime(k.limits.expires_at) }}</span>
        </div>
      </div>
    </div>

    <ModalDialog v-model:open="showForm" :title="editing ? '编辑 Key' : '创建 Key'" wide>
      <div>
        <label class="label" for="kname">名称</label>
        <input id="kname" v-model="form.name" class="input" maxlength="100" placeholder="例如 claude-code" />
      </div>
      <div>
        <div class="label">路由</div>
        <RouteEditor v-model:type="form.type" v-model:pool="form.pool" v-model:hops="form.hops" :listings="listings" :pools="pools" :max-hops="cfg.config.max_hops" />
      </div>
      <details class="rounded border border-slate-200 p-3">
        <summary class="cursor-pointer text-sm font-medium text-slate-700">限制（可选）</summary>
        <div class="mt-3 grid grid-cols-2 gap-3">
          <div>
            <label class="label" for="quota">总花费上限（元）</label>
            <input id="quota" v-model="form.quota" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="h5">每 5 小时上限</label>
            <input id="h5" v-model="form.h5" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="d1">每天上限</label>
            <input id="d1" v-model="form.d1" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="d7">每 7 天上限</label>
            <input id="d7" v-model="form.d7" class="input" inputmode="decimal" />
          </div>
          <div class="col-span-2">
            <label class="label" for="ips">IP 白名单（逗号分隔，支持 CIDR）</label>
            <input id="ips" v-model="form.ips" class="input" placeholder="203.0.113.0/24, 198.51.100.7" />
          </div>
          <div>
            <label class="label" for="exp">到期日</label>
            <input id="exp" v-model="form.expires" class="input" type="date" />
          </div>
        </div>
        <p class="hint">花费限额按已入账用量统计，有几分钟延迟。</p>
      </details>
      <template #footer>
        <button class="btn-secondary" @click="showForm = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="save">{{ editing ? '保存' : '创建' }}</button>
      </template>
    </ModalDialog>

    <ModalDialog :open="!!created" title="Key 已创建" @update:open="(v: boolean) => !v && (created = null)">
      <p class="text-sm text-amber-700">请立即保存，Key 只显示这一次。</p>
      <div class="flex items-center gap-2">
        <code class="flex-1 break-all rounded bg-slate-100 p-2 text-sm">{{ created?.key }}</code>
        <button class="btn-secondary btn-sm" @click="copy(created!.key)">复制</button>
      </div>
      <pre class="overflow-x-auto rounded bg-slate-900 p-3 text-xs text-slate-100">curl {{ baseURL }}/v1/messages \
  -H "x-api-key: {{ created?.key }}" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"你好"}]}'</pre>
      <template #footer>
        <button class="btn-primary" @click="created = null">我已保存</button>
      </template>
    </ModalDialog>
  </div>
</template>
