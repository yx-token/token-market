<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { admin } from '@/api'
import { ApiError } from '@/api/client'
import type { SystemVersion, SystemUpdateCheck } from '@/api/types'
import { useToast } from '@/stores/toast'
import { dateTime } from '@/utils/format'
import PageHeader from '@/components/PageHeader.vue'
import ModalDialog from '@/components/ModalDialog.vue'

const toast = useToast()
const status = ref<SystemVersion | null>(null)
const check = ref<SystemUpdateCheck | null>(null)
const checking = ref(false)
const submitting = ref(false)
const confirming = ref(false)
const reconnecting = ref(false)
const statusError = ref('')
const checkError = ref('')
const finished = new Set(['succeeded', 'failed', 'rolled_back', 'rollback_failed'])
const active = computed(() => !!status.value?.job && !finished.has(status.value.job.phase))
const canUpdate = computed(() => status.value?.enabled && check.value?.update_available && check.value.release.compatible
  && !active.value && !checking.value && !submitting.value && !reconnecting.value && !statusError.value && !checkError.value
  && status.value.job?.phase !== 'rollback_failed')
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let polling = false

function schedulePoll(delay: number) {
  clearTimeout(timer)
  if (!disposed) timer = setTimeout(poll, delay)
}

function message(e: unknown) {
  return e instanceof ApiError ? e.text : e instanceof Error ? e.message : '请求失败，请重试'
}

async function loadStatus() {
  try {
    const next = await admin.systemVersion()
    if (disposed) return
    status.value = next
    reconnecting.value = false
    statusError.value = ''
  } catch (e) {
    if (disposed) return
    reconnecting.value = !!status.value
    statusError.value = message(e)
  }
}

async function checkUpdate(manual = true) {
  if (checking.value) return
  checking.value = true
  checkError.value = ''
  try {
    const result = await admin.checkSystemUpdate(manual)
    if (disposed) return
    check.value = result
    if (manual) toast.info(result.update_available ? `发现新版本 ${result.release.version}` : '当前没有可用的新版本')
  } catch (e) {
    if (!disposed) checkError.value = message(e)
  } finally {
    checking.value = false
  }
}

async function startUpdate() {
  if (!canUpdate.value || !check.value || !status.value) return
  confirming.value = false
  submitting.value = true
  try {
    const job = await admin.startSystemUpdate(check.value.release.version)
    if (disposed) return
    status.value = { ...status.value, enabled: false, reason: '已有更新任务正在执行', job }
    schedulePoll(1000)
    toast.info('更新任务已创建，可在此查看进度')
  } catch (e) {
    if (!disposed) toast.error(e)
    await loadStatus()
  } finally {
    submitting.value = false
  }
}

async function poll() {
  if (polling || disposed) return
  polling = true
  await loadStatus()
  polling = false
  schedulePoll(active.value || reconnecting.value ? 2000 : 10000)
}

function reloadPage() { window.location.reload() }

onMounted(() => {
  void poll()
  void checkUpdate(false)
})
onUnmounted(() => { disposed = true; clearTimeout(timer) })
</script>

