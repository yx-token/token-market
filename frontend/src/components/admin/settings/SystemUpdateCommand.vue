<template>
  <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
    <div class="flex items-center justify-between gap-3 bg-gray-50 px-3 py-2 dark:bg-dark-800">
      <span class="text-xs font-medium text-gray-600 dark:text-gray-300">{{ label }}</span>
      <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="copyToClipboard(code)">
        {{ copied ? t('version.copied') : t('version.copyCommand') }}
      </button>
    </div>
    <pre class="overflow-x-auto p-3 text-xs leading-5 text-gray-800 dark:text-gray-200"><code>{{ code }}</code></pre>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'

defineProps<{ label: string; code: string }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
</script>
