<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useAuth } from '@/stores/auth'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import AuthShell from '@/components/AuthShell.vue'

const auth = useAuth()
const router = useRouter()
const toast = useToast()
const email = ref('')
const password = ref('')
const confirm = ref('')
const { busy, run } = useAction()

async function submit() {
  if (password.value !== confirm.value) {
    toast.error('两次输入的密码不一致')
    return
  }
  const ok = await run(async () => {
    await authApi.register(email.value, password.value)
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
