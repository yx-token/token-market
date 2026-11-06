<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useAction } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useConfig } from '@/stores/config'
import { useLoginModal } from '@/stores/loginModal'
import { useToast } from '@/stores/toast'
import { clearPromo, promoSignupFields } from '@/utils/promo'
import { clearRewardTicket, rewardSignupFields } from '@/utils/rewardTicket'

const modal = useLoginModal()
const auth = useAuth()
const cfg = useConfig()
const toast = useToast()
const router = useRouter()
const email = ref('')
const password = ref('')
const code = ref('')
const cooldown = ref(0)
const sending = ref(false)
const catRun = ref(false)
const { busy, run } = useAction()
let timer: ReturnType<typeof setInterval> | null = null

const needCode = computed(() => modal.mode === 'register' && !!cfg.config.email_verify_enabled)

watch(
  () => modal.open,
  async (v) => {
    catRun.value = false
    if (v) {
      void cfg.ensure()
      await nextTick()
      requestAnimationFrame(() => {
        catRun.value = true
      })
    } else {
      email.value = ''
      password.value = ''
      code.value = ''
      cooldown.value = 0
      if (timer) {
        clearInterval(timer)
        timer = null
      }
    }
  },
  { immediate: true },
)

onUnmounted(() => {
  if (timer) clearInterval(timer)
})

function startCooldown(sec: number) {
  cooldown.value = sec
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    cooldown.value -= 1
    if (cooldown.value <= 0 && timer) {
      clearInterval(timer)
      timer = null
    }
  }, 1000)
}

async function sendCode() {
  if (!email.value.trim()) {
    toast.error('请先填写邮箱')
    return
  }
  sending.value = true
  try {
    const res = await authApi.sendRegisterCode(email.value.trim())
    toast.success('验证码已发送')
    startCooldown(res.cooldown_seconds || 60)
  } catch (e: unknown) {
    const msg = e && typeof e === 'object' && 'text' in e ? String((e as { text: string }).text) : '发送失败'
    toast.error(msg)
  } finally {
    sending.value = false
  }
}

async function submit() {
  if (needCode.value && !code.value.trim()) {
    toast.error('请填写邮箱验证码')
    return
  }
  const ok = await run(async () => {
    if (modal.mode === 'register') {
      await authApi.register(email.value, password.value, {
        code: needCode.value ? code.value.trim() : undefined,
        ...promoSignupFields(),
        ...rewardSignupFields(),
      })
      // 归因只在注册时发生一次：注册成功后丢掉本地记录，保留 tm_visitor_id
      clearPromo()
      // 票据已随注册绑定（算新用户），不要再让登录后的绑定按老用户处理
      clearRewardTicket()
    }
    await auth.login(email.value, password.value)
    return true
  })
  if (!ok) return
  const dest = modal.redirect || '/'
  modal.hide()
  if (router.currentRoute.value.fullPath !== dest) {
    await router.push(dest)
  }
}

function switchMode() {
  modal.mode = modal.mode === 'login' ? 'register' : 'login'
  code.value = ''
}

function onMaskClick(e: MouseEvent) {
  if (e.target === e.currentTarget) modal.hide()
}
</script>

<template>
  <Teleport to="body">
    <div
      class="xm-login-mask"
      :class="{ 'is-open': modal.open }"
      role="dialog"
      aria-modal="true"
      aria-labelledby="xm-login-title"
      :aria-hidden="modal.open ? 'false' : 'true'"
      @click="onMaskClick"
    >
      <div class="xm-login-wrap" @click.stop>
        <div class="xm-login-cat" :class="{ 'is-run': catRun }" aria-hidden="true">
          <img src="/assets/xianmao-pet-lie.svg" alt="" width="280" height="168" />
        </div>
        <div class="xm-login-card">
          <button type="button" class="xm-login-close" aria-label="关闭" @click="modal.hide()">×</button>
          <h2 id="xm-login-title">{{ modal.mode === 'login' ? '欢迎来闲猫' : '注册闲猫账号' }}</h2>
          <p class="sub">{{ modal.mode === 'login' ? '登录后买卖挂单、充值调用' : '同一个账号既能买也能卖' }}</p>
          <form @submit.prevent="submit">
            <div class="xm-login-field">
              <label for="xm-login-email">邮箱</label>
              <input
                id="xm-login-email"
                v-model="email"
                type="email"
                autocomplete="email"
                placeholder="请输入邮箱"
                required
              />
            </div>
            <div v-if="needCode" class="xm-login-field">
              <label for="xm-login-code">邮箱验证码</label>
              <div class="xm-login-code-row">
                <input
                  id="xm-login-code"
                  v-model="code"
                  type="text"
                  inputmode="numeric"
                  autocomplete="one-time-code"
                  maxlength="6"
                  placeholder="6 位验证码"
                  required
                />
                <button
                  type="button"
                  class="xm-login-code-btn"
                  :disabled="sending || cooldown > 0 || !email"
                  @click="sendCode"
                >
                  {{ cooldown > 0 ? `${cooldown}s` : sending ? '…' : '获取验证码' }}
                </button>
              </div>
            </div>
            <div class="xm-login-field">
              <label for="xm-login-pass">密码</label>
              <input
                id="xm-login-pass"
                v-model="password"
                type="password"
                :autocomplete="modal.mode === 'login' ? 'current-password' : 'new-password'"
                placeholder="请输入密码"
                required
                minlength="8"
              />
            </div>
            <button class="xm-login-submit" type="submit" :disabled="busy">
              {{ busy ? '处理中…' : modal.mode === 'login' ? '登录' : '注册并登录' }}
            </button>
          </form>
          <div class="xm-login-foot">
            <button type="button" class="linkish" @click="switchMode">
              {{ modal.mode === 'login' ? '没有账号？注册' : '已有账号？登录' }}
            </button>
            <button type="button" class="linkish" @click="modal.hide()">先逛逛</button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
