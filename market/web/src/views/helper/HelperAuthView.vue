<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { buyer } from '@/api'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { syncHelperSession } from '@/utils/oneClickUse'

const route = useRoute()
const auth = useAuth()
const loginModal = useLoginModal()

const code = computed(() => String(route.query.code || '').trim().toLowerCase())
const phase = ref<'boot' | 'need-login' | 'ready' | 'working' | 'done' | 'error'>('boot')
const message = ref('正在准备授权…')
const errorText = ref('')

async function authorize() {
  if (!code.value) {
    phase.value = 'error'
    errorText.value = '缺少设备码，请从闲猫助手重新点「去登录」'
    return
  }
  if (!auth.loggedIn) {
    phase.value = 'need-login'
    message.value = '请先登录闲猫账号，再授权助手'
    loginModal.show({ redirect: `/helper-auth?code=${encodeURIComponent(code.value)}` })
    return
  }
  phase.value = 'working'
  message.value = '正在授权闲猫助手…'
  errorText.value = ''
  try {
    const out = await buyer.authorizeHelperAuth(code.value)
    // 顺带推本机桥，助手轮询到之前就能刷新顶栏
    if (auth.user) {
      void syncHelperSession({
        userId: auth.user.id,
        account: auth.user.email || out.account || '',
        apiBase: typeof location !== 'undefined' ? location.origin : '',
        retries: 2,
      })
    }
    phase.value = 'done'
    message.value = out.account
      ? `已授权「${out.account}」。可以回到闲猫助手窗口。`
      : '已授权。可以回到闲猫助手窗口。'
  } catch (e) {
    phase.value = 'error'
    errorText.value = e instanceof Error ? e.message : '授权失败'
    message.value = '授权失败'
  }
}

onMounted(async () => {
  if (!auth.loaded) await auth.load()
  if (!code.value) {
    phase.value = 'error'
    errorText.value = '缺少设备码，请从闲猫助手重新点「去登录」'
    return
  }
  if (!auth.loggedIn) {
    phase.value = 'need-login'
    message.value = '请先登录闲猫账号'
    loginModal.show({ redirect: `/helper-auth?code=${encodeURIComponent(code.value)}` })
    return
  }
  phase.value = 'ready'
  message.value = `将以 ${auth.user?.email || '当前账号'} 登录闲猫助手。请确认是你本人发起的登录后再点授权。`
})
</script>

<template>
  <main class="helper-auth">
    <section class="card">
      <img class="logo" src="/assets/xianmao-pet.svg" width="56" height="56" alt="闲猫" />
      <h1>授权闲猫助手</h1>
      <p class="lead">{{ message }}</p>
      <p v-if="code && phase !== 'done'" class="code-line">设备码 <code>{{ code }}</code></p>
      <p v-if="errorText" class="err">{{ errorText }}</p>

      <div class="actions">
        <button
          v-if="phase === 'need-login' || phase === 'ready' || phase === 'error'"
          type="button"
          class="btn-primary"
          @click="authorize"
        >
          {{ phase === 'need-login' ? '登录并授权' : phase === 'ready' ? '确认授权' : '重新授权' }}
        </button>
        <p v-else-if="phase === 'working'" class="muted">请稍候…</p>
        <p v-else-if="phase === 'done'" class="ok">授权完成，可关闭此页</p>
      </div>
    </section>
  </main>
</template>

<style scoped>
.helper-auth {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: #f5f5f5;
}
.card {
  width: min(420px, 100%);
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 20px;
  padding: 28px 24px 24px;
  text-align: center;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.06);
}
.logo {
  margin: 0 auto 12px;
}
h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: #2a2118;
}
.lead {
  margin: 10px 0 0;
  font-size: 14px;
  line-height: 1.5;
  color: #5c5348;
}
.code-line {
  margin: 12px 0 0;
  font-size: 13px;
  color: #5c5348;
}
.code-line code {
  display: inline-block;
  margin-left: 6px;
  padding: 2px 8px;
  border-radius: 8px;
  background: #f0ebe3;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  letter-spacing: 0.04em;
  font-weight: 700;
  color: #2a2118;
}
.err {
  margin: 10px 0 0;
  color: #b42318;
  font-size: 13px;
}
.ok {
  margin: 0;
  color: #1f5c3a;
  font-weight: 600;
}
.muted {
  margin: 0;
  color: #8a7f72;
}
.actions {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}
.btn-primary {
  appearance: none;
  border: 0;
  border-radius: 999px;
  padding: 10px 18px;
  font-size: 14px;
  font-weight: 600;
  color: #fff;
  background: #2a2118;
  cursor: pointer;
}
.btn-primary:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>
