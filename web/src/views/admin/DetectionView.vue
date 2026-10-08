<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Policy } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { label } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const toast = useToast()
const policies = useLoader(() => admin.policies(), [] as Policy[] | null)
const { busy, run } = useAction()
const editing = ref<Policy | null>(null)
const form = reactive({
  enabled: false,
  scheduled: false,
  interval: 360,
  jitter: 30,
  suites: '',
  models: '',
  budget: 1,
  items: 12,
  h0: 0.9,
  h1: 0.7,
  alpha: 0.05,
  beta: 0.1,
  autoPause: true,
  grade: 'B',
})
const newAuth = reactive({ platform: '', authType: '' })
const override = reactive({ upstream: '', mode: 'on', scheduled: '', expires: '', reason: '' })
onMounted(policies.load)

function edit(p: Policy) {
  editing.value = p
  Object.assign(form, {
    enabled: p.enabled,
    scheduled: p.scheduled_enabled,
    interval: p.interval_minutes,
    jitter: p.jitter_minutes,
    suites: p.suites.join(', '),
    models: p.models.join(', '),
    budget: p.daily_budget_usd,
    items: p.items_per_run,
    h0: p.thresholds.h0,
    h1: p.thresholds.h1,
    alpha: p.thresholds.alpha,
    beta: p.thresholds.beta,
    autoPause: p.auto_pause,
    grade: p.default_grade,
  })
}

const split = (s: string) => s.split(/[,\s]+/).filter(Boolean)

async function save() {
  const p = editing.value
  if (!p) return
  const body = {
    enabled: form.enabled,
    scheduled_enabled: form.enabled && form.scheduled,
    interval_minutes: Number(form.interval),
    jitter_minutes: Number(form.jitter),
    suites: split(form.suites),
    models: split(form.models),
    daily_budget_usd: Number(form.budget),
    items_per_run: Number(form.items),
    thresholds: { h0: Number(form.h0), h1: Number(form.h1), alpha: Number(form.alpha), beta: Number(form.beta) },
    auto_pause: form.autoPause,
    default_grade: form.grade,
  }
  if (await run(() => admin.upsertPolicy(p.platform, p.auth_type, body), '策略已保存，下一轮生效')) {
    editing.value = null
    policies.load()
  }
}

async function quick(p: Policy, field: 'enabled' | 'scheduled_enabled') {
  const body: Record<string, boolean> = { [field]: !p[field] }
  if (field === 'enabled' && p.enabled) body.scheduled_enabled = false
  if (field === 'scheduled_enabled' && !p.enabled) {
    toast.error('需要先开启检测')
    return
  }
  if (await run(() => admin.upsertPolicy(p.platform, p.auth_type, body), '已更新')) policies.load()
}

async function createAuthPolicy() {
  if (!newAuth.platform || !newAuth.authType) return
  if (await run(() => admin.upsertPolicy(newAuth.platform, newAuth.authType, {}), '已按厂商策略创建接入方式级策略')) policies.load()
}

async function setOverride() {
  const id = Number(override.upstream)
  if (!id) {
    toast.error('请输入上游 ID')
    return
  }
  const body: Record<string, unknown> = { enabled: override.mode === 'on', reason: override.reason }
  if (override.scheduled) body.scheduled_enabled = override.scheduled === 'on'
  if (override.expires) body.expires_at = new Date(override.expires).toISOString()
  await run(() => admin.setOverride(id, body), '覆盖已设置')
}
async function clearOverride() {
  const id = Number(override.upstream)
  if (id) await run(() => admin.deleteOverride(id), '覆盖已删除')
}
async function probe() {
  const id = Number(override.upstream)
  if (id) await run(() => admin.probe(id), '已排队检测')
}
</script>

