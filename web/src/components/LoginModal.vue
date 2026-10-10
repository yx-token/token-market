<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useAction } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'

const modal = useLoginModal()
const auth = useAuth()
const router = useRouter()
const email = ref('')
const password = ref('')
const catRun = ref(false)
const { busy, run } = useAction()

watch(
  () => modal.open,
  async (v) => {
    catRun.value = false
    if (v) {
      await nextTick()
      requestAnimationFrame(() => {
        catRun.value = true
      })
    } else {
      email.value = ''
      password.value = ''
    }
  },
)

async function submit() {
  const ok = await run(async () => {
    if (modal.mode === 'register') {
      await authApi.register(email.value, password.value)
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
        <div class="xm-login-card">
          <div class="xm-login-cat" :class="{ 'is-run': catRun }" aria-hidden="true">
            <img src="/assets/xianmao-pet-lie.svg" alt="" width="280" height="168" />
          </div>
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
