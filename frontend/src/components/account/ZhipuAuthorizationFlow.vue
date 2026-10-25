<template>
  <div class="space-y-4" data-testid="zhipu-oauth-flow">
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('admin.accounts.oauth.zhipu.description') }}</p>
    <p class="text-sm font-medium">{{ t(`admin.accounts.oauth.zhipu.providers.${provider}`) }}</p>
    <button
      v-if="!oauth.loading.value && oauth.status.value !== 'success'"
      type="button" class="btn btn-primary" data-testid="zhipu-init"
      @click="oauth.initOAuth(provider, proxyId)"
    >{{ t(oauth.session.value ? 'admin.accounts.oauth.zhipu.restart' : 'admin.accounts.oauth.zhipu.start') }}</button>
    <div v-if="oauth.session.value" class="space-y-4 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
      <OAuthAuthorizationLink
        :url="oauth.session.value.authorize_url"
        namespace="admin.accounts.oauth.zhipu" test-prefix="zhipu"
      />
      <p v-if="oauth.loading.value" class="text-sm text-gray-500 dark:text-gray-400" data-testid="zhipu-countdown">
        {{ t('admin.accounts.oauth.zhipu.remaining', { seconds: oauth.remainingSeconds.value }) }}
      </p>
    </div>
    <p role="status" aria-live="polite" class="text-sm text-gray-700 dark:text-gray-300" data-testid="zhipu-status">
      {{ t(`admin.accounts.oauth.zhipu.status.${oauth.status.value}`) }}
    </p>
    <p v-if="oauth.error.value || saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">
      {{ oauth.error.value || saveError }}
    </p>
    <details v-if="oauth.session.value && oauth.status.value === 'pending'" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <summary class="cursor-pointer text-sm font-medium">{{ t('admin.accounts.oauth.zhipu.manualTitle') }}</summary>
      <div class="mt-3 space-y-3">
        <label class="input-label">
          {{ t('admin.accounts.oauth.zhipu.callbackLabel') }}
          <textarea v-model="callback" rows="3" class="input mt-1" :placeholder="t('admin.accounts.oauth.zhipu.callbackPlaceholder')" data-testid="zhipu-callback" :disabled="oauth.exchanging.value" />
        </label>
        <p class="input-hint">{{ t('admin.accounts.oauth.zhipu.manualHint') }}</p>
        <button type="button" class="btn btn-secondary" :disabled="!callback.trim() || oauth.exchanging.value" data-testid="zhipu-exchange" @click="oauth.exchangeCode(callback)">
          {{ t(oauth.exchanging.value ? 'admin.accounts.oauth.verifying' : 'admin.accounts.oauth.completeAuth') }}
        </button>
      </div>
    </details>
    <button v-if="oauth.result.value" type="button" class="btn btn-primary" :disabled="saving" data-testid="zhipu-save-account" @click="emit('authorized', oauth.result.value)">
      {{ saving ? t('common.saving') : t('admin.accounts.oauth.completeAuth') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useZhipuOAuth } from '@/composables/useZhipuOAuth'
import type { ZhipuOAuthProvider, ZhipuOAuthResult } from '@/api/admin/zhipu'
import OAuthAuthorizationLink from './OAuthAuthorizationLink.vue'
const props = defineProps<{ provider: ZhipuOAuthProvider; proxyId?: number | null; saving?: boolean; saveError?: string }>()
const emit = defineEmits<{ authorized: [result: ZhipuOAuthResult] }>()
const { t } = useI18n()
const oauth = useZhipuOAuth()
const callback = ref('')
const reset = () => { callback.value = ''; oauth.resetState() }
defineExpose({ reset })
watch(() => [props.provider, props.proxyId], reset)
watch(oauth.result, value => { if (value) { callback.value = ''; emit('authorized', value) } })
</script>