<template>
  <div>
    <PageHeader title="检测策略" description="首次接入的上游始终执行上架检测；这里的开关控制之后的检测：是否检测（恢复复测、追加检测、手动复测），是否定时检测。" />
    <div class="card">
      <DataTable
        :columns="[
          { key: 'platform', label: '厂商' },
          { key: 'auth_type', label: '接入方式' },
          { key: 'enabled', label: '是否检测' },
          { key: 'scheduled_enabled', label: '是否定时检测' },
          { key: 'interval', label: '周期' },
          { key: 'models', label: '模型' },
          { key: 'items_per_run', label: '每次题量', align: 'right' },
          { key: 'daily_budget_usd', label: '每日预算', align: 'right' },
          { key: 'updated_at', label: '更新时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="policies.data.value"
        :loading="policies.loading.value"
        row-key="id"
      >
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-auth_type="{ row }">{{ row.auth_type ? label('authType', row.auth_type).text : '全部' }}</template>
        <template #cell-enabled="{ row }">
          <button :class="['btn-sm rounded px-2', row.enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500']" :disabled="busy" @click="quick(row, 'enabled')">
            {{ row.enabled ? '开' : '关' }}
          </button>
        </template>
        <template #cell-scheduled_enabled="{ row }">
          <button :class="['btn-sm rounded px-2', row.scheduled_enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500']" :disabled="busy" @click="quick(row, 'scheduled_enabled')">
            {{ row.scheduled_enabled ? '开' : '关' }}
          </button>
        </template>
        <template #cell-interval="{ row }">{{ row.interval_minutes }} 分钟 ± {{ row.jitter_minutes }}</template>
        <template #cell-models="{ row }">{{ row.models.join(', ') || '—' }}</template>
        <template #cell-daily_budget_usd="{ row }">{{ row.daily_budget_usd }}</template>
        <template #cell-updated_at="{ row }">{{ dateTime(row.updated_at) }}</template>
        <template #cell-actions="{ row }"><button class="btn-ghost btn-sm" @click="edit(row)">编辑</button></template>
      </DataTable>
      <div class="mt-4 flex flex-wrap items-end gap-2 border-t border-slate-100 pt-4">
        <div>
          <label class="label" for="np">新建接入方式级策略：厂商</label>
          <input id="np" v-model="newAuth.platform" class="input w-40" placeholder="anthropic" />
        </div>
        <div>
          <label class="label" for="na">接入方式</label>
          <input id="na" v-model="newAuth.authType" class="input w-40" placeholder="setup-token" />
        </div>
        <button class="btn-secondary" :disabled="busy" @click="createAuthPolicy">创建</button>
      </div>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">单个上游</h2>
      <p class="mb-3 text-sm text-slate-500">对某个上游强制开启或关闭检测（例如重点观察可疑账号），可设到期时间；也可以立即检测一次（不受开关限制）。</p>
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <label class="label" for="ou">上游 ID</label>
          <input id="ou" v-model="override.upstream" class="input w-28" inputmode="numeric" />
        </div>
        <div>
          <label class="label" for="om">检测</label>
          <select id="om" v-model="override.mode" class="input w-28">
            <option value="on">强制开启</option>
            <option value="off">强制关闭</option>
          </select>
        </div>
        <div>
          <label class="label" for="os">定时检测</label>
          <select id="os" v-model="override.scheduled" class="input w-28">
            <option value="">跟随策略</option>
            <option value="on">开启</option>
            <option value="off">关闭</option>
          </select>
        </div>
        <div>
          <label class="label" for="oe">到期</label>
          <input id="oe" v-model="override.expires" class="input" type="datetime-local" />
        </div>
        <div>
          <label class="label" for="or">原因</label>
          <input id="or" v-model="override.reason" class="input w-48" />
        </div>
        <button class="btn-secondary" :disabled="busy" @click="setOverride">设置覆盖</button>
        <button class="btn-ghost" :disabled="busy" @click="clearOverride">删除覆盖</button>
        <button class="btn-primary" :disabled="busy" @click="probe">立即检测</button>
      </div>
    </div>

    <ModalDialog :open="!!editing" :title="`编辑策略：${label('platform', editing?.platform).text} ${editing?.auth_type || ''}`" wide @update:open="(v: boolean) => !v && (editing = null)">
      <div class="flex gap-6 text-sm">
        <label class="flex items-center gap-2"><input v-model="form.enabled" type="checkbox" /> 是否检测</label>
        <label class="flex items-center gap-2"><input v-model="form.scheduled" type="checkbox" :disabled="!form.enabled" /> 是否定时检测</label>
        <label class="flex items-center gap-2"><input v-model="form.autoPause" type="checkbox" /> 确认降智后自动停止调度</label>
      </div>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div>
          <label class="label" for="pi">周期（分钟）</label>
          <input id="pi" v-model.number="form.interval" class="input" type="number" min="10" />
        </div>
        <div>
          <label class="label" for="pj">随机抖动（分钟）</label>
          <input id="pj" v-model.number="form.jitter" class="input" type="number" min="0" />
        </div>
        <div>
          <label class="label" for="pn2">每次题量</label>
          <input id="pn2" v-model.number="form.items" class="input" type="number" min="1" max="200" />
        </div>
        <div>
          <label class="label" for="pb">每日预算</label>
          <input id="pb" v-model.number="form.budget" class="input" type="number" min="0" step="0.1" />
        </div>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="label" for="pm">模型（逗号分隔，多个时轮换）</label>
          <input id="pm" v-model="form.models" class="input" />
        </div>
        <div>
          <label class="label" for="ps">套件</label>
          <input id="ps" v-model="form.suites" class="input" />
          <p class="hint">availability, latency, iq, reasoning, long_context</p>
        </div>
      </div>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-5">
        <div>
          <label class="label" for="h0">正常线 h0</label>
          <input id="h0" v-model.number="form.h0" class="input" type="number" step="0.01" />
        </div>
        <div>
          <label class="label" for="h1">降智线 h1</label>
          <input id="h1" v-model.number="form.h1" class="input" type="number" step="0.01" />
        </div>
        <div>
          <label class="label" for="pa">α（误判）</label>
          <input id="pa" v-model.number="form.alpha" class="input" type="number" step="0.01" />
        </div>
        <div>
          <label class="label" for="pbeta">β（漏判）</label>
          <input id="pbeta" v-model.number="form.beta" class="input" type="number" step="0.01" />
        </div>
        <div>
          <label class="label" for="pg">默认等级</label>
          <select id="pg" v-model="form.grade" class="input">
            <option v-for="g in ['S', 'A', 'B', 'C', 'D']" :key="g" :value="g">{{ g }}</option>
          </select>
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="editing = null">取消</button>
        <button class="btn-primary" :disabled="busy" @click="save">保存</button>
      </template>
    </ModalDialog>
  </div>
</template>
