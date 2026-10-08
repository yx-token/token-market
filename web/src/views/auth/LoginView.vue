<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/stores/auth'
import { useAction } from '@/composables/useAsync'
import AuthShell from '@/components/AuthShell.vue'

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
  <div class="py-4">
    <AuthShell title="登录" subtitle="欢迎回来。">
      <form class="space-y-5" @submit.prevent="submit">
        <div>
          <label class="label" for="email">邮箱</label>
          <input id="email" v-model="email" class="input" type="email" autocomplete="email" required />
        </div>
        <div>
          <label class="label" for="password">密码</label>
          <input id="password" v-model="password" class="input" type="password" autocomplete="current-password" required />
        </div>
        <button class="btn-primary w-full !py-2.5 !text-sm" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}<span aria-hidden="true">→</span></button>
      </form>
      <p class="mt-8 border-t border-dashed border-ink-200 pt-5 text-[13px] text-ink-500">
        还没有账号？<RouterLink to="/register" class="link">注册一个</RouterLink>
      </p>
    </AuthShell>
  </div>
</template>
