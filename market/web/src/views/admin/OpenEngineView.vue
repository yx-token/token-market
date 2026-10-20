<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { request } from '@/api/client'

const route = useRoute()
const err = ref('')

function safeEnginePath(raw: unknown): string {
  const s = typeof raw === 'string' ? raw.trim() : ''
  if (!s.startsWith('/') || s.startsWith('//') || s.includes('://')) {
    return '/admin/dashboard'
  }
  return s
}

onMounted(async () => {
  const to = safeEnginePath(route.query.to)
  try {
    const data = await request<{
      access_token: string
      refresh_token?: string
      expires_in?: number
      user?: unknown
    }>('POST', '/api/admin/engine/session')
    localStorage.setItem('engine_auth_token', data.access_token)
    if (data.refresh_token) localStorage.setItem('engine_refresh_token', data.refresh_token)
    if (data.user) localStorage.setItem('engine_auth_user', JSON.stringify(data.user))
    if (data.expires_in) {
      localStorage.setItem('engine_token_expires_at', String(Date.now() + data.expires_in * 1000))
    }
    window.location.replace(to)
  } catch (e) {
    err.value = e instanceof Error ? e.message : '无法进入引擎'
  }
})
</script>

<template>
  <div class="open-engine">
    <p v-if="!err">正在进入引擎…</p>
    <p v-else class="err">{{ err }}</p>
  </div>
</template>

<style scoped>
.open-engine {
  min-height: 40vh;
  display: grid;
  place-items: center;
  color: var(--text-muted, #64748b);
  font-size: 14px;
}
.err {
  color: #b91c1c;
}
</style>
