<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { InspectionSettings } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'

const toast = useToast()
const advanced = ref('')
const loaded = ref(false)
const { busy, run } = useAction()

function parsed(): InspectionSettings | null {
  try {
    const value = JSON.parse(advanced.value)
    return value && !Array.isArray(value) && typeof value === 'object' ? value : null
  } catch { return null }
}
function patch(patch: Partial<InspectionSettings>) {
  const current = parsed()
  if (!current) { toast.error('请先修正质检设置 JSON'); return }
  advanced.value = JSON.stringify({ ...current, ...patch }, null, 2)
}
const mode = computed({ get: () => parsed()?.mode ?? 'trial', set: (mode: 'trial' | 'formal') => patch({ mode }) })
const scheduling = computed({ get: () => parsed()?.scheduling?.enabled ?? false, set: (enabled: boolean) => patch({ scheduling: { ...(parsed()?.scheduling ?? { minimum_grade: 'B', strategy: 'quality_adjusted' }), enabled } }) })
const minimumGrade = computed({ get: () => parsed()?.scheduling?.minimum_grade ?? 'B', set: (minimum_grade: string) => patch({ scheduling: { ...(parsed()?.scheduling ?? { enabled: false, strategy: 'quality_adjusted' }), minimum_grade } }) })

function setSettings(settings: InspectionSettings) {
  advanced.value = JSON.stringify(settings, null, 2)
  loaded.value = true
}

async function load() {
  try { setSettings(await admin.inspectionSettings()) } catch (e) { toast.error(e) }
}

async function save() {
  const body = parsed()
  if (!body) {
    toast.error('质检高级设置需要是 JSON 对象')
    return
  }
  const result = await run(() => admin.updateInspectionSettings(body), '质检设置已保存，后续检测生效')
  if (result) setSettings(result)
}

onMounted(load)
</script>

<template>
  <section class="card mt-5">
    <div class="flex items-center justify-between gap-3"><h2 class="card-title">质检设置</h2><button class="btn-primary" :disabled="busy || !loaded" @click="save">保存质检设置</button></div>
    <p class="hint mb-4">独立保存质检模式、定级、预算与调度规则。</p>
    <template v-if="loaded">
      <div class="grid gap-4 md:grid-cols-3">
        <div><label class="label" for="inspection-mode">质检模式</label><select id="inspection-mode" v-model="mode" class="input"><option value="trial">试运行</option><option value="formal">正式</option></select></div>
        <label class="flex items-center gap-2 text-sm"><input id="inspection-scheduling" v-model="scheduling" type="checkbox" />启用质检调度</label>
        <div><label class="label" for="inspection-min-grade">保障池最低等级</label><select id="inspection-min-grade" v-model="minimumGrade" class="input"><option v-for="grade in ['S', 'A', 'B', 'C']" :key="grade" :value="grade">{{ grade }}</option></select></div>
      </div>
      <p class="callout my-4">{{ mode === 'trial' ? '试运行：计算并展示质检结果，等级、回收报价与调度沿用现有判定。即使开启质检调度也不生效。' : '正式：新题接入判定并按质量系数定级；开启质检调度后应用保障池准入和质量价格排序。' }}</p>
      <label class="label" for="inspection-advanced">定级、历史条件、时限、并发、token 预算与质检项覆盖（JSON）</label>
      <textarea id="inspection-advanced" v-model="advanced" class="input min-h-80 font-mono text-xs" spellcheck="false" />
      <p class="hint">可直接编辑完整对象，上方控件同步对应字段。未校准规则即使启用，也不能参与扣价。</p>
    </template>
    <p v-else class="hint">正在读取质检设置。<button class="link" @click="load">重新加载</button></p>
  </section>
</template>
