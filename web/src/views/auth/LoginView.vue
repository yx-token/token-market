<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useAction } from '@/composables/useAsync'

const auth = useAuth()
const router = useRouter()
const route = useRoute()
const email = ref('')
const password = ref('')
const { busy, run } = useAction()

async function submit() {
  const ok = await run(async () => {
    await auth.login(email.value, password.value)
    return true
  })
  if (ok) router.push(typeof route.query.redirect === 'string' ? route.query.redirect : '/')
}
</script>

<template>
  <div class="mx-auto mt-10 max-w-sm">
    <div class="card">
      <h1 class="card-title text-lg">登录</h1>
      <form class="space-y-4" @submit.prevent="submit">
        <div>
          <label class="label" for="email">邮箱</label>
          <input id="email" v-model="email" class="input" type="email" autocomplete="email" required />
        </div>
        <div>
          <label class="label" for="password">密码</label>
          <input id="password" v-model="password" class="input" type="password" autocomplete="current-password" required />
        </div>
        <button class="btn-primary w-full" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
      </form>
      <p class="mt-4 text-center text-sm text-slate-500">
        还没有账号？<RouterLink to="/register" class="link">注册</RouterLink>
      </p>
    </div>
  </div>
</template>
