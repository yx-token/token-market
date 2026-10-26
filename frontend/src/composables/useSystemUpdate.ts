import { computed, onScopeDispose, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { performUpdate, restartService, getRollbackVersions, rollback as rollbackAPI, type RollbackVersionInfo } from '@/api/admin/system'
import { extractApiErrorMessage } from '@/utils/apiError'

const GITHUB_REPO = 'yx-token/token-market'
const DOCKER_IMAGE = 'weishaw/sub2api'

/** Shared actions for the sidebar badge and the settings update panel. Version checks use the app cache. */
export function useSystemUpdate(fallbackVersion?: MaybeRefOrGetter<string | undefined>) {
  const { t } = useI18n()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const isAdmin = computed(() => authStore.isAdmin)
  const loading = computed(() => appStore.versionLoading)
  const currentVersion = computed(() => appStore.currentVersion || toValue(fallbackVersion) || appStore.siteVersion || '')
  const latestVersion = computed(() => appStore.latestVersion)
  const hasUpdate = computed(() => appStore.hasUpdate)
  const releaseInfo = computed(() => appStore.releaseInfo)
  const buildType = computed(() => appStore.buildType)
  const isReleaseBuild = computed(() => buildType.value === 'release')
  const versionChecked = computed(() => appStore.versionLoaded)
  const checkError = computed(() => appStore.versionError)
  const checkWarning = computed(() => appStore.versionWarning)
  const updating = ref(false)
  const restarting = ref(false)
  const needRestart = ref(false)
  const updateError = ref('')
  const restartError = ref('')
  const updateSuccess = ref(false)
  const restartCountdown = ref(0)
  const successKind = ref<'update' | 'rollback'>('update')
  const rollbackPanelOpen = ref(false)
  const rollbackVersions = ref<RollbackVersionInfo[]>([])
  const rollbackVersionsLoading = ref(false)
  const rollbackVersionsError = ref('')
  const selectedRollbackVersion = ref('')
  const rollingBack = ref(false)
  const rollbackError = ref('')
  const busy = computed(() => updating.value || rollingBack.value || restarting.value)
  const manualTab = ref<'script' | 'docker'>('script')
  const manualTabs = computed(() => [
    { key: 'script' as const, label: t('version.deployScript') },
    { key: 'docker' as const, label: t('version.deployDocker') }
  ])
  const scriptRollbackCommand = computed(() => selectedRollbackVersion.value
    ? `curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/v${selectedRollbackVersion.value}/deploy/install.sh | sudo bash -s -- rollback v${selectedRollbackVersion.value}`
    : '')
  const dockerRollbackCommand = computed(() => selectedRollbackVersion.value ? [
    `# ${t('version.dockerEditCompose')}`, `image: ${DOCKER_IMAGE}:${selectedRollbackVersion.value}`, '',
    `# ${t('version.dockerRecreate')}`, 'docker compose up -d'
  ].join('\n') : '')
  const activeManualCommand = computed(() => manualTab.value === 'docker' ? dockerRollbackCommand.value : scriptRollbackCommand.value)
  const dockerUpdateCommand = 'docker compose pull && docker compose up -d'
  const dockerPinnedImage = computed(() => {
    const tag = (latestVersion.value || '').replace(/^v/, '')
    return /^\d+\.\d+\.\d+(?:[-+][\w.-]+)?$/.test(tag) ? `image: ${DOCKER_IMAGE}:${tag}` : ''
  })
  let disposed = false
  let countdownTimer: ReturnType<typeof setInterval> | undefined
  let retryTimer: ReturnType<typeof setTimeout> | undefined
  let resolveRetry: (() => void) | undefined
  const healthController = new AbortController()

  function resetRollbackState() {
    rollbackPanelOpen.value = false
    rollbackVersions.value = []
    rollbackVersionsError.value = ''
    selectedRollbackVersion.value = ''
    rollbackError.value = ''
    manualTab.value = 'script'
  }
  async function refreshVersion(force = true) {
    if (!isAdmin.value || busy.value || loading.value || needRestart.value) return
    updateError.value = ''
    updateSuccess.value = false
    resetRollbackState()
    await appStore.fetchVersion(force)
  }
  async function handleUpdate() {
    if (!isAdmin.value || !isReleaseBuild.value || busy.value || needRestart.value) return
    updating.value = true
    updateError.value = ''
    updateSuccess.value = false
    try {
      const result = await performUpdate()
      if (disposed) return
      successKind.value = 'update'
      updateSuccess.value = true
      needRestart.value = result.need_restart
      appStore.clearVersionCache()
    } catch (error) {
      if (!disposed) updateError.value = extractApiErrorMessage(error, t('version.updateFailed'))
    } finally {
      updating.value = false
    }
  }
  async function loadRollbackVersions() {
    if (!isAdmin.value || !isReleaseBuild.value || rollbackVersionsLoading.value || busy.value) return
    rollbackVersionsLoading.value = true
    rollbackVersionsError.value = ''
    try {
      const data = await getRollbackVersions()
      if (!disposed) rollbackVersions.value = (data.versions || []).slice(0, 3)
    } catch (error) {
      if (!disposed) rollbackVersionsError.value = extractApiErrorMessage(error, t('version.loadVersionsFailed'))
    } finally {
      rollbackVersionsLoading.value = false
    }
  }
  async function toggleRollbackPanel() {
    if (!isAdmin.value || busy.value || needRestart.value) return
    rollbackPanelOpen.value = !rollbackPanelOpen.value
    if (rollbackPanelOpen.value && rollbackVersions.value.length === 0) await loadRollbackVersions()
  }
  function selectRollbackVersion(version: string) {
    if (busy.value) return
    rollbackError.value = ''
    selectedRollbackVersion.value = selectedRollbackVersion.value === version ? '' : version
  }
  function formatPublishedAt(publishedAt: string): string {
    if (!publishedAt) return ''
    const date = new Date(publishedAt)
    return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString()
  }
  async function handleRollback() {
    if (!isAdmin.value || !isReleaseBuild.value || busy.value || needRestart.value || !selectedRollbackVersion.value) return
    rollingBack.value = true
    rollbackError.value = ''
    try {
      const result = await rollbackAPI(selectedRollbackVersion.value)
      if (disposed) return
      successKind.value = 'rollback'
      updateSuccess.value = true
      needRestart.value = result.need_restart
      rollbackPanelOpen.value = false
      appStore.clearVersionCache()
    } catch (error) {
      if (!disposed) rollbackError.value = extractApiErrorMessage(error, t('version.rollbackFailed'))
    } finally {
      rollingBack.value = false
    }
  }
  async function checkServiceAndReload() {
    for (let i = 0; i < 5 && !disposed; i++) {
      try {
        const response = await fetch('/health', { method: 'GET', cache: 'no-cache', signal: healthController.signal })
        if (disposed) return
        if (response.ok) { window.location.reload(); return }
      } catch { /* Restart may temporarily disconnect the server. */ }
      if (i < 4 && !disposed) await new Promise<void>(resolve => {
        resolveRetry = resolve
        retryTimer = setTimeout(resolve, 1000)
      })
    }
    if (!disposed) window.location.reload()
  }
  async function handleRestart() {
    if (!isAdmin.value || !isReleaseBuild.value || busy.value || !needRestart.value) return
    restarting.value = true
    restartError.value = ''
    restartCountdown.value = 8
    try {
      await restartService()
    } catch (error) {
      const err = error as { status?: number; response?: { status?: number } }
      // A lost connection is expected; an explicit HTTP rejection is an actual failure.
      if (err?.status || err?.response?.status) {
        restarting.value = false
        restartCountdown.value = 0
        restartError.value = extractApiErrorMessage(error, t('admin.settings.systemUpdate.restartFailed'))
        return
      }
    }
    if (disposed) return
    countdownTimer = setInterval(() => {
      restartCountdown.value--
      if (restartCountdown.value <= 0) {
        clearInterval(countdownTimer)
        void checkServiceAndReload()
      }
    }, 1000)
  }
  onScopeDispose(() => {
    disposed = true
    clearInterval(countdownTimer)
    clearTimeout(retryTimer)
    resolveRetry?.()
    healthController.abort()
  })
  return {
    isAdmin, loading, currentVersion, latestVersion, hasUpdate, releaseInfo, buildType, isReleaseBuild,
    versionChecked, checkError, checkWarning, updating, restarting, needRestart, updateError, restartError,
    updateSuccess, restartCountdown, successKind, busy, rollbackPanelOpen, rollbackVersions,
    rollbackVersionsLoading, rollbackVersionsError, selectedRollbackVersion, rollingBack, rollbackError,
    manualTab, manualTabs, activeManualCommand, dockerUpdateCommand, dockerPinnedImage,
    refreshVersion, handleUpdate, toggleRollbackPanel, loadRollbackVersions, selectRollbackVersion,
    formatPublishedAt, handleRollback, handleRestart
  }
}
export type SystemUpdateState = ReturnType<typeof useSystemUpdate>
