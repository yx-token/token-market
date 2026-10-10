<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { seller } from '@/api'
import type { Upstream, VendorView } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { authTypeText, label } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const upstreams = useLoader(() => seller.upstreams(), [] as Upstream[] | null)
const vendors = ref<VendorView[]>([])
const wizard = ref(false)
const resume = ref<Upstream | null>(null)

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
</script>

<template>
  <div>
    <PageHeader title="我的上游" description="一个上游是一个官方订阅账号或一个 Coding Plan key。首次接入需要通过上架检测才能挂单。">
      <RefreshButton :loading="upstreams.loading.value" @click="upstreams.load()" />
      <RouterLink to="/seller/publish" class="xm-btn xm-btn-dark">＋ 发布供给</RouterLink>
      <button type="button" class="xm-btn xm-btn-ghost" @click="openNew">仅接入上游</button>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '上游' },
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
        :rows="upstreams.data.value"
        :loading="upstreams.loading.value"
        row-key="id"
        empty="还没有上游，点击右上角接入"
      >
        <template #cell-id="{ row }">
          <span class="font-semibold">#{{ row.id }}</span>
          <span v-if="row.is_reference" class="ml-1 text-xs text-violet-600">基线</span>
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
        <template #cell-grade="{ row }">{{ row.grade ?? '—' }}</template>
        <template #cell-tier="{ row }">{{ row.tier ?? '—' }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1.5">
            <RouterLink :to="`/seller/upstreams/${row.id}`" class="xm-btn xm-btn-primary xm-btn-sm">详情</RouterLink>
            <button
              v-if="['draft', 'authorizing', 'failed'].includes(row.status)"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm"
              @click="openResume(row)"
            >继续授权</button>
          </div>
        </template>
      </DataTable>
    </div>
    <OnboardingWizard v-model:open="wizard" :vendors="vendors" :resume="resume" @done="upstreams.load()" />
  </div>
</template>
