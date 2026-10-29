<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import type { Upstream } from '@/api/types'
import { useSellerOAuth } from '@/composables/useSellerOAuth'
import { authGuides, takesProjectID } from '@/utils/authGuides'
import { copyText } from '@/utils/clipboard'
import { useToast } from '@/stores/toast'

const props = defineProps<{ upstreamId: number; platform: string; authType: string; reauthorize?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ ready: [upstream: Upstream] }>()
const toast = useToast()
const inputID = useId()
const project = ref('')
const code = ref('')
const { session, busy, active, error, status, countdown, start, complete, stop } = useSellerOAuth(
  () => props.upstreamId, () => !!props.reauthorize, (upstream) => emit('ready', upstream),
)
const expectedFlow = computed(() => props.platform === 'kimi' && props.authType === 'oauth' ? 'device-code'
  : props.platform === 'zhipu' && ['oauth', 'oauth-intl'].includes(props.authType) ? 'init-poll' : 'link-code')
const flow = computed(() => session.value?.flow ?? expectedFlow.value)
const guide = computed(() => authGuides[`${props.platform}:${props.authType}`])
const withProject = computed(() => takesProjectID(props.platform, props.authType))
const authorizationURL = computed(() => {
  const value = flow.value === 'device-code' ? session.value?.verification_uri_complete || session.value?.verification_uri : session.value?.auth_url
  if (!value) return ''
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) ? url.href : ''
  } catch { return '' }
})

watch(() => [props.upstreamId, props.platform, props.authType, props.reauthorize], () => {
  stop()
  session.value = null
  error.value = ''
  status.value = ''
  code.value = ''
  project.value = ''
})

async function begin() {
  code.value = ''
  await start(withProject.value && project.value.trim() ? { project_id: project.value.trim() } : undefined)
}

async function copy(value: string) {
  if (await copyText(value)) toast.success('已复制')
  else toast.error('复制失败，请手动选择并复制。')
}

function submit() {
  const value = code.value.trim()
  if (!value || props.disabled) return
  return complete(flow.value === 'init-poll' ? { callback_url: value } : { code: value })
}
</script>

<template>
  <section class="space-y-3" aria-label="官方账号授权" :aria-busy="busy">
    <p v-if="guide?.how" class="hint">{{ guide.how }}</p>
    <div v-if="withProject">
      <label class="label" :for="`${inputID}-project`">GCP 项目 ID（可选）</label>
      <input :id="`${inputID}-project`" v-model="project" class="input font-mono" :disabled="active || busy" placeholder="my-project-123456" autocomplete="off" />
    </div>
    <button type="button" class="xm-btn xm-btn-ghost" :disabled="busy || active || disabled" @click="begin">
      {{ busy ? '正在处理…' : active ? '授权进行中' : session ? '重新发起授权' : flow === 'device-code' ? '获取设备码' : '获取授权链接' }}
    </button>
    <div v-if="session && active" class="callout callout-info space-y-3">
      <template v-if="flow === 'device-code'">
        <p class="text-sm">请在 Kimi 官方页面确认以下设备码，仅授权你自己的订阅账号。</p>
        <div class="flex flex-wrap items-center gap-3">
          <strong class="font-mono text-3xl tracking-widest" data-testid="user-code">{{ session.user_code }}</strong>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="copy(session.user_code || '')">复制设备码</button>
        </div>
      </template>
      <div v-if="authorizationURL" class="space-y-2">
        <a :href="authorizationURL" target="_blank" rel="noopener noreferrer" class="link break-all text-sm">{{ authorizationURL }}</a>
        <div class="flex flex-wrap gap-2">
          <a :href="authorizationURL" target="_blank" rel="noopener noreferrer" class="xm-btn xm-btn-ghost xm-btn-sm">新窗口打开授权页</a>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="copy(authorizationURL)">复制授权链接</button>
        </div>
      </div>
      <p v-else class="text-sm">未收到可用的授权链接，请取消后重试。</p>
      <p class="text-sm">剩余时间：<span class="font-mono">{{ countdown }}</span>。请勿关闭此页面。</p>
      <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy" @click="stop">取消本次授权</button>
    </div>
    <p v-if="status" role="status" class="hint">{{ status }}</p>
    <p v-if="error" role="alert" class="callout callout-err">{{ error }}</p>
    <p v-if="guide?.guideURL" class="hint">
      <a :href="guide.guideURL" target="_blank" rel="noopener noreferrer" class="link">{{ guide.guideText || '前往官方页面' }}</a>
    </p>
    <details v-if="flow === 'init-poll' && active" class="border border-ink-200 bg-paper/60 p-3">
      <summary class="cursor-pointer text-sm font-medium text-ink-700">手动粘贴回调地址</summary>
      <div class="mt-3 space-y-3">
        <label class="label" :for="`${inputID}-callback`">完整回调地址</label>
        <textarea :id="`${inputID}-callback`" v-model="code" class="input" rows="3" placeholder="粘贴官方授权后浏览器地址栏中的完整地址，无需提取 code" :disabled="busy" />
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy || disabled || !code.trim()" @click="submit">提交回调地址</button>
      </div>
    </details>
    <div v-else-if="flow === 'link-code'" class="space-y-3">
      <p class="hint">在官方页面完成授权后，粘贴授权码或完整回调地址。</p>
      <label class="label" for="code">授权码或回调地址</label>
      <textarea id="code" v-model="code" class="input" rows="3" placeholder="code#state 或 https://…?code=…&state=…" :disabled="busy" />
      <button type="button" class="xm-btn xm-btn-primary" :disabled="busy || disabled || !code.trim()" @click="submit">提交</button>
    </div>
  </section>
</template>
