<script setup lang="ts">
// 管理员查看某个上游的检测明细：最近的检测记录，以及选中检测的完整结果。
import { ref, watch } from 'vue'
import { admin } from '@/api'
import type { ProbeRun, ProbeRunDetail as Detail, Upstream } from '@/api/types'
import ModalDialog from '@/components/ModalDialog.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import { dateTime } from '@/utils/format'
import { upstreamName } from '@/utils/labels'

const props = defineProps<{ upstream: Upstream | null }>()
const open = defineModel<boolean>('open', { default: false })

const runs = ref<ProbeRun[]>([])
const detail = ref<Detail | null>(null)
const loading = ref(false)

watch(open, async (v) => {
  if (!v || !props.upstream) return
  runs.value = []
  detail.value = null
  loading.value = true
  try {
    runs.value = (await admin.probeRuns(props.upstream.id)) ?? []
    if (runs.value.length) await select(runs.value[0])
  } finally {
    loading.value = false
  }
})

async function select(r: ProbeRun) {
  detail.value = await admin.probeRun(r.id)
}
</script>

<template>
  <ModalDialog v-model:open="open" :title="upstream ? `检测明细：${upstreamName(upstream)}` : '检测明细'" wide>
    <div v-if="loading" class="text-sm text-slate-500">加载中…</div>
    <div v-else-if="!runs.length" class="text-sm text-slate-500">这个上游还没有检测记录。</div>
    <div v-else class="space-y-4 text-sm">
      <div class="flex flex-wrap gap-2">
        <button
          v-for="r in runs"
          :key="r.id"
          type="button"
          class="btn-sm"
          :class="detail?.run.id === r.id ? 'btn-primary' : 'btn-secondary'"
          @click="select(r)"
        >
          #{{ r.id }} · {{ dateTime(r.queued_at) }}
        </button>
      </div>
      <ProbeRunDetail v-if="detail" :detail="detail" />
    </div>
    <template #footer>
      <button class="btn-primary" @click="open = false">关闭</button>
    </template>
  </ModalDialog>
</template>
