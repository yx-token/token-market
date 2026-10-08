<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useAuth } from '@/stores/auth'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'

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
  <div class="mx-auto mt-10 max-w-sm">
    <div class="card">
      <h1 class="card-title text-lg">注册</h1>
      <p class="mb-4 text-sm text-slate-500">同一个账号既可以购买，也可以挂出自己闲置的订阅额度。</p>
      <form class="space-y-4" @submit.prevent="submit">
        <div>
          <label class="label" for="email">邮箱</label>
          <input id="email" v-model="email" class="input" type="email" autocomplete="email" required />
        </div>
        <div>
          <label class="label" for="password">密码</label>
          <input id="password" v-model="password" class="input" type="password" autocomplete="new-password" minlength="8" required />
          <p class="hint">至少 8 位</p>
        </div>
        <div>
          <label class="label" for="confirm">确认密码</label>
          <input id="confirm" v-model="confirm" class="input" type="password" autocomplete="new-password" required />
        </div>
        <button class="btn-primary w-full" type="submit" :disabled="busy">{{ busy ? '提交中…' : '注册' }}</button>
      </form>
      <p class="mt-4 text-center text-sm text-slate-500">
        已有账号？<RouterLink to="/login" class="link">登录</RouterLink>
      </p>
    </div>
  </div>
</template>
