<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { seller } from '@/api'
import type { Earnings, SettlementLine, Withdrawal } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'

import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime, money, parseAmount } from '@/utils/format'

const auth = useAuth()
const cfg = useConfig()
const toast = useToast()
const earnings = useLoader(() => seller.earnings(), null as Earnings | null)
const lines = useLoader(() => seller.lines({ limit: 100 }), [] as SettlementLine[] | null)
const withdrawals = useLoader(() => seller.withdrawals(), [] as Withdrawal[] | null)
const { busy, run } = useAction()
const showWithdraw = ref(false)
const form = ref({ amount: '', channel: 'alipay', account: '' })

function reload() {
  earnings.load()
  lines.load()
  withdrawals.load()
}
onMounted(() => {
  reload()
  cfg.ensure()
})

async function withdraw() {
  const a = parseAmount(form.value.amount)
  if (a === null) {
    toast.error('请输入正确的金额（最多两位小数）')
    return
  }
  if (await run(() => seller.withdraw(a, form.value.channel, form.value.account), '提现申请已提交')) {
    showWithdraw.value = false
    form.value.amount = ''
    reload()
  }
}
</script>

<template>
  <div>
    <PageHeader :title="'收益与提现'" :description="`收益按小时结算，冻结 ${cfg.config.freeze_days} 天后可提现（1 个收益单位 = 1 元）。平台检测消耗的额度同样计入收益。`">
      <RefreshButton :loading="earnings.loading.value || lines.loading.value || withdrawals.loading.value" @click="reload" />
      <button type="button" class="xm-btn xm-btn-primary" @click="showWithdraw = true">申请提现</button>
    </PageHeader>
    <div class="xm-stats" style="grid-template-columns: repeat(3, 1fr)">
      <div class="xm-stat"><b>可提现</b><strong>{{ money(earnings.data.value?.available) }}</strong></div>
      <div class="xm-stat"><b>待结算（冻结中）</b><strong>{{ money(earnings.data.value?.pending) }}</strong></div>
      <div class="xm-stat"><b>提现中</b><strong>{{ money(earnings.data.value?.withdrawing) }}</strong></div>
      <div class="xm-stat"><b>今日</b><strong>{{ money(earnings.data.value?.today) }}</strong></div>
      <div class="xm-stat"><b>近 7 天</b><strong>{{ money(earnings.data.value?.last_7_days) }}</strong></div>
      <div class="xm-stat"><b>累计</b><strong>{{ money(earnings.data.value?.total) }}</strong></div>
    </div>

    <div class="card mt-5">
      <h2 class="mb-3 text-[15px] font-extrabold text-[#1f1f1f]">提现记录</h2>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'channel', label: '方式' },
          { key: 'status', label: '状态' },
          { key: 'review_reason', label: '说明' },
          { key: 'created_at', label: '申请时间' },
          { key: 'paid_at', label: '打款时间' },
        ]"
        :rows="withdrawals.data.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-channel="{ row }">{{ ({ alipay: '支付宝', bank: '银行卡', wechat: '微信' } as Record<string, string>)[row.channel] ?? row.channel }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="withdrawalStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-paid_at="{ row }">{{ dateTime(row.paid_at) }}</template>
      </DataTable>
    </div>

    <div class="card mt-5">
      <h2 class="mb-3 text-[15px] font-extrabold text-[#1f1f1f]">结算明细（按上游 × 小时 × 渠道）</h2>
      <DataTable
        :columns="[
          { key: 'hour', label: '时段' },
          { key: 'upstream_id', label: '上游' },
          { key: 'channel', label: '渠道' },
          { key: 'requests', label: '请求数', align: 'right' },
          { key: 'official_cost', label: '官方价', align: 'right' },
          { key: 'payout', label: '收益', align: 'right' },
          { key: 'unfrozen', label: '已解冻', align: 'right' },
        ]"
        :rows="lines.data.value"
        :loading="lines.loading.value"
      >
        <template #cell-hour="{ row }">{{ dateTime(row.hour) }}</template>
        <template #cell-upstream_id="{ row }">#{{ row.upstream_id }}</template>
        <template #cell-channel="{ row }"><StatusBadge kind="channel" :value="row.channel" /></template>
        <template #cell-official_cost="{ row }">{{ money(row.official_cost) }}</template>
        <template #cell-payout="{ row }">{{ money(row.payout) }}</template>
        <template #cell-unfrozen="{ row }">{{ money(row.unfrozen) }}</template>
      </DataTable>
    </div>

    <ModalDialog v-model:open="showWithdraw" title="申请提现">
      <div v-if="auth.user?.kyc_status !== 'verified'" class="callout callout-warn">
        提现前需要完成实名认证。<RouterLink to="/me?from=earnings" class="link">去认证 →</RouterLink>
      </div>
      <p class="text-sm text-slate-600">可提现 {{ money(earnings.data.value?.available) }}，最低 {{ money(cfg.config.withdraw_min) }}。</p>
      <div>
        <label class="label" for="wa">金额（元）</label>
        <input id="wa" v-model="form.amount" class="input" inputmode="decimal" />
      </div>
      <div>
        <label class="label" for="wc">收款方式</label>
        <select id="wc" v-model="form.channel" class="input">
          <option value="alipay">支付宝</option>
          <option value="bank">银行卡</option>
          <option value="wechat">微信</option>
        </select>
      </div>
      <div>
        <label class="label" for="wacc">收款账号</label>
        <input id="wacc" v-model="form.account" class="input" placeholder="账号与户名" />
        <p class="hint">加密存储，仅用于打款。</p>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showWithdraw = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="withdraw">提交</button>
      </template>
    </ModalDialog>
  </div>
</template>
