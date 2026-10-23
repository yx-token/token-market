<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

type IdentityRow = {
  market_user_id: number
  email: string
  is_admin: boolean
  market_status: string
  engine_user_id?: number
  engine_role?: string
  engine_status?: string
  shadow_kind?: string
  shadow_status?: string
  shadow_engine_user_id?: number
  linked: boolean
}

const rows = ref<IdentityRow[]>([])
const error = ref('')
const msg = ref('')
const busyEmail = ref('')
const passwordDraft = ref<Record<string, string>>({})

const linkedCount = computed(() => rows.value.filter((r) => r.linked).length)

async function load() {
  error.value = ''
  rows.value = await marketGet<IdentityRow[]>(`${MARKET_API_PREFIX}/identities`)
}

function needsPassword(row: IdentityRow) {
  return !row.linked || (!row.is_admin && !row.shadow_kind)
}

function done(row: IdentityRow) {
  return row.linked && (row.is_admin || !!row.shadow_kind)
}

async function ensure(row: IdentityRow) {
  error.value = ''
  msg.value = ''
  const password = (passwordDraft.value[row.email] || '').trim()
  if (needsPassword(row) && password.length < 6) {
    error.value = `${row.email}：请填写至少 6 位密码。买家还会用它绑定影子身份。`
    return
  }
  busyEmail.value = row.email
  try {
    const out = await marketSend<{ email: string; engine_user_id: number }>('POST', `${MARKET_API_PREFIX}/identities/ensure`, {
      email: row.email,
      password: password || 'noop-already-linked',
      role: row.is_admin ? 'admin' : 'user',
    })
    let note = `${out.email} → 引擎用户 #${out.engine_user_id}`
    if (!row.is_admin) {
      const bound = await marketSend<{ shadow_status?: string; engine_user_id?: number; token_ready?: boolean }>('POST', `${MARKET_API_PREFIX}/identities/bind`, {
        email: row.email,
        password,
      })
      note += `，影子 ${bound.shadow_status || 'active'} → #${bound.engine_user_id ?? out.engine_user_id}`
      note += bound.token_ready ? '，令牌已写入' : '，令牌未就绪'
    }
    msg.value = note
    delete passwordDraft.value[row.email]
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '同步失败'
  } finally {
    busyEmail.value = ''
  }
}

onMounted(async () => {
  try {
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  }
})
</script>

<template>
  <section class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">身份对照</h2>
        <p class="y-page-desc">
          市场用户按邮箱对齐引擎 users（已关联 {{ linkedCount }}/{{ rows.length }}）
        </p>
      </div>
      <button class="y-btn y-btn--ghost" type="button" @click="load">刷新</button>
    </header>
    <div class="y-container--tight py-4">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <div class="y-table-wrap overflow-x-auto">
        <table class="y-table w-full text-left text-sm">
          <thead>
            <tr>
              <th>邮箱</th>
              <th>市场</th>
              <th>引擎</th>
              <th>影子</th>
              <th>同步</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.market_user_id">
              <td>
                <div class="font-medium">{{ r.email }}</div>
                <div class="text-xs text-gray-500">#{{ r.market_user_id }}{{ r.is_admin ? ' · 管理员' : '' }}</div>
              </td>
              <td>{{ r.market_status }}</td>
              <td>
                <span v-if="r.linked" class="y-chip y-chip--ok">
                  #{{ r.engine_user_id }} · {{ r.engine_role }} · {{ r.engine_status }}
                </span>
                <span v-else class="y-chip y-chip--warn">未关联</span>
              </td>
              <td class="text-xs text-gray-500">
                <template v-if="r.shadow_kind">
                  {{ r.shadow_kind }}/{{ r.shadow_status || '—' }}
                  <span v-if="r.shadow_engine_user_id"> → #{{ r.shadow_engine_user_id }}</span>
                </template>
                <template v-else>—</template>
              </td>
              <td>
                <div class="flex flex-wrap items-center gap-2">
                  <input
                    v-if="needsPassword(r)"
                    v-model="passwordDraft[r.email]"
                    class="input w-36"
                    type="password"
                    placeholder="引擎登录密码"
                    autocomplete="new-password"
                  />
                  <button
                    class="y-btn y-btn--sm"
                    type="button"
                    :disabled="busyEmail === r.email || done(r)"
                    @click="ensure(r)"
                  >
                    {{ done(r) ? (r.is_admin ? '已同步' : '已绑定') : busyEmail === r.email ? '同步中…' : r.is_admin ? '同步到引擎' : '同步并绑定' }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="mt-3 text-xs text-gray-500">
        已存在同邮箱引擎用户时直接标记关联，不会改密。未关联行需提供初始密码写入引擎 bcrypt。
      </p>
    </div>
  </section>
</template>
