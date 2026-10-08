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
    <PageHeader title="概览" />
    <div class="mb-5 grid grid-cols-2 gap-3 md:grid-cols-7">
      <RouterLink v-for="t in todo" :key="t.text" :to="t.to" :class="['stat hover:border-brand-500', t.count ? 'border-amber-300 bg-amber-50' : '']">
        <div class="stat-label">{{ t.text }}</div>
        <div class="stat-value">{{ t.count }}</div>
      </RouterLink>
    </div>

    <div class="card mb-5">
      <h2 class="card-title">sub2api 集成状态</h2>
      <div v-if="status" class="grid grid-cols-1 gap-3 text-sm md:grid-cols-2">
        <div>
          兼容自检：
          <span :class="status.guard_state === 'normal' ? 'font-medium text-emerald-700' : 'font-medium text-red-700'">
            {{ status.guard_state === 'normal' ? '正常' : '安全模式（写入已暂停）' }}
          </span>
          <span v-if="status.reason" class="ml-2 text-slate-500">{{ status.reason }}</span>
        </div>
        <div>版本：{{ status.sub2api_version ?? '—' }}，检查时间 {{ dateTime(status.checked_at) }}</div>
        <div>出站队列：{{ Object.entries(status.ops_by_status).map(([k, v]) => `${k} ${v}`).join('，') || '空' }}</div>
        <div>用量入账游标：{{ status.ingest_cursor }}</div>
      </div>
    </div>

    <div v-if="ledger" class="mb-5 grid grid-cols-2 gap-4 md:grid-cols-6">
      <StatCard label="平台现金" :value="money(ledger.cash)" hint="充值 − 退款 − 已打款提现 − 调整" />
      <StatCard label="平台收入" :value="money(ledger.revenue)" hint="买家支付 − 卖家收益" />
      <StatCard label="探测成本" :value="money(ledger.probe_cost)" />
      <StatCard label="人工调整" :value="money(ledger.adjustment)" />
      <StatCard label="买家余额合计" :value="money(ledger.buyer_balances)" />
      <StatCard label="应付卖家合计" :value="money(ledger.seller_payable)" />
    </div>

    <div v-if="quality" class="card">
      <h2 class="card-title">质量概况</h2>
      <table class="table">
        <thead>
          <tr>
            <th>平台</th>
            <th>健康分布</th>
            <th>等级分布</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in quality.platforms ?? []" :key="p.platform">
            <td>{{ label('platform', p.platform).text }}</td>
            <td>{{ distribution('health', p.health) }}</td>
            <td>{{ distribution('grade', p.grades) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
