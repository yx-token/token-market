<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { PlatformLedger, QualityOverview, Sub2APIStatus } from '@/api/types'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import { dateTime, money } from '@/utils/format'
import { distribution, label } from '@/utils/labels'

const toast = useToast()
const status = ref<Sub2APIStatus | null>(null)
const ledger = ref<PlatformLedger | null>(null)
const quality = ref<QualityOverview | null>(null)
const todo = ref<{ text: string; count: number; to: string }[]>([])

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
      { text: '待确认充值', count: rc?.length ?? 0, to: '/admin/funds' },
      { text: '待打款退款', count: rf?.length ?? 0, to: '/admin/funds' },
      { text: '待审核提现', count: wd?.length ?? 0, to: '/admin/funds' },
      { text: '待审核实名', count: kyc?.length ?? 0, to: '/admin/users' },
      { text: '待处理申诉', count: ap?.length ?? 0, to: '/admin/quality' },
      { text: '计费异常', count: an?.length ?? 0, to: '/admin/billing' },
      { text: '风险事件', count: risk?.length ?? 0, to: '/admin/risk' },
    ]
  } catch (e) {
    toast.error(e)
  }
})
</script>

<template>
  <div>
    <PageHeader title="运营概览" description="待办事项、底层网关状态、平台资金与上游质量一览。" />

    <div class="mb-6 grid grid-cols-2 gap-px overflow-hidden border border-ink-900 bg-ink-900 sm:grid-cols-4 lg:grid-cols-7">
      <RouterLink
        v-for="(t, i) in todo"
        :key="t.text"
        :to="t.to"
        :class="['group relative flex flex-col justify-between gap-6 p-4 transition-colors', t.count ? 'bg-white hover:bg-brand-50' : 'bg-paper hover:bg-white']"
      >
        <div class="flex items-start justify-between">
          <span class="font-mono text-[10px] text-ink-400">{{ String(i + 1).padStart(2, '0') }}</span>
          <span v-if="t.count" class="h-1.5 w-1.5 animate-blink rounded-full bg-brand-500" />
        </div>
        <div>
          <div :class="['num text-[32px] font-medium leading-none', t.count ? 'text-brand-600' : 'text-ink-300']">{{ t.count }}</div>
          <div class="mt-2 text-[12.5px] text-ink-700">{{ t.text }}<span class="ml-1 opacity-0 transition-opacity group-hover:opacity-100">→</span></div>
        </div>
      </RouterLink>
    </div>

    <div class="mb-6 grid grid-cols-1 gap-5 lg:grid-cols-[1fr_1.4fr]">
      <div class="card">
        <h2 class="card-title">底层网关 sub2api</h2>
        <div v-if="status">
          <div class="mb-4 flex items-center gap-3 border border-ink-200 bg-paper px-4 py-3">
            <span :class="['relative flex h-3 w-3', status.guard_state === 'normal' ? '' : '']">
              <span :class="['absolute inline-flex h-full w-full animate-ping rounded-full opacity-60', status.guard_state === 'normal' ? 'bg-emerald-400' : 'bg-brand-400']" />
              <span :class="['relative inline-flex h-3 w-3 rounded-full', status.guard_state === 'normal' ? 'bg-emerald-600' : 'bg-brand-500']" />
            </span>
            <div>
              <div class="font-medium">{{ status.guard_state === 'normal' ? '兼容自检正常' : '安全模式：写入已暂停' }}</div>
              <div v-if="status.reason" class="text-xs text-ink-500">{{ status.reason }}</div>
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
      <div v-if="ledger" class="grid grid-cols-2 gap-4 md:grid-cols-3">
        <StatCard label="平台现金" :value="money(ledger.cash)" hint="充值 − 退款 − 已打款提现 − 调整" />
        <StatCard label="平台收入" :value="money(ledger.revenue)" hint="买家支付 − 卖家收益" accent />
        <StatCard label="探测成本" :value="money(ledger.probe_cost)" />
        <StatCard label="人工调整" :value="money(ledger.adjustment)" />
        <StatCard label="买家余额合计" :value="money(ledger.buyer_balances)" />
        <StatCard label="应付卖家合计" :value="money(ledger.seller_payable)" />
      </div>
    </div>

    <div v-if="quality" class="card">
      <h2 class="card-title">上游质量</h2>
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
            <td colspan="3" class="!py-8 text-center font-mono text-xs text-ink-400">— 暂无上游 —</td>
          </tr>
          <tr v-for="p in quality.platforms ?? []" :key="p.platform">
            <td class="font-medium text-ink-900">{{ label('platform', p.platform).text }}</td>
            <td>{{ distribution('health', p.health) }}</td>
            <td>{{ distribution('grade', p.grades) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
