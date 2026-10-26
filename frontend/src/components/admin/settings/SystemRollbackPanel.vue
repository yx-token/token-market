<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <button
        type="button"
        class="flex w-full items-center justify-between gap-3 text-left"
        :aria-expanded="rollbackPanelOpen"
        aria-controls="system-rollback-panel"
        :disabled="busy || needRestart"
        data-testid="toggle-rollback"
        @click="toggleRollbackPanel"
      >
        <span class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('version.rollback') }}</span>
        <Icon name="chevronDown" size="sm" :class="{ 'rotate-180': rollbackPanelOpen }" />
      </button>
    </div>
    <div v-if="rollbackPanelOpen" id="system-rollback-panel" class="space-y-4 p-6">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('version.rollbackSelectVersion') }}</p>
      <p v-if="rollbackVersionsLoading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-else-if="rollbackVersionsError" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
        <p>{{ rollbackVersionsError }}</p>
        <button type="button" class="btn btn-secondary btn-sm" data-testid="retry-rollback" @click="loadRollbackVersions">
          {{ t('version.retry') }}
        </button>
      </div>
      <p v-else-if="!rollbackVersions.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('version.noRollbackVersions') }}</p>
      <div v-else class="flex flex-wrap gap-3" :aria-label="t('version.rollbackSelectVersion')" role="group">
        <button
          v-for="version in rollbackVersions"
          :key="version.version"
          type="button"
          class="btn btn-secondary flex-col items-start"
          :class="{ 'ring-2 ring-primary-500': selectedRollbackVersion === version.version }"
          :aria-pressed="selectedRollbackVersion === version.version"
          :disabled="busy"
          data-testid="rollback-version"
          @click="selectRollbackVersion(version.version)"
        >
          <span class="font-mono">v{{ version.version }}</span>
          <span v-if="formatPublishedAt(version.published_at)" class="text-xs text-gray-500 dark:text-gray-400">
            {{ formatPublishedAt(version.published_at) }}
          </span>
        </button>
      </div>
      <template v-if="selectedRollbackVersion">
        <p class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
          {{ t('version.rollbackWarning') }}
        </p>
        <p v-if="rollbackError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ rollbackError }}</p>
        <button type="button" class="btn btn-secondary" :disabled="busy || needRestart" data-testid="rollback-submit" @click="handleRollback">
          {{ rollingBack ? t('version.rollingBack') : t('version.rollbackConfirm', { version: 'v' + selectedRollbackVersion }) }}
        </button>
        <div class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('version.manualRollbackCommand') }}</h3>
          <div class="flex gap-2" role="group" :aria-label="t('version.manualRollbackCommand')">
            <button
              v-for="tab in manualTabs"
              :key="tab.key"
              type="button"
              class="btn btn-secondary btn-sm"
              :aria-pressed="manualTab === tab.key"
              :class="{ 'ring-2 ring-primary-500': manualTab === tab.key }"
              @click="manualTab = tab.key"
            >{{ tab.label }}</button>
          </div>
          <SystemUpdateCommand :label="t('version.manualRollbackCommand')" :code="activeManualCommand" />
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SystemUpdateState } from '@/composables/useSystemUpdate'
import Icon from '@/components/icons/Icon.vue'
import SystemUpdateCommand from './SystemUpdateCommand.vue'

const props = defineProps<{ update: SystemUpdateState }>()
const { t } = useI18n()
const {
  rollbackPanelOpen, rollbackVersions, rollbackVersionsLoading, rollbackVersionsError,
  selectedRollbackVersion, rollingBack, rollbackError, busy, needRestart, manualTab,
  manualTabs, activeManualCommand, toggleRollbackPanel, loadRollbackVersions,
  selectRollbackVersion, formatPublishedAt, handleRollback
} = props.update
</script>
