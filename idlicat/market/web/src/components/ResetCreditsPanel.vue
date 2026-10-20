<script setup lang="ts">
// 重置卡：官方发放的一次性额度重置（Codex、Claude OAuth）。卖家可以手动用卡；
// Codex 账号还可以授权平台在窗口用量达到阈值时自动用卡，用卡记录（手动与自动）都列在这里。
import { computed, onMounted, ref } from 'vue'
import { seller } from '@/api'
import { ApiError } from '@/api/client'
import type { ResetCreditEvent, ResetCredits } from '@/api/types'
import { useToast } from '@/stores/toast'
import { dateTime } from '@/utils/format'
import ModalDialog from '@/components/ModalDialog.vue'

const props = defineProps<{ upstreamId: number }>()
const emit = defineEmits<{ changed: [] }>()
const toast = useToast()
const rc = ref<ResetCredits | null>(null)
const loading = ref(false)
const busy = ref(false)
// 网络中断时用卡结果未知：再次确认沿用同一个请求号，后端直接返回首次结果，不会重复用卡；手动刷新后作废
let pendingRequest: string | null = null

async function load(refresh = false) {
  loading.value = true
  try {
    rc.value = await seller.resetCredits(props.upstreamId, refresh)
    if (refresh) {
      pendingRequest = null
      if (!rc.value.error) toast.success('已刷新')
    }
  } catch (e) {
    toast.error(e)
  } finally {
    loading.value = false
  }
}
onMounted(() => load())

const isClaude = computed(() => rc.value?.platform === 'anthropic')
const canRedeem = computed(() => !!rc.value?.queried && rc.value.available_count > 0)

const windowText: Record<string, string> = {
  five_hour: '5 小时',
  seven_day: '7 天',
  seven_day_overage_included: '7 天（含超额）',
  '5h': '5 小时',
  '7d': '7 天',
}
const windows = (ws?: string[]) => (ws ?? []).map((w) => windowText[w] ?? w).join('、')

const eventStatus: Record<ResetCreditEvent['status'], { text: string; chip: string }> = {
  pending: { text: '进行中', chip: 'chip-blue' },
  success: { text: '已重置', chip: 'chip-green' },
  no_credit: { text: '没有可用的卡', chip: 'chip-gray' },
  failed: { text: '失败', chip: 'chip-red' },
  unknown: { text: '结果未知', chip: 'chip-yellow' },
}
const autoStatus: Record<string, string> = {
  checking: '正在向官方确认',
  available: '有可用的卡',
  resetting: '正在用卡',
  success: '刚刚用卡成功',
  no_credit: '没有可用的卡，平台已暂停售卖，等待窗口自然重置',
  failed: '用卡失败，平台已暂停售卖，稍后自动重试',
}

// ---- 手动用卡 ----
const showConfirm = ref(false)

function newRequestId() {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
}

async function redeem() {
  if (!rc.value || busy.value) return
  const requestId = pendingRequest ?? newRequestId()
  pendingRequest = requestId
  busy.value = true
  try {
    const ev = await seller.redeemResetCredit(props.upstreamId, requestId, rc.value.available_count)
    pendingRequest = null
    showConfirm.value = false
    if (ev.status === 'success') toast.success(ev.windows.length ? `已重置 ${windows(ev.windows)} 窗口` : '已用卡，官方额度已重置')
    else if (ev.status === 'unknown') toast.error('用卡结果未知，卡可能已用掉。请稍后刷新查看额度与重置卡')
    else toast.error(`用卡失败（${ev.error_code ?? ev.status}）`)
    emit('changed')
  } catch (e) {
    if (e instanceof ApiError && e.status === 0) {
      toast.error('网络中断，用卡结果未知。请先刷新查看；再次确认会沿用这次请求，不会重复用卡')
      return
    }
    pendingRequest = null
    toast.error(e)
    if (e instanceof ApiError && e.code === 'RESET_CREDITS_CHANGED') showConfirm.value = false
  } finally {
    busy.value = false
    await load()
  }
}

// ---- 授权自动用卡 ----
const showAuto = ref(false)
const autoEnabled = ref(false)
const auto5h = ref<number | ''>('')
const auto7d = ref<number | ''>('')

