<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { seller } from '@/api'
import type { Upstream, VendorView } from '@/api/types'
import { detectionText } from '@/api/market-detection'
import { sellerUntested } from '@/components/seller/listingGate'
import '@/styles/seller-center.css'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { authTypeText, label, upstreamName } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const upstreamsError = ref('')
const upstreams = useLoader(async () => {
  try {
    const rows = (await seller.upstreams()) ?? []
    upstreamsError.value = ''
    return rows
  } catch (error) {
    upstreamsError.value = '上游没有载入'
    throw error
  }
}, [] as Upstream[])
const vendors = ref<VendorView[]>([])
const wizard = ref(false)
const resume = ref<Upstream | null>(null)
type Bucket = 'all' | 'todo' | 'ready' | 'listed'
const bucket = ref<Bucket>('all')
const buckets: { id: Bucket; label: string }[] = [
  { id: 'all', label: '全部' },
  { id: 'todo', label: '待完成' },
  { id: 'ready', label: '可挂单' },
  { id: 'listed', label: '已挂单' },
]
const { busy, run } = useAction()
const visible = computed(() =>
  (upstreams.data.value ?? []).filter((u) => {
    if (bucket.value === 'todo') return ['draft', 'authorizing', 'failed', 'selecting_models', 'verifying', 'verify_failed'].includes(u.status)
    if (bucket.value === 'ready') return u.status === 'ready'
    if (bucket.value === 'listed') return u.status === 'listed'
    return true
  }),
)

let timer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  upstreams.load()
  vendors.value = (await seller.vendors().catch(() => null)) ?? []
  // 有上游在检测中时定期刷新
  timer = setInterval(() => {
    if (upstreams.data.value?.some((u) => u.status === 'verifying')) upstreams.load()
  }, 10000)
})
onUnmounted(() => timer && clearInterval(timer))

function openNew() {
  resume.value = null
  wizard.value = true
}
function openResume(u: Upstream) {
  resume.value = u
  wizard.value = true
}

async function detectNow(u: Upstream) {
  if (!confirm('现在发起质量检测？未检测也可以先挂寄售或额度包。检测会消耗该账号额度。')) return
  if (await run(() => seller.retryVerification(u.id), '已开始质量检测')) await upstreams.load()
}

async function removeUpstream(u: Upstream) {
  if (!confirm(`删除上游 ${upstreamName(u)}？关联商品会关闭，账号在进行中的请求结束后从平台移除。`)) return
  if (await run(() => seller.removeUpstream(u.id), '已删除上游')) await upstreams.load()
}
</script>

<template>
  <div class="seller-page">
    <PageHeader title="我的上游" description="一个上游是一个官方订阅账号或一个 Coding Plan key。模型保存且隐私检查通过后即可挂单，质量检测不是前置条件。">
      <RefreshButton :loading="upstreams.loading.value" @click="upstreams.load()" />
      <RouterLink to="/seller/publish" class="xm-btn xm-btn-dark">＋ 发布闲置TOKEN</RouterLink>
      <button type="button" class="xm-btn xm-btn-ghost" @click="openNew">接入上游</button>
    </PageHeader>
    <p class="seller-callout seller-callout-warn">未检测不能显示成通过，也不能进保障池或做平台回收。买家列表会标「未检测」。只有 gpt-6-astra 额外展示降智结果。</p>
    <div class="seller-filters" role="group" aria-label="按进度筛选">
      <button v-for="item in buckets" :key="item.id" type="button" class="seller-chip" :class="{ 'is-on': bucket === item.id }" @click="bucket = item.id">{{ item.label }}</button>
    </div>
    <div v-if="upstreamsError && !upstreams.loading.value" class="seller-empty">
      <strong>上游没有载入</strong>
      <p>请重试。空列表不是失败。</p>
      <button type="button" class="xm-btn xm-btn-primary" @click="upstreams.load()">重试</button>
    </div>
    <div v-else class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '账号' },
          { key: 'platform', label: '厂商' },
          { key: 'auth_type', label: '接入方式' },
          { key: 'proxy_id', label: '代理 IP' },
          { key: 'status', label: '状态' },
          { key: 'health', label: '健康' },
          { key: 'grade', label: '等级' },
          { key: 'tier', label: '档位' },
          { key: 'created_at', label: '接入时间' },
          { key: 'actions', label: '操作' },
        ]"
        :rows="visible"
        :loading="upstreams.loading.value"
        row-key="id"
        :empty="upstreams.data.value.length ? '这个筛选下没有上游' : '还没有上游，点击右上角接入或发布'"
      >
        <template #cell-id="{ row }">
          <span class="font-semibold">{{ upstreamName(row) }}</span>
          <span v-if="row.is_reference" class="ml-1 text-xs text-violet-600">基线</span>
          <div v-if="row.account_email" class="text-xs text-ink-500">#{{ row.id }}</div>
        </template>
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-auth_type="{ row }">{{ authTypeText(row.platform, row.auth_type) }}</template>
        <template #cell-proxy_id="{ row }">{{ row.proxy_id ? `代理 #${row.proxy_id}` : '—' }}</template>
        <template #cell-status="{ row }">
          <StatusBadge kind="upstreamStatus" :value="row.status" />
          <div v-if="row.failure_reason && ['verify_failed', 'failed', 'suspended'].includes(row.status)" class="mt-0.5 max-w-xs truncate text-xs text-red-600" :title="row.failure_reason">
            {{ row.failure_reason }}
          </div>
        </template>
        <template #cell-health="{ row }"><StatusBadge kind="health" :value="row.health" /></template>
        <template #cell-grade="{ row }">{{ sellerUntested(row) ? detectionText(row.detection_status) : (row.grade ?? '—') }}</template>
        <template #cell-tier="{ row }">{{ row.tier ?? '—' }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1.5">
            <RouterLink :to="`/seller/upstreams/${row.id}`" class="xm-btn xm-btn-primary xm-btn-sm">详情</RouterLink>
            <button
              v-if="['draft', 'authorizing', 'failed', 'selecting_models'].includes(row.status)"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm"
              @click="openResume(row)"
            >{{ row.status === 'selecting_models' ? '选择挂售模型' : '继续授权' }}</button>
            <RouterLink
              v-if="row.status === 'ready' || row.status === 'listed'"
              :to="`/seller/publish?upstream=${row.id}`"
              class="xm-btn xm-btn-ghost xm-btn-sm"
            >{{ sellerUntested(row) ? '未检测也可发布' : '发布闲置TOKEN' }}</RouterLink>
            <button
              v-if="row.status === 'ready' || row.status === 'verify_failed'"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm"
              :disabled="busy"
              @click="detectNow(row)"
            >发起质量检测</button>
            <button
              v-if="!row.is_reference && row.status !== 'removed'"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm text-red-600"
              :disabled="busy"
              @click="removeUpstream(row)"
            >删除</button>
          </div>
        </template>
      </DataTable>
    </div>
    <OnboardingWizard v-model:open="wizard" :vendors="vendors" :resume="resume" @done="upstreams.load()" />
  </div>
</template>
