<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { seller } from '@/api'
import type { Upstream, VendorView } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { label } from '@/utils/labels'
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
      <button class="btn-primary" @click="openNew">接入上游</button>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '上游' },
          { key: 'platform', label: '厂商' },
          { key: 'auth_type', label: '接入方式' },
          { key: 'status', label: '状态' },
          { key: 'health', label: '健康' },
          { key: 'grade', label: '等级' },
          { key: 'tier', label: '档位' },
          { key: 'created_at', label: '接入时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="upstreams.data.value"
        :loading="upstreams.loading.value"
        row-key="id"
        empty="还没有上游，点击右上角接入"
      >
        <template #cell-id="{ row }">
          <RouterLink :to="`/seller/upstreams/${row.id}`" class="link">#{{ row.id }}</RouterLink>
          <span v-if="row.is_reference" class="ml-1 text-xs text-violet-600">基线</span>
        </template>
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-auth_type="{ row }">{{ label('authType', row.auth_type).text }}</template>
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
          <button v-if="['draft', 'authorizing', 'failed'].includes(row.status)" class="btn-ghost btn-sm" @click="openResume(row)">继续授权</button>
          <RouterLink v-else :to="`/seller/upstreams/${row.id}`" class="btn-ghost btn-sm">管理</RouterLink>
        </template>
      </DataTable>
    </div>
    <OnboardingWizard v-model:open="wizard" :vendors="vendors" :resume="resume" @done="upstreams.load()" />
  </div>
</template>