function openAuto() {
  const a = rc.value?.auto
  if (!a) return
  autoEnabled.value = a.enabled
  auto5h.value = a.threshold_5h_percent ?? ''
  auto7d.value = a.threshold_7d_percent ?? ''
  showAuto.value = true
}
async function saveAuto() {
  busy.value = true
  try {
    await seller.setAutoReset(props.upstreamId, {
      enabled: autoEnabled.value,
      threshold_5h_percent: auto5h.value === '' ? undefined : Number(auto5h.value),
      threshold_7d_percent: auto7d.value === '' ? undefined : Number(auto7d.value),
    })
    toast.success(autoEnabled.value ? '已授权，挂单在售时生效' : '已取消授权')
    showAuto.value = false
    await load()
  } catch (e) {
    toast.error(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="mb-3 flex flex-wrap items-center gap-3">
      <h2 class="card-title !mb-0">重置卡</h2>
      <span v-if="rc?.queried" class="chip chip-ink">可用 {{ rc.available_count }} 张</span>
      <span v-if="rc?.queried" class="text-xs text-slate-400">
        {{ rc.source === 'snapshot' ? '最近一次查询的结果' : '向官方查询' }}<template v-if="rc.fetched_at"> · {{ dateTime(rc.fetched_at) }}</template>
      </span>
      <div class="ml-auto flex gap-2">
        <button class="btn-ghost btn-sm" :disabled="loading" @click="load(true)">{{ loading ? '查询中…' : '查询' }}</button>
        <button class="btn-secondary btn-sm" :disabled="!canRedeem || busy" @click="showConfirm = true">使用一张</button>
      </div>
    </div>
    <p v-if="rc?.error" class="mb-2 text-sm text-amber-700">{{ rc.error }}</p>
    <p v-if="rc && !rc.queried && !rc.error" class="text-sm text-slate-400">
      {{ isClaude ? 'Claude 的重置卡需要向官方查询，点击"查询"即可，查询不会消耗。' : '还没有重置卡信息，点击"查询"向官方确认。' }}
    </p>
    <p v-if="rc?.queried && !rc.credits.length && rc.available_count === 0" class="text-sm text-slate-400">官方显示这个账号目前没有可用的重置卡。</p>

    <ul v-if="rc?.credits.length" class="space-y-1 text-sm">
      <li v-for="(c, i) in rc.credits" :key="i" class="flex flex-wrap items-baseline gap-2">
        <span v-if="c.label" class="font-medium">{{ c.label }}</span>
        <span v-if="isClaude" class="num">剩 {{ c.resets_left }} 次</span>
        <span v-if="c.clears?.length" class="text-slate-500">恢复 {{ windows(c.clears) }} 窗口</span>
        <span v-if="c.expires_at" class="text-xs text-slate-400">{{ dateTime(c.expires_at) }} 到期</span>
        <span v-if="isClaude && !c.redeemable" class="chip chip-gray">暂不可用{{ c.blocking?.length ? `：${c.blocking.join('、')}` : '' }}</span>
      </li>
    </ul>
    <p v-if="rc?.cooldown_until" class="mt-1 text-xs text-slate-500">官方冷却中，{{ dateTime(rc.cooldown_until) }} 后才能再次用卡。</p>

    <div v-if="rc?.auto" class="mt-4 border-t border-ink-100 pt-3">
      <div class="flex flex-wrap items-center gap-2 text-sm">
        <span class="font-medium">自动用卡</span>
        <span :class="['chip', rc.auto.enabled ? 'chip-green' : 'chip-gray']">{{ rc.auto.enabled ? '已授权' : '未授权' }}</span>
        <button class="btn-ghost btn-sm ml-auto" @click="openAuto">设置</button>
      </div>
      <p v-if="rc.auto.enabled" class="mt-1 text-sm text-slate-600">
        5 小时窗口用量达到 {{ rc.auto.effective_5h_percent }}%、或 7 天窗口达到 {{ rc.auto.effective_7d_percent }}% 时，平台暂停售卖并自动使用一张最早到期的卡，用卡成功后恢复售卖。
        <template v-if="!rc.auto.active">挂单当前不在售卖，暂不会自动用卡。</template>
      </p>
      <p v-else class="mt-1 text-sm text-slate-400">授权后，窗口快用完时平台可以替你用卡，继续售卖。</p>
      <p v-if="rc.auto.enabled && rc.auto.state" class="mt-1 text-xs text-slate-500">
        当前：{{ autoStatus[rc.auto.state.status] ?? rc.auto.state.status }}
        <template v-if="rc.auto.state.error_code">（{{ rc.auto.state.error_code }}）</template>
        <template v-if="rc.auto.state.checked_at"> · {{ dateTime(rc.auto.state.checked_at) }}</template>
      </p>
    </div>

    <div v-if="rc?.events.length" class="mt-4 border-t border-ink-100 pt-3">
      <div class="mb-1 text-sm font-medium">用卡记录</div>
      <ul class="space-y-1 text-sm">
        <li v-for="ev in rc.events" :key="ev.id" class="flex flex-wrap items-baseline gap-2">
          <span class="text-xs text-slate-400">{{ dateTime(ev.created_at) }}</span>
          <span>{{ ev.trigger === 'auto' ? '平台自动' : '手动' }}</span>
          <span :class="['chip', eventStatus[ev.status]?.chip ?? 'chip-gray']">{{ eventStatus[ev.status]?.text ?? ev.status }}</span>
          <span v-if="ev.windows.length" class="text-slate-500">{{ ev.trigger === 'auto' ? '触发' : '恢复' }} {{ windows(ev.windows) }} 窗口</span>
          <span v-if="ev.error_code && ev.status !== 'success'" class="text-xs text-slate-400">{{ ev.error_code }}</span>
        </li>
      </ul>
    </div>

    <ModalDialog v-model:open="showConfirm" title="使用重置卡">
      <p class="text-sm text-slate-600">
        将消耗 1 张重置卡，由官方立即恢复额度窗口。<strong>此操作不可撤销。</strong>当前可用 {{ rc?.available_count }} 张，用后剩 {{ Math.max(0, (rc?.available_count ?? 0) - 1) }} 张。
      </p>
      <p class="hint">窗口还剩较多额度时用卡会浪费剩余部分，建议在快用完时再用。{{ isClaude ? 'Claude 的卡通常要窗口真正用满才能使用。' : '' }}</p>
      <template #footer>
        <button class="btn-secondary" :disabled="busy" @click="showConfirm = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="redeem">{{ busy ? '用卡中…' : '确认使用' }}</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showAuto" title="自动用卡">
      <label class="flex items-center gap-2 text-sm"><input v-model="autoEnabled" type="checkbox" /> 授权平台在窗口快用完时自动使用我的重置卡</label>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="label" for="auto-5h">5 小时窗口阈值（%）</label>
          <input id="auto-5h" v-model.number="auto5h" class="input" type="number" min="1" :max="rc?.auto?.pause_line_percent" :placeholder="`跟随暂停线 ${rc?.auto?.pause_line_percent}`" />
        </div>
        <div>
          <label class="label" for="auto-7d">7 天窗口阈值（%）</label>
          <input id="auto-7d" v-model.number="auto7d" class="input" type="number" min="1" :max="rc?.auto?.pause_line_percent" :placeholder="`跟随暂停线 ${rc?.auto?.pause_line_percent}`" />
        </div>
      </div>
      <ul class="hint list-disc space-y-1 pl-4">
        <li>任一窗口的用量达到阈值时，平台暂停售卖并使用一张最早到期的卡，用卡成功后恢复售卖；没有可用的卡时保持暂停，直到窗口自然重置。</li>
        <li>阈值不能高于暂停线（100 − 自留比例 = {{ rc?.auto?.pause_line_percent }}%），留空表示跟随暂停线；低于暂停线时平台在阈值处就会暂停。用量包含你在平台之外的使用。</li>
        <li>只在挂单正在售卖时生效，每次自动用卡都会记录在用卡记录中。</li>
      </ul>
      <template #footer>
        <button class="btn-secondary" @click="showAuto = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="saveAuto">保存</button>
      </template>
    </ModalDialog>
  </div>
</template>
