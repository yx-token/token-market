<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { PlatformLedger, QualityOverview, Sub2APIStatus } from '@/api/types'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import ConsoleIcon, { type ConsoleIconName } from '@/components/icons/ConsoleIcon.vue'
import { dateTime, money } from '@/utils/format'
import { distribution, label } from '@/utils/labels'

const toast = useToast()
const status = ref<Sub2APIStatus | null>(null)
const ledger = ref<PlatformLedger | null>(null)
const quality = ref<QualityOverview | null>(null)
const todo = ref<{ text: string; count: number; to: string; icon: ConsoleIconName }[]>([])

onMounted(async () => {
  try {
    const [s, l, q, kyc, wd, an, risk, ap, rc, rf] = await Promise.all([
      admin.sub2apiStatus(),
      admin.ledger(),
      admin.qualityOverview(),
      admin.pendingKYC(),
      admin.withdrawals('pending_review'),
      admin.anomalies('open'),
      admin.riskEvents('open'),
      admin.appeals('open'),
      admin.recharges('pending'),
      admin.refunds('paying'),
    ])
    status.value = s
    ledger.value = l
    quality.value = q
    todo.value = [
      { text: '待确认充值', count: rc?.length ?? 0, to: '/console/funds', icon: 'wallet' },
      { text: '待打款退款', count: rf?.length ?? 0, to: '/console/funds', icon: 'hand-coins' },
      { text: '待审核提现', count: wd?.length ?? 0, to: '/console/funds', icon: 'banknote' },
      { text: '待审核实名', count: kyc?.length ?? 0, to: '/console/users', icon: 'badge-check' },
      { text: '待处理申诉', count: ap?.length ?? 0, to: '/console/quality', icon: 'scale' },
      { text: '计费异常', count: an?.length ?? 0, to: '/console/billing', icon: 'circle-alert' },
      { text: '风险事件', count: risk?.length ?? 0, to: '/console/risk', icon: 'shield-alert' },
    ]
  } catch (e) {
    toast.error(e)
  }
})

const ledgerCards = computed(() => {
  if (!ledger.value) return []
  const l = ledger.value
  return [
    { label: '平台现金', value: money(l.cash), hint: '充值 − 退款 − 已打款提现 − 调整', tone: 'primary' as const, icon: 'banknote' as ConsoleIconName },
    { label: '平台收入', value: money(l.revenue), hint: '买家支付 − 卖家收益', tone: 'success' as const, icon: 'trending-up' as ConsoleIconName },
    { label: '探测成本', value: money(l.probe_cost), tone: 'warning' as const, icon: 'activity' as ConsoleIconName },
    { label: '人工调整', value: money(l.adjustment), tone: 'primary' as const, icon: 'sliders-horizontal' as ConsoleIconName },
    { label: '买家余额合计', value: money(l.buyer_balances), tone: 'primary' as const, icon: 'users' as ConsoleIconName },
    { label: '应付卖家合计', value: money(l.seller_payable), tone: 'warning' as const, icon: 'hand-coins' as ConsoleIconName },
  ]
})
</script>

