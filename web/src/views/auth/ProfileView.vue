<script setup lang="ts">
import { ref } from 'vue'
import { auth as authApi } from '@/api'
import { useAuth } from '@/stores/auth'
import { useAction } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { dateTime } from '@/utils/format'

const auth = useAuth()
const realName = ref('')
const idNumber = ref('')
const { busy, run } = useAction()

async function submitKYC() {
  const ok = await run(() => authApi.submitKYC(realName.value, idNumber.value), '已提交，等待审核')
  if (ok) {
    realName.value = ''
    idNumber.value = ''
    await auth.reloadUser()
  }
}
</script>

<template>
  <div class="mx-auto max-w-2xl">
    <PageHeader title="账号" />
    <div class="card mb-5">
      <dl class="grid grid-cols-2 gap-3 text-sm">
        <dt class="text-slate-500">邮箱</dt>
        <dd>{{ auth.user?.email }}</dd>
        <dt class="text-slate-500">注册时间</dt>
        <dd>{{ dateTime(auth.user?.created_at) }}</dd>
        <dt class="text-slate-500">实名状态</dt>
        <dd><StatusBadge kind="kycStatus" :value="auth.user?.kyc_status" /></dd>
      </dl>
    </div>
    <div v-if="auth.user?.kyc_status !== 'verified'" class="card">
      <h2 class="card-title">实名认证</h2>
      <p class="mb-4 text-sm text-slate-500">卖家提现前需要完成实名认证。证件号加密存储，仅用于身份核验与防止重复认证。</p>
      <form class="space-y-4" @submit.prevent="submitKYC">
        <div>
          <label class="label" for="name">真实姓名</label>
          <input id="name" v-model="realName" class="input" required />
        </div>
        <div>
          <label class="label" for="id">身份证号</label>
          <input id="id" v-model="idNumber" class="input" required minlength="6" />
        </div>
        <button class="btn-primary" type="submit" :disabled="busy || auth.user?.kyc_status === 'pending'">
          {{ auth.user?.kyc_status === 'pending' ? '审核中' : '提交' }}
        </button>
      </form>
    </div>
  </div>
</template>
