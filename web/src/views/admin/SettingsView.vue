<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { Settings } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import InspectionSettingsPanel from '@/components/InspectionSettingsPanel.vue'

const toast = useToast()
const s = ref<Settings | null>(null)
const grades = ref('')
const models = ref('')
const { busy, run } = useAction()

const numberFields: { key: keyof Settings; label: string; hint: string; step?: string }[] = [
  { key: 'commission_rate', label: '寄售抽佣率 c', hint: '买家价 = 卖家倍率 × (1 + c)，例如 0.2', step: '0.01' },
  { key: 'freeze_days', label: '收益冻结天数', hint: '结算后多少天可提现' },
  { key: 'withdraw_min', label: '最低提现金额（元）', hint: '', step: '0.01' },
  { key: 'withdraw_auto_approve_max', label: '自动审核上限（元）', hint: '不超过该金额且无风险时自动通过', step: '0.01' },
  { key: 'manual_retest_daily_limit', label: '卖家每日手动复测次数', hint: '' },
  { key: 'quote_validity_days', label: '回收报价有效天数', hint: '' },
  { key: 'price_change_cooldown_minutes', label: '寄售改价冷却（分钟）', hint: '' },
  { key: 'shadow_concurrency', label: '买家默认并发', hint: '每个买家在 sub2api 中的并发上限' },
  { key: 'refund_lock_seconds', label: '退款等待（秒）', hint: '暂停 Key 后等待进行中请求结束的时间' },
  { key: 'new_seller_days', label: '新卖家期（天）', hint: '期内提现需人工审核' },
  { key: 'new_seller_direct_cap', label: '新卖家单一买家收益上限（元）', hint: '超过记风险事件', step: '0.01' },
  { key: 'scheduler_change_budget', label: '每轮调度最大改动账号数', hint: '' },
  { key: 'balance_mismatch_tolerance', label: '余额对账容差（元）', hint: '', step: '0.01' },
  { key: 'probe_request_timeout_seconds', label: '质量检测单次请求超时（秒）', hint: '每道题发给被测上游或基线账号的单次请求，超时按请求失败计；10–1800' },
  { key: 'probe_batch_timeout_minutes', label: '质量检测总时限（分钟）', hint: '一次检测整批题的总时长，超过记为检测失败（上架检测记为不通过，可重新检测）；不能短于单次请求超时' },
]


onMounted(load)
async function load() {
  try {
    s.value = await admin.settings()
    grades.value = Object.entries(s.value.grade_coefficients)
      .map(([k, v]) => `${k}=${v}`)
      .join(', ')
    models.value = (s.value.price_models ?? []).join(', ')
  } catch (e) {
    toast.error(e)
  }
}

async function save() {
  if (!s.value) return
  const coeff: Record<string, number> = {}
  for (const part of grades.value.split(',')) {
    const [k, v] = part.split('=').map((x) => x.trim())
    if (k && v !== undefined && !Number.isNaN(Number(v))) coeff[k] = Number(v)
  }
  const patch: Partial<Settings> = { ...s.value, grade_coefficients: coeff, price_models: models.value.split(/[,\s]+/).filter(Boolean) }
  for (const f of numberFields) (patch as Record<string, unknown>)[f.key] = Number(s.value[f.key])
  const res = await run(() => admin.updateSettings(patch), '已保存')
  if (res) s.value = res
}
</script>

<template>
  <div v-if="s">
    <PageHeader title="系统设置">
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </PageHeader>
    <div class="card grid grid-cols-1 gap-4 md:grid-cols-2">
      <div v-for="f in numberFields" :key="f.key">
        <label class="label" :for="f.key">{{ f.label }}</label>
        <input :id="f.key" v-model="(s as any)[f.key]" class="input" type="number" :step="f.step ?? '1'" min="0" />
        <p v-if="f.hint" class="hint">{{ f.hint }}</p>
      </div>
      <div>
        <label class="label" for="grades">等级系数</label>
        <input id="grades" v-model="grades" class="input" placeholder="S=1.2, A=1, B=0.85, C=0.7, D=0" />
        <p class="hint">回收报价 = 厂商基础倍率 × 等级系数（不超过默认分组上限）</p>
      </div>
      <div>
        <label class="label" for="models">市场展示价格的模型</label>
        <input id="models" v-model="models" class="input" />
        <p class="hint">价格取自 sub2api 内置价目表，查不到的模型不展示</p>
      </div>
      <div class="md:col-span-2">
        <label class="flex items-center gap-2 text-sm">
          <input v-model="s.require_seller_proxy" type="checkbox" /> 要求卖家提供代理 IP
        </label>
        <p class="hint">默认开启：卖家先配置自己的代理列表，再为每个账号选择一个空闲代理。关闭后，卖家也可以选择由平台自动关联专用代理；已有账号的绑定保持不变。</p>
      </div>
      <label class="flex items-center gap-2 text-sm">
        <input v-model="s.allow_no_proxy" type="checkbox" /> 平台没有可用代理时允许直接接入（仅开发环境；卖家必填要求仍生效）
      </label>
    </div>
    <InspectionSettingsPanel />
  </div>
</template>