<template>
  <div class="console-dashboard space-y-6">
    <PageHeader title="运营概览" description="待办事项、底层网关状态、平台资金与上游质量一览。" />

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-7">
      <RouterLink
        v-for="(t, i) in todo"
        :key="t.text"
        :to="t.to"
        class="liquid-stat group"
        :style="{ animationDelay: `${i * 40}ms` }"
      >
        <div class="flex items-start justify-between gap-2">
          <div
            :class="[
              'liquid-stat-icon liquid-stat-icon--sm',
              t.count ? 'liquid-stat-icon--active' : '',
            ]"
          >
            <ConsoleIcon :name="t.icon" size="sm" />
          </div>
          <span v-if="t.count" class="mt-1 h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-primary-500" />
        </div>
        <p :class="['liquid-stat-value mt-3', t.count ? 'text-primary-500' : 'text-gray-300']">{{ t.count }}</p>
        <p class="liquid-stat-label mt-1">{{ t.text }}</p>
        <p class="mt-1 text-[11px] text-gray-400 opacity-0 transition-opacity group-hover:opacity-100">查看 →</p>
      </RouterLink>
    </div>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_1.5fr]">
      <div class="card">
        <h2 class="card-title">
          <ConsoleIcon name="server" size="sm" class="card-title-icon" />
          底层网关 sub2api
        </h2>
        <div v-if="status">
          <div class="mb-4 flex items-center gap-3 rounded-xl border border-white/50 bg-white/60 px-4 py-3">
            <span class="relative flex h-3 w-3">
              <span
                :class="[
                  'absolute inline-flex h-full w-full animate-ping rounded-full opacity-60',
                  status.guard_state === 'normal' ? 'bg-emerald-400' : 'bg-amber-400',
                ]"
              />
              <span
                :class="[
                  'relative inline-flex h-3 w-3 rounded-full',
                  status.guard_state === 'normal' ? 'bg-emerald-600' : 'bg-amber-500',
                ]"
              />
            </span>
            <div>
              <div class="text-sm font-semibold text-gray-900">
                {{ status.guard_state === 'normal' ? '兼容自检正常' : '安全模式：写入已暂停' }}
              </div>
              <div v-if="status.reason" class="text-xs text-gray-500">{{ status.reason }}</div>
            </div>
          </div>
          <dl class="kv">
            <dt>版本</dt>
            <dd class="num">{{ status.sub2api_version ?? '—' }}</dd>
            <dt>检查时间</dt>
            <dd class="num">{{ dateTime(status.checked_at) }}</dd>
            <dt>出站队列</dt>
            <dd class="num">{{ Object.entries(status.ops_by_status).map(([k, v]) => `${k} ${v}`).join(' · ') || '空' }}</dd>
            <dt>入账游标</dt>
            <dd class="num">{{ status.ingest_cursor }}</dd>
          </dl>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3 md:grid-cols-3">
        <div
          v-for="(c, i) in ledgerCards"
          :key="c.label"
          class="liquid-stat"
          :style="{ animationDelay: `${(i + 7) * 40}ms` }"
        >
          <div class="flex items-start gap-3">
            <div
              :class="[
                'liquid-stat-icon',
                c.tone === 'success' && 'liquid-stat-icon--success',
                c.tone === 'warning' && 'liquid-stat-icon--warning',
              ]"
            >
              <ConsoleIcon :name="c.icon" size="md" />
            </div>
            <div class="min-w-0 flex-1">
              <p class="liquid-stat-label">{{ c.label }}</p>
              <p
                :class="[
                  'liquid-stat-value text-[1.35rem]',
                  c.tone === 'success' && 'text-emerald-600',
                  c.tone === 'warning' && 'text-amber-600',
                ]"
              >
                {{ c.value }}
              </p>
              <p v-if="c.hint" class="mt-1 truncate text-[11px] text-gray-400" :title="c.hint">{{ c.hint }}</p>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="quality" class="card">
      <h2 class="card-title">
        <ConsoleIcon name="activity" size="sm" class="card-title-icon" />
        上游质量
      </h2>
      <div class="overflow-x-auto rounded-xl border border-white/40">
        <table class="table">
          <thead>
            <tr>
              <th>平台</th>
              <th>健康分布</th>
              <th>等级分布</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!quality.platforms?.length">
              <td colspan="3" class="!py-8 text-center text-xs text-gray-400">暂无上游</td>
            </tr>
            <tr v-for="p in quality.platforms ?? []" :key="p.platform">
              <td class="font-medium text-gray-900">{{ label('platform', p.platform).text }}</td>
              <td>{{ distribution('health', p.health) }}</td>
              <td>{{ distribution('grade', p.grades) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
