<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface KYCItem {
  user_id: number
  email: string
  real_name: string
  id_number_masked: string
  status: string
  submitted_at: string
}

interface OrgUser {
  id: number
  email: string
  status?: string
}

const rows = ref<KYCItem[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const showFreeze = ref(false)
const freezeId = ref('')
const freezeQuery = ref('')
const userHits = ref<OrgUser[]>([])
const userLoading = ref(false)

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return rows.value
  return rows.value.filter((r) =>
    [r.user_id, r.email, r.real_name, r.id_number_masked]
      .map((v) => String(v ?? '').toLowerCase())
      .some((s) => s.includes(kw)),
  )
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = (await marketGet<KYCItem[] | null>(`${MARKET_API_PREFIX}/kyc`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

async function review(item: KYCItem, approve: boolean) {
  let note = ''
  if (!approve) {
    const input = window.prompt('驳回原因')
    if (input == null) return
    note = input
  }
  if (
    await run(approve ? '已通过' : '已驳回', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/kyc/${item.user_id}/review`, { approve, note }),
    )
  ) {
    await load()
  }
}

function openFreeze(prefill?: number) {
  freezeId.value = prefill != null ? String(prefill) : ''
  freezeQuery.value = ''
  userHits.value = []
  showFreeze.value = true
}

async function searchUsers() {
  const q = freezeQuery.value.trim()
  if (!q) {
    userHits.value = []
    return
  }
  userLoading.value = true
  try {
    userHits.value =
      (await marketGet<OrgUser[] | null>(
        `${MARKET_API_PREFIX}/org/users?q=${encodeURIComponent(q)}`,
      )) ?? []
  } catch {
    userHits.value = []
  } finally {
    userLoading.value = false
  }
}

async function setStatus(status: string) {
  const id = Number(freezeId.value)
  if (!id) {
    error.value = '请填写用户 ID'
    return
  }
  if (status === 'frozen' && !window.confirm(`冻结用户 #${id}？冻结后无法登录与调用。`)) return
  if (
    await run(status === 'frozen' ? '已冻结' : '已解冻', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/users/${id}/status`, { status }),
    )
  ) {
    showFreeze.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-users-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">用户与实名</h2>
        <p class="y-page-desc">C 端买家/卖家：实名审核、冻结/解冻</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn y-btn--ghost" @click="openFreeze()">冻结 / 解冻</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <div class="y-search">
        <input v-model="keyword" type="search" placeholder="用户 / 邮箱 / 姓名" />
      </div>
      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!filtered.length" class="y-msg">暂无待审实名</p>
      <table v-else>
        <thead>
          <tr>
            <th>用户</th>
            <th>邮箱</th>
            <th>姓名</th>
            <th>证件号</th>
            <th>提交时间</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in filtered" :key="row.user_id">
            <td>#{{ row.user_id }}</td>
            <td>{{ row.email }}</td>
            <td>{{ row.real_name || '—' }}</td>
            <td>{{ row.id_number_masked || '—' }}</td>
            <td>{{ row.submitted_at || '—' }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="review(row, true)">通过</button>
              <button type="button" class="y-link" :disabled="busy" @click="review(row, false)">驳回</button>
              <button type="button" class="y-link" @click="openFreeze(row.user_id)">冻结</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="!loading" class="y-footer">共 {{ filtered.length }} 条</footer>

    <div v-if="showFreeze" class="y-modal-mask" @click.self="showFreeze = false">
      <div class="y-modal" role="dialog" aria-label="冻结解冻">
        <header class="y-modal__head">
          <h3>冻结 / 解冻用户</h3>
          <button type="button" class="y-link" @click="showFreeze = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>用户 ID</span>
            <input v-model="freezeId" class="input w-full" inputmode="numeric" />
          </label>
          <label class="y-field">
            <span>按邮箱搜索</span>
            <div class="search-row">
              <input v-model="freezeQuery" class="input w-full" placeholder="邮箱关键字" @keyup.enter="searchUsers" />
              <button type="button" class="y-btn y-btn--ghost" :disabled="userLoading" @click="searchUsers">搜</button>
            </div>
          </label>
          <ul v-if="userHits.length" class="hit-list">
            <li v-for="u in userHits" :key="u.id">
              <button type="button" class="y-link" @click="freezeId = String(u.id)">
                #{{ u.id }} · {{ u.email }}{{ u.status && u.status !== 'active' ? ` · ${u.status}` : '' }}
              </button>
            </li>
          </ul>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" :disabled="busy" @click="setStatus('active')">解冻</button>
          <button type="button" class="y-btn" :disabled="busy" @click="setStatus('frozen')">冻结</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.y-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}
.y-modal {
  width: min(440px, 100%);
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head,
.y-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__foot {
  justify-content: flex-end;
}
.y-modal__body {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.search-row {
  display: flex;
  gap: 8px;
}
.hit-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 6px;
  max-height: 160px;
  overflow: auto;
}
</style>
