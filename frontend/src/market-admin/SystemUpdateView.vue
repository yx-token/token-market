<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import {
  checkUpdates,
  getRollbackVersions,
  getVersion,
  performUpdate,
  restartService,
  rollback,
  type RollbackVersionInfo,
  type VersionInfo,
} from '@/api/admin/system'

defineOptions({ name: 'MarketAdminSystemUpdate' })

const rollbackColumns: Column[] = [
  { key: 'version', label: '版本', class: 'col-ver' },
  { key: 'published_at', label: '发布时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const version = ref('')
const info = ref<VersionInfo | null>(null)
const rollbacks = ref<RollbackVersionInfo[]>([])
const loading = ref(false)
const checking = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const confirming = ref(false)
const confirmRollback = ref<string | null>(null)
let disposed = false

const canUpdate = computed(
  () =>
    !!info.value?.has_update &&
    info.value.build_type === 'release' &&
    !busy.value &&
    !checking.value &&
    !loading.value,
)

async function loadVersion() {
  const v = await getVersion()
  if (!disposed) version.value = v.version
}

async function loadCheck(force = false) {
  checking.value = true
  error.value = ''
  try {
    info.value = await checkUpdates(force)
    if (force) {
      msg.value = info.value.has_update
        ? `发现新版本 ${info.value.latest_version}`
        : '当前没有可用的新版本'
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '检查更新失败'
  } finally {
    checking.value = false
  }
}

async function loadRollbacks() {
  try {
    const res = await getRollbackVersions()
    if (!disposed) rollbacks.value = res.versions ?? []
  } catch {
    if (!disposed) rollbacks.value = []
  }
}

async function refresh(forceCheck = false) {
  loading.value = true
  error.value = ''
  try {
    await loadVersion()
    await Promise.all([loadCheck(forceCheck), loadRollbacks()])
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function startUpdate() {
  confirming.value = false
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const res = await performUpdate()
    msg.value = res.message || '更新已完成'
    if (res.need_restart) msg.value += '（需要重启服务）'
    await refresh()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '更新失败'
  } finally {
    busy.value = false
  }
}

async function doRollback(target?: string) {
  confirmRollback.value = null
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const res = await rollback(target)
    msg.value = res.message || '回滚已完成'
    if (res.need_restart) msg.value += '（需要重启服务）'
    await refresh()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '回滚失败'
  } finally {
    busy.value = false
  }
}

async function doRestart() {
  if (!window.confirm('确认重启服务？进行中的请求可能中断。')) return
  busy.value = true
  error.value = ''
  try {
    const res = await restartService()
    msg.value = res.message || '已请求重启'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '重启失败'
  } finally {
    busy.value = false
  }
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}

onMounted(() => {
  void refresh(false)
})
onUnmounted(() => {
  disposed = true
})
</script>

<template>
  <section id="market-system-update-page" class="update-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">系统更新</h2>
          <p class="y-page-desc">查看引擎版本、检查 GitHub 发布并执行站内更新 / 回滚</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="checking || busy" @click="refresh(true)">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': checking }" />
          {{ checking ? '检查中…' : '检查更新' }}
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="busy" @click="doRestart">
          重启服务
        </button>
      </template>
      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>
        <p v-if="loading && !info" class="y-msg mb-2">加载中…</p>

        <div class="cards-row">
          <div class="card-block">
            <div class="card-head">
              <h3>当前版本</h3>
              <span class="mono ver">{{ version || '—' }}</span>
            </div>
            <dl class="kv">
              <div>
                <dt>构建类型</dt>
                <dd>{{ info?.build_type || '—' }}</dd>
              </div>
              <div>
                <dt>缓存结果</dt>
                <dd>{{ info ? (info.cached ? '是' : '否') : '—' }}</dd>
              </div>
            </dl>
            <p v-if="info?.warning" class="hint warn">{{ info.warning }}</p>
            <p v-if="info?.build_type === 'source'" class="hint">
              源码构建不支持一键更新，请通过镜像或发布包升级。
            </p>
          </div>

          <div class="card-block">
            <div class="card-head">
              <h3>最新正式版本</h3>
              <button type="button" class="btn btn-primary page-action-btn" :disabled="!canUpdate" @click="confirming = true">
                {{ busy ? '处理中…' : '更新系统' }}
              </button>
            </div>
            <template v-if="info">
              <div class="release-line">
                <span class="mono ver">{{ info.latest_version || '—' }}</span>
                <span class="hint">{{ info.has_update ? '有新版本可用' : '已是最新' }}</span>
                <a
                  v-if="info.release_info?.html_url"
                  class="y-link ml-auto"
                  :href="info.release_info.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  发布详情 ↗
                </a>
              </div>
              <p v-if="info.release_info?.published_at" class="hint">
                发布于 {{ fmtTime(info.release_info.published_at) }}
              </p>
              <pre v-if="info.release_info?.body" class="notes">{{ info.release_info.body }}</pre>
            </template>
            <p v-else class="hint">{{ checking ? '正在查询发布信息…' : '尚未查询发布信息' }}</p>
          </div>
        </div>
        <p class="hint mt">更新会下载完整发布包，耗时可能较长；失败时请查看服务日志。</p>
      </template>
      <template #table>
        <div class="section-title">可回滚版本</div>
        <DataTable :columns="rollbackColumns" :data="rollbacks" :loading="loading" :expandable-actions="false">
          <template #cell-version="{ row }">
            <span class="mono">{{ row.version }}</span>
          </template>
          <template #cell-published_at="{ row }">{{ fmtTime(row.published_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button
                type="button"
                class="y-link y-link--danger"
                :disabled="busy"
                @click="confirmRollback = row.version"
              >
                回滚
              </button>
              <a
                v-if="row.html_url"
                class="y-link"
                :href="row.html_url"
                target="_blank"
                rel="noopener noreferrer"
              >
                详情
              </a>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无可回滚版本" description="完成正式版更新后会出现可回滚记录" />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <div v-if="confirming" class="y-modal-mask" @click.self="confirming = false">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>确认更新系统</h3>
          <button type="button" class="y-link" @click="confirming = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <p>
            将从 <strong class="mono">{{ version }}</strong> 更新至
            <strong class="mono">{{ info?.latest_version }}</strong>。
          </p>
          <p class="hint">服务可能短暂不可用，进行中的请求可能中断。请确认已备份数据库。</p>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="confirming = false">取消</button>
          <button type="button" class="btn btn-primary" :disabled="!canUpdate" @click="startUpdate">确认更新</button>
        </footer>
      </div>
    </div>

    <div v-if="confirmRollback" class="y-modal-mask" @click.self="confirmRollback = null">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>确认回滚</h3>
          <button type="button" class="y-link" @click="confirmRollback = null">关闭</button>
        </header>
        <div class="y-modal__body">
          <p>
            回滚到 <strong class="mono">{{ confirmRollback }}</strong>？
          </p>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="confirmRollback = null">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="doRollback(confirmRollback!)">
            确认回滚
          </button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
#market-system-update-page.update-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.page-heading { min-width: 0; }
.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.cards-row {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
}
.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
}
.card-block {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 10px;
  padding: 14px 16px;
  background: var(--color-bg-elevated, #fff);
}
#market-system-update-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-system-update-page :deep(th.col-ops),
#market-system-update-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
}
@media (max-width: 900px) {
  .cards-row { grid-template-columns: 1fr; }
}
.card-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}
.card-head h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.ver {
  font-size: 1.05rem;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
.kv {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  margin: 0;
  font-size: 13px;
}
.kv dt {
  color: var(--color-text-secondary, #868686);
}
.kv dd {
  margin: 2px 0 0;
}
.release-line {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 10px;
}
.notes {
  margin: 10px 0 0;
  max-height: 16rem;
  overflow: auto;
  padding: 10px;
  border-radius: 8px;
  background: var(--color-bg-secondary, #f8fafc);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
.hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.hint.warn {
  color: #b45309;
}
.hint.mt {
  margin-top: 12px;
}
.ml-auto {
  margin-left: auto;
}
.y-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}
.y-modal {
  width: min(440px, 100%);
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head,
.y-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__foot {
  justify-content: flex-end;
}
.y-modal__body {
  padding: 0 16px 16px;
  font-size: 14px;
}
</style>
