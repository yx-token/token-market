<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useAuth } from '@/stores/auth'
import { useConfig } from '@/stores/config'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { clearPromo, promoSignupFields } from '@/utils/promo'
import { clearRewardTicket, rewardSignupFields } from '@/utils/rewardTicket'
import AuthShell from '@/components/AuthShell.vue'

const auth = useAuth()
const router = useRouter()
const toast = useToast()
const cfg = useConfig()
const email = ref('')
const password = ref('')
const confirm = ref('')
const code = ref('')
const cooldown = ref(0)
const sending = ref(false)
const { busy, run } = useAction()
let timer: ReturnType<typeof setInterval> | null = null

const needCode = computed(() => !!cfg.config.email_verify_enabled)

onMounted(() => { void cfg.ensure() })
onUnmounted(() => { if (timer) clearInterval(timer) })

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
    toast.success('验证码已发送，请查收邮箱')
    startCooldown(res.cooldown_seconds || 60)
  } catch (e: unknown) {
    const msg = e && typeof e === 'object' && 'text' in e ? String((e as { text: string }).text) : '发送失败'
    toast.error(msg)
  } finally {
    sending.value = false
  }
}

async function submit() {
  if (password.value !== confirm.value) {
    toast.error('两次输入的密码不一致')
    return
  }
  if (needCode.value && !code.value.trim()) {
    toast.error('请填写邮箱验证码')
    return
  }
  const ok = await run(async () => {
    await authApi.register(email.value, password.value, {
      code: needCode.value ? code.value.trim() : undefined,
      ...promoSignupFields(),
      ...rewardSignupFields(),
    })
    // 归因只在注册时发生一次：成功后丢掉本地记录
    clearPromo()
    clearRewardTicket()
    await auth.login(email.value, password.value)
    return true
  }, '注册成功')
  if (ok) router.push('/')
}
</script>

<template>
  <div class="py-4">
    <AuthShell title="注册" subtitle="同一个账号既可以购买，也可以挂出自己闲置的订阅额度。">
      <form class="space-y-5" @submit.prevent="submit">
        <div>
          <label class="label" for="email">邮箱</label>
          <input id="email" v-model="email" class="input" type="email" autocomplete="email" required />
        </div>
        <div v-if="needCode">
          <label class="label" for="code">邮箱验证码</label>
          <div class="flex gap-2">
            <input
              id="code"
              v-model="code"
              class="input flex-1"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="6"
              placeholder="6 位数字"
              required
            />
            <button
              type="button"
              class="btn-secondary shrink-0 !px-3 whitespace-nowrap"
              :disabled="sending || cooldown > 0 || !email"
              @click="sendCode"
            >
              {{ cooldown > 0 ? `${cooldown}s` : sending ? '发送中…' : '获取验证码' }}
            </button>
          </div>
          <p class="hint">验证码将发到你的邮箱，10 分钟内有效</p>
        </div>
        <div class="grid grid-cols-1 gap-5 sm:grid-cols-2">
          <div>
            <label class="label" for="password">密码</label>
            <input id="password" v-model="password" class="input" type="password" autocomplete="new-password" minlength="8" required />
            <p class="hint">至少 8 位</p>
          </div>
          <div>
            <label class="label" for="confirm">确认密码</label>
            <input id="confirm" v-model="confirm" class="input" type="password" autocomplete="new-password" required />
          </div>
        </div>
        <button class="btn-primary w-full !py-2.5 !text-sm" type="submit" :disabled="busy">{{ busy ? '提交中…' : '注册' }}<span aria-hidden="true">→</span></button>
      </form>
      <p class="mt-8 border-t border-dashed border-ink-200 pt-5 text-[13px] text-ink-500">
        已有账号？<RouterLink to="/login" class="link">登录</RouterLink>
      </p>
    </AuthShell>
  </div>
</template>
