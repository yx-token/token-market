<template>
  <div>
    <p class="input-label">{{ t(`${namespace}.verificationUrl`) }}</p>
    <p class="break-all text-sm" :data-testid="`${testPrefix}-verification-url`">{{ safeUrl }}</p>
    <div class="mt-2 flex flex-wrap gap-2">
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="!safeUrl"
        :aria-label="t(`${namespace}.copyUrl`)"
        @click="copyToClipboard(safeUrl)"
      >{{ t('common.copy') }}</button>
      <a v-if="safeUrl" :href="safeUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm">
        {{ t(`${namespace}.openUrl`) }}
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
const props = defineProps<{ url: string; namespace: string; testPrefix: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const safeUrl = computed(() => {
  try { return new URL(props.url).protocol === 'https:' ? props.url : '' } catch { return '' }
})
</script>
