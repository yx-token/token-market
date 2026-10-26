<template>
  <div class="space-y-6" data-testid="system-update-settings">
    <div class="card">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.systemUpdate.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading || busy || needRestart" data-testid="check-updates" @click="refreshVersion(true)">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ loading ? t('admin.settings.systemUpdate.checking') : t('admin.settings.systemUpdate.checkNow') }}
        </button>
      </div>
      <div class="space-y-6 p-6">
        <dl class="grid gap-6 sm:grid-cols-3">
          <div>
            <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('version.currentVersion') }}</dt>
            <dd class="mt-1 break-all font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ currentVersion || '—' }}</dd>
          </div>
          <div>
            <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('version.latestVersion') }}</dt>
            <dd class="mt-1 break-all font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ latestVersion || '—' }}</dd>
          </div>
          <div>
            <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.buildType') }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ versionChecked || updateSuccess ? buildLabel : '—' }}</dd>
          </div>
        </dl>
        <div aria-live="polite" class="space-y-3">
          <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.checking') }}</p>
          <div v-else-if="checkError || checkWarning" role="alert" class="rounded-lg bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
            <p class="font-medium">{{ t('admin.settings.systemUpdate.checkFailed') }}</p>
            <p class="mt-1 break-words">{{ checkError || checkWarning }}</p>
          </div>
          <p v-else-if="versionChecked && !updateSuccess" data-testid="version-status" class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ hasUpdate ? t('version.updateAvailable') : t('version.upToDate') }}
          </p>
          <div v-if="updateSuccess" role="status" class="space-y-3 rounded-lg bg-green-50 p-4 text-sm text-green-800 dark:bg-green-900/20 dark:text-green-300">
            <p class="font-medium">{{ successKind === 'rollback' ? t('version.rollbackComplete') : t('version.updateComplete') }}</p>
            <template v-if="needRestart">
              <p>{{ t('version.restartRequired') }}</p>
              <button type="button" class="btn btn-primary" :disabled="busy" data-testid="restart-service" @click="handleRestart">
                {{ restarting ? t('version.restarting') : t('version.restartNow') }}
              </button>
              <p v-if="restarting" class="tabular-nums">{{ t('admin.settings.systemUpdate.restartCountdown', { seconds: restartCountdown }) }}</p>
            </template>
          </div>
          <p v-if="updateError || restartError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ updateError || restartError }}</p>
        </div>
        <div v-if="isReleaseBuild" class="space-y-3">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.releaseHint') }}</p>
          <button
            v-if="hasUpdate && versionChecked && !checkError && !checkWarning && !updateSuccess"
            type="button"
            class="btn btn-primary"
            :disabled="loading || busy || needRestart"
            data-testid="perform-update"
            @click="handleUpdate"
          >
            <Icon name="download" size="sm" />
            {{ updating ? t('version.updating') : t('version.updateNow') }}
          </button>
        </div>
        <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-700 dark:bg-dark-800 dark:text-gray-300" data-testid="source-update-hint">
          {{ t('admin.settings.systemUpdate.sourceHint') }}
        </p>
        <a v-if="releaseInfo?.html_url && releaseInfo.html_url !== '#'" :href="releaseInfo.html_url" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-sm text-primary-600 hover:underline dark:text-primary-400">
          {{ t('version.viewChangelog') }} <Icon name="externalLink" size="sm" />
        </a>
      </div>
    </div>

    <div class="card">
      <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.systemUpdate.dockerTitle') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.dockerHint') }}</p>
      </div>
      <div class="space-y-4 p-6">
        <p class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.settings.systemUpdate.dockerInstructions') }}</p>
        <SystemUpdateCommand :label="t('admin.settings.systemUpdate.dockerCommand')" :code="dockerUpdateCommand" />
        <template v-if="dockerPinnedImage">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.systemUpdate.dockerPinnedHint') }}</p>
          <SystemUpdateCommand :label="t('admin.settings.systemUpdate.composeImage')" :code="dockerPinnedImage" />
        </template>
      </div>
    </div>

    <SystemRollbackPanel v-if="isReleaseBuild" :update="update" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
import Icon from '@/components/icons/Icon.vue'
import SystemUpdateCommand from './SystemUpdateCommand.vue'
import SystemRollbackPanel from './SystemRollbackPanel.vue'

const { t } = useI18n()
const update = useSystemUpdate()
const {
  loading, currentVersion, latestVersion, hasUpdate, releaseInfo, buildType, isReleaseBuild,
  versionChecked, checkError, checkWarning, updating, restarting, needRestart, updateError,
  restartError, updateSuccess, restartCountdown, successKind, busy, dockerUpdateCommand,
  dockerPinnedImage, refreshVersion, handleUpdate, handleRestart
} = update
const buildLabel = computed(() => isReleaseBuild.value
  ? t('admin.settings.systemUpdate.releaseBuild')
  : `${t('version.sourceMode')} (${buildType.value || 'source'})`)
onMounted(() => { void refreshVersion(false) })
</script>