<template>
  <div>
    <PageHeader title="系统更新" kicker="SYSTEM UPDATE" description="查看发布版本，在站内完成更新并跟踪进度。">
      <button class="btn-secondary" :disabled="checking || submitting || active || reconnecting" @click="checkUpdate()">
        {{ checking ? '检查中…' : '检查更新' }}
      </button>
    </PageHeader>

    <div v-if="!status" class="card" role="status">
      <p v-if="statusError" class="text-brand-600">{{ statusError }}</p>
      <p v-else class="text-ink-500">正在读取系统版本…</p>
      <button v-if="statusError" class="btn-secondary mt-4" @click="loadStatus">重新加载</button>
    </div>

    <template v-else>
      <div class="card mb-6">
        <div class="flex flex-wrap items-baseline justify-between gap-3 border-b border-ink-200 pb-4">
          <h2 class="font-display text-lg font-bold">当前版本</h2>
          <span class="font-mono text-lg">{{ status.version }}</span>
        </div>
        <dl class="mt-4 grid gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
          <div><dt class="text-ink-500">构建提交</dt><dd class="mt-1 font-mono">{{ status.commit.slice(0, 12) || '—' }}</dd></div>
          <div><dt class="text-ink-500">运行平台</dt><dd class="mt-1 font-mono">{{ status.platform }}</dd></div>
          <div class="sm:col-span-2"><dt class="text-ink-500">发布仓库</dt><dd class="mt-1 break-all font-mono">{{ status.repository }}</dd></div>
        </dl>
        <p v-if="status.reason && !active" class="mt-4 border-t border-ink-200 pt-4 text-sm text-ink-600">{{ status.reason }}</p>
      </div>

      <div v-if="status.job || reconnecting" class="card mb-6" role="status" aria-live="polite">
        <h2 class="font-display text-lg font-bold">更新进度 <span class="ml-2 font-mono text-sm font-normal">{{ status.job?.version }}</span></h2>
        <p class="mt-3 text-sm" :class="status.job && ['failed', 'rolled_back', 'rollback_failed'].includes(status.job.phase) ? 'text-brand-600' : 'text-ink-700'">
          {{ reconnecting ? '服务正在重启或暂时无法连接，正在自动重试…' : status.job?.message }}
        </p>
        <p v-if="status.job" class="mt-2 text-xs text-ink-500">最后更新：{{ dateTime(status.job.updated_at) }}</p>
        <button v-if="status.job?.phase === 'succeeded' && !reconnecting" class="btn-primary mt-4" @click="reloadPage">刷新页面，加载新版本</button>
        <p v-if="status.job?.phase === 'rollback_failed'" class="mt-3 text-sm text-brand-600">请先恢复服务健康状态，再进行更新。</p>
      </div>

      <div class="card">
        <div class="flex flex-wrap items-center justify-between gap-4 border-b border-ink-200 pb-4">
          <h2 class="font-display text-lg font-bold">最新正式版本</h2>
          <button class="btn-primary" :disabled="!canUpdate" @click="confirming = true">{{ submitting ? '提交中…' : active ? '正在更新…' : '更新系统' }}</button>
        </div>
        <p v-if="checkError" class="mt-4 text-sm text-brand-600" role="alert">{{ checkError }}</p>
        <p v-else-if="!check" class="mt-4 text-sm text-ink-500">{{ checking ? '正在查询发布信息…' : '尚未查询发布信息' }}</p>
        <template v-else>
          <div class="mt-4 flex flex-wrap items-baseline gap-x-4 gap-y-2">
            <span class="font-mono text-xl">{{ check.release.version }}</span>
            <span class="text-sm text-ink-600">{{ check.update_available && status.version !== check.release.version ? '有新版本可用' : '当前没有可用的新版本' }}</span>
            <a :href="check.release.url" target="_blank" rel="noopener noreferrer" class="ml-auto text-sm underline underline-offset-4">查看发布详情 ↗</a>
          </div>
          <p class="mt-2 text-xs text-ink-500">发布于 {{ dateTime(check.release.published_at) }}</p>
          <p v-if="!check.release.compatible" class="mt-4 text-sm text-brand-600">此版本尚未提供当前平台的完整更新包，请通过镜像升级。</p>
          <pre class="mt-5 max-h-96 overflow-y-auto whitespace-pre-wrap break-words border-t border-ink-200 pt-4 font-sans text-sm leading-relaxed text-ink-700">{{ check.release.notes || '此版本暂无发布说明。' }}</pre>
        </template>
        <p class="mt-5 border-t border-ink-200 pt-4 text-xs leading-relaxed text-ink-500">更新会同步替换三个服务和前端，服务将短暂重启；新版本健康检查失败时自动回退。数据库迁移不会随程序回退撤销。</p>
      </div>
    </template>

    <ModalDialog v-model:open="confirming" title="确认更新系统">
      <p class="text-sm leading-relaxed">将系统从 <strong>{{ status?.version }}</strong> 更新至 <strong>{{ check?.release.version }}</strong>。</p>
      <p class="text-sm leading-relaxed text-ink-600">服务将短暂重启，进行中的模型请求可能中断。请确认已阅读发布说明，并已备份数据库。</p>
      <template #footer>
        <button class="btn-secondary" @click="confirming = false">取消</button>
        <button class="btn-primary" :disabled="!canUpdate" @click="startUpdate">确认更新</button>
      </template>
    </ModalDialog>
  </div>
</template>
