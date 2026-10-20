<script setup lang="ts">
// 管理员查看某个上游的检测明细：最近的检测记录，以及选中检测的完整结果。
// 也会被卖家/上游侧复用——标准答案只在显式传 show-answer 时展示。
import { ref, watch } from 'vue'
import { admin } from '@/api'
import type { ProbeRun, ProbeRunDetail as Detail, Upstream } from '@/api/types'
import ModalDialog from '@/components/ModalDialog.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import { dateTime } from '@/utils/format'
import { upstreamName } from '@/utils/labels'

const props = withDefaults(defineProps<{ upstream: Upstream | null; showAnswer?: boolean }>(), { showAnswer: false })
const open = defineModel<boolean>('open', { default: false })

const runs = ref<ProbeRun[]>([])
const detail = ref<Detail | null>(null)
const detailLoading = ref(false)
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
  detailLoading.value = true
  try {
    detail.value = await admin.probeRun(r.id)
  } finally {
    detailLoading.value = false
  }
}
</script>

<template>
  <ModalDialog v-model:open="open" :title="upstream ? `检测明细：${upstreamName(upstream)}` : '检测明细'" size="xl">
    <div v-if="loading" class="space-y-4 py-2" aria-busy="true" aria-label="正在加载检测记录">
      <div class="flex flex-wrap gap-2">
        <span v-for="i in 3" :key="i" class="h-7 w-36 animate-pulse rounded-lg bg-slate-100" />
      </div>
      <div class="h-56 animate-pulse rounded bg-slate-100" />
      <p class="text-xs text-slate-400">加载中…</p>
    </div>
    <div v-else-if="!runs.length" class="flex flex-col items-center gap-2 py-10 text-center">
      <span class="text-2xl" aria-hidden="true">🧪</span>
      <p class="text-sm text-slate-600">这个上游还没有检测记录。</p>
      <p class="text-xs text-slate-400">上架或手动触发一次检测后，这里会显示逐题结果。</p>
    </div>
    <div v-else class="space-y-4 text-sm">
      <div class="flex flex-wrap gap-2" role="group" aria-label="选择检测批次">
        <button
          v-for="r in runs"
          :key="r.id"
          type="button"
          class="btn-sm"
          :class="detail?.run.id === r.id ? 'btn-primary' : 'btn-secondary'"
          :aria-pressed="detail?.run.id === r.id"
          @click="select(r)"
        >
          <span class="num">#{{ r.id }}</span> · {{ dateTime(r.queued_at) }}
        </button>
      </div>
      <ProbeRunDetail v-if="detail" :detail="detail" :show-answer="showAnswer" />
      <div v-else-if="detailLoading" class="space-y-3 py-2" aria-busy="true" aria-label="正在加载检测明细">
        <div class="h-4 w-64 animate-pulse rounded bg-slate-100" />
        <div class="h-40 animate-pulse rounded bg-slate-100" />
        <p class="text-xs text-slate-400">加载中…</p>
      </div>
      <div v-else class="py-6 text-center text-xs text-slate-500">这批检测的明细加载失败，请重新选择。</div>
    </div>
    <template #footer>
      <button class="btn-primary" @click="open = false">关闭</button>
    </template>
  </ModalDialog>
</template>