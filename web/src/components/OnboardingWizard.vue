<script setup lang="ts">
// 接入上游：选择厂商与接入方式 → OAuth 授权或提交 Coding Plan key → 进入上架检测。
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { seller } from '@/api'
import type { Upstream, VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import ModalDialog from '@/components/ModalDialog.vue'
import { label } from '@/utils/labels'

const props = defineProps<{ vendors: VendorView[]; resume?: Upstream | null }>()
const emit = defineEmits<{ done: [] }>()
const open = defineModel<boolean>('open', { default: false })
const router = useRouter()
const { busy, run } = useAction()

const step = ref<'choose' | 'authorize' | 'done'>('choose')
const platform = ref('')
const authType = ref('')
const upstream = ref<Upstream | null>(null)
const authURL = ref('')
const code = ref('')
const apiKey = ref('')

const vendor = computed(() => props.vendors.find((v) => v.platform === platform.value))
const isOAuth = computed(() => !['coding', 'go'].includes(upstream.value?.auth_type ?? authType.value))

watch(open, (v) => {
  if (!v) return
  code.value = ''
  apiKey.value = ''
  authURL.value = ''
  if (props.resume) {
    upstream.value = props.resume
    platform.value = props.resume.platform
    authType.value = props.resume.auth_type
    step.value = 'authorize'
  } else {
    upstream.value = null
    platform.value = props.vendors[0]?.platform ?? ''
    authType.value = props.vendors[0]?.auth_types[0] ?? ''
    step.value = 'choose'
  }
})

watch(platform, () => {
  if (!vendor.value?.auth_types.includes(authType.value)) authType.value = vendor.value?.auth_types[0] ?? ''
})

async function create() {
  const u = await run(() => seller.createUpstream(platform.value, authType.value))
  if (!u) return
  upstream.value = u
  step.value = 'authorize'
  emit('done')
}

async function getLink() {
  if (!upstream.value) return
  const res = await run(() => seller.startOAuth(upstream.value!.id))
  if (res) {
    authURL.value = res.auth_url
    window.open(res.auth_url, '_blank', 'noopener')
  }
}

async function submit() {
  if (!upstream.value) return
  const id = upstream.value.id
  const u = await run(() => (isOAuth.value ? seller.completeOAuth(id, code.value.trim()) : seller.submitCodingKey(id, apiKey.value.trim())))
  if (u) {
    upstream.value = u
    step.value = 'done'
    emit('done')
  }
}

function goDetail() {
  open.value = false
  if (upstream.value) router.push(`/seller/upstreams/${upstream.value.id}`)
}
</script>

<template>
  <ModalDialog v-model:open="open" title="接入上游" wide>
    <template v-if="step === 'choose'">
      <p class="text-sm text-slate-500">只接受官方订阅账号（OAuth 授权）与国产 Coding Plan 订阅 key。平台为每个上游分配专用代理，账号凭证由底层网关保管。</p>
      <div v-if="vendors.length === 0" class="callout callout-warn">平台暂未开放任何厂商接入。</div>
      <div class="grid grid-cols-2 gap-2 md:grid-cols-3">
        <button
          v-for="v in vendors"
          :key="v.platform"
          type="button"
          :class="['choice', platform === v.platform && 'choice-on']"
          @click="platform = v.platform"
        >
          <div class="font-medium">{{ label('platform', v.platform).text }}</div>
          <div class="text-xs text-slate-500">寄售倍率 {{ v.consignment_min }}–{{ v.consignment_max }}×</div>
        </button>
      </div>
      <div v-if="vendor && vendor.auth_types.length > 1">
        <label class="label" for="auth">接入方式</label>
        <select id="auth" v-model="authType" class="input">
          <option v-for="t in vendor.auth_types" :key="t" :value="t">{{ label('authType', t).text }}</option>
        </select>
      </div>
    </template>

    <template v-else-if="step === 'authorize'">
      <div class="text-sm text-slate-600">
        上游 #{{ upstream?.id }} · {{ label('platform', platform).text }} · {{ label('authType', upstream?.auth_type ?? authType).text }}
      </div>
      <template v-if="isOAuth">
        <ol class="list-decimal space-y-2 pl-5 text-sm text-slate-600">
          <li>点击下面的按钮打开官方授权页面，用要挂出的账号登录并同意授权。</li>
          <li>授权完成后，把页面显示的授权码，或浏览器地址栏中的完整回调地址粘贴到下面。</li>
        </ol>
        <div class="flex flex-wrap items-center gap-2">
          <button class="btn-secondary" :disabled="busy" @click="getLink">{{ authURL ? '重新获取授权链接' : '获取授权链接' }}</button>
          <a v-if="authURL" :href="authURL" target="_blank" rel="noopener" class="link break-all text-xs">{{ authURL }}</a>
        </div>
        <div>
          <label class="label" for="code">授权码或回调地址</label>
          <textarea id="code" v-model="code" class="input" rows="3" placeholder="code#state 或 https://…?code=…&state=…" />
        </div>
      </template>
      <template v-else>
        <p class="text-sm text-slate-600">粘贴你的 Coding Plan 订阅 key。平台会向官方查询订阅窗口用量来确认这是订阅 key（不接受按量付费 key 或中转 key）。</p>
        <div>
          <label class="label" for="key">API Key</label>
          <input id="key" v-model="apiKey" class="input" autocomplete="off" />
        </div>
      </template>
    </template>

    <template v-else>
      <div v-if="upstream?.status === 'verifying'" class="callout callout-ok">
        授权完成，正在进行上架检测（通常几分钟内完成）。检测通过后就可以挂单了，检测报告可以在上游详情中查看。
      </div>
      <div v-else class="callout callout-warn">
        当前状态：{{ label('upstreamStatus', upstream?.status).text }}。{{ upstream?.failure_reason ?? '' }}
      </div>
    </template>

    <template #footer>
      <button class="btn-secondary" @click="open = false">关闭</button>
      <button v-if="step === 'choose'" class="btn-primary" :disabled="busy || !platform || !authType" @click="create">下一步</button>
      <button v-else-if="step === 'authorize'" class="btn-primary" :disabled="busy || (isOAuth ? !code.trim() : !apiKey.trim())" @click="submit">提交</button>
      <button v-else class="btn-primary" @click="goDetail">查看上游</button>
    </template>
  </ModalDialog>
</template>
