<template>
  <div class="space-y-4" data-testid="kimi-device-flow">
    <p class="text-sm text-gray-600 dark:text-gray-400">
      {{ t('admin.accounts.oauth.kimi.description') }}
    </p>
    <button
      v-if="!oauth.loading.value && oauth.status.value !== 'success'"
      type="button"
      class="btn btn-primary"
      data-testid="kimi-generate-code"
      @click="oauth.generateDeviceCode(proxyId)"
    >
      {{ t(oauth.deviceCode.value ? 'admin.accounts.oauth.kimi.restart' : 'admin.accounts.oauth.kimi.start') }}
    </button>
    <div
      v-if="oauth.deviceCode.value"
      class="space-y-4 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700"
    >
      <div>
        <p class="input-label">{{ t('admin.accounts.oauth.kimi.userCode') }}</p>
        <div class="flex flex-wrap items-center gap-3">
          <code class="break-all text-2xl font-semibold tracking-widest" data-testid="kimi-user-code">
            {{ oauth.deviceCode.value.user_code }}
          </code>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :aria-label="t('admin.accounts.oauth.kimi.copyCode')"
            @click="copyToClipboard(oauth.deviceCode.value.user_code)"
          >
            {{ t('common.copy') }}
          </button>
        </div>
      </div>
      <OAuthAuthorizationLink :url="verificationUrl" namespace="admin.accounts.oauth.kimi" test-prefix="kimi" />
      <p
        v-if="oauth.loading.value"
        class="text-sm text-gray-500 dark:text-gray-400"
        data-testid="kimi-countdown"
      >
        {{ t('admin.accounts.oauth.kimi.remaining', { seconds: oauth.remainingSeconds.value }) }}
      </p>
    </div>
    <p
      role="status"
      aria-live="polite"
      class="text-sm text-gray-700 dark:text-gray-300"
      data-testid="kimi-status"
    >
      {{ t(`admin.accounts.oauth.kimi.status.${oauth.status.value}`) }}
    </p>
    <p
      v-if="oauth.error.value || saveError"
      role="alert"
      class="text-sm text-red-600 dark:text-red-400"
    >
      {{ oauth.error.value || saveError }}
    </p>
    <button
      v-if="oauth.tokenInfo.value"
      type="button"
      class="btn btn-primary"
      :disabled="saving"
      data-testid="kimi-save-account"
      @click="emit('authorized', oauth.tokenInfo.value)"
    >
      {{ saving ? t('common.saving') : t('admin.accounts.oauth.completeAuth') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import OAuthAuthorizationLink from './OAuthAuthorizationLink.vue'
import { useI18n } from 'vue-i18n'
import { useKimiOAuth } from '@/composables/useKimiOAuth'
import { useClipboard } from '@/composables/useClipboard'
import type { KimiTokenInfo } from '@/api/admin/kimi'

const props = defineProps<{ proxyId?: number | null; saving?: boolean; saveError?: string }>()
const emit = defineEmits<{ authorized: [tokenInfo: KimiTokenInfo] }>()
const { t } = useI18n()
const oauth = useKimiOAuth()
const { copyToClipboard } = useClipboard()
const verificationUrl = computed(() => {
  const raw = oauth.deviceCode.value?.verification_uri_complete || oauth.deviceCode.value?.verification_uri || ''
  try {
    return new URL(raw).protocol === 'https:' ? raw : ''
  } catch {
    return ''
  }
})

defineExpose({ reset: oauth.resetState })
watch(() => props.proxyId, oauth.resetState)
watch(oauth.tokenInfo, token => {
  if (token) emit('authorized', token)
})
</script>
