<script setup lang="ts">
// 管理员查看某个上游的检测明细：最近的检测记录，以及选中检测的逐题结果（题目、是否通过、回答片段）。
import { computed, ref, watch } from 'vue'
import { admin } from '@/api'
import type { ProbeRun, ProbeRunDetail, Upstream } from '@/api/types'
import ModalDialog from '@/components/ModalDialog.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { dateTime, ms } from '@/utils/format'
import { upstreamName } from '@/utils/labels'

const props = defineProps<{ upstream: Upstream | null }>()
const open = defineModel<boolean>('open', { default: false })

const runs = ref<ProbeRun[]>([])
const detail = ref<ProbeRunDetail | null>(null)
const loading = ref(false)
const runStatus: Record<string, string> = {
  queued: '排队中',
  running: '检测中',
  third_party: '等待第三方',
  done: '完成',
  failed: '失败',
  skipped_budget: '超预算跳过',
  skipped_disabled: '未开启跳过',
}

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

// 按题目合并被测上游与基线账号的结果
const rows = computed(() => {
  const d = detail.value
  if (!d) return []
  const ref = new Map((d.reference_results ?? []).map((x) => [x.item_id, x]))
  return d.results.map((r) => ({ ...r, ref: r.item_id !== undefined ? ref.get(r.item_id) : undefined }))
})

function passText(p: boolean | null | undefined) {
  return p === null || p === undefined ? '—' : p ? '通过' : '未通过'
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

      <template v-if="detail">
        <div class="flex flex-wrap items-center gap-2">
          <StatusBadge v-if="detail.run.summary?.verdict" kind="verdict" :value="detail.run.summary.verdict" />
          <StatusBadge kind="trigger" :value="detail.run.trigger" />
          <span class="text-slate-500">模型 {{ detail.run.model }}</span>
          <span class="text-slate-500">{{ runStatus[detail.run.status] ?? detail.run.status }}</span>
          <span class="text-slate-500">共 {{ detail.run.items }} 题，通过 {{ detail.run.passed }} 题</span>
          <span v-if="detail.reference" class="text-slate-500">
            基线 #{{ detail.reference.upstream_id }}：通过 {{ detail.reference.passed }} / {{ detail.reference.items }}
          </span>
        </div>
        <p v-if="detail.run.summary?.error" class="text-red-600">{{ detail.run.summary.error }}</p>

        <div class="max-h-[60vh] overflow-auto">
          <table class="w-full text-left text-xs">
            <thead class="sticky top-0 bg-white text-slate-500">
              <tr>
                <th class="p-2">题目</th>
                <th class="p-2">套件 / 类别</th>
                <th class="p-2">结果</th>
                <th v-if="detail.reference" class="p-2">基线</th>
                <th class="p-2">首字 / 总时长</th>
                <th class="p-2">输出 / 推理 token</th>
                <th class="p-2">回答片段</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in rows" :key="i" class="border-t border-slate-100 align-top">
                <td class="p-2">
                  <div class="font-medium">#{{ r.item_id ?? '—' }}</div>
                  <div class="max-w-xs truncate text-slate-500" :title="r.prompt">{{ r.prompt }}</div>
                  <div v-if="r.models.length" class="text-slate-500">限 {{ r.models.join(', ') }}</div>
                </td>
                <td class="p-2">{{ r.suite }}<div class="text-slate-500">{{ r.category }}</div></td>
                <td class="p-2" :class="r.passed ? 'text-emerald-700' : 'text-red-600'">
                  {{ passText(r.passed) }}
                  <div v-if="r.error_class" class="text-slate-500">{{ r.error_class }}{{ r.http_status ? ` · ${r.http_status}` : '' }}</div>
                </td>
                <td v-if="detail.reference" class="p-2" :class="r.ref?.passed ? 'text-emerald-700' : 'text-red-600'">
                  {{ passText(r.ref?.passed) }}
                </td>
                <td class="p-2 whitespace-nowrap">{{ ms(r.ttft_ms) }} / {{ ms(r.duration_ms) }}</td>
                <td class="p-2 whitespace-nowrap">{{ r.output_tokens }} / {{ r.reasoning_tokens }}</td>
                <td class="p-2">
                  <div class="max-w-sm whitespace-pre-wrap break-words text-slate-600">{{ r.excerpt || '—' }}</div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="hint">回答片段只保存前 500 个字符。</p>
      </template>
    </div>
    <template #footer>
      <button class="btn-primary" @click="open = false">关闭</button>
    </template>
  </ModalDialog>
</template>
