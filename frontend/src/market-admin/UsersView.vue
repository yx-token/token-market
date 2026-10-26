<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface KYCItem {
  user_id: number
  email: string
  real_name: string
  id_number_masked: string
  status: string
  submitted_at: string
}

interface MarketUser {
  id: number
  email: string
  is_admin: boolean
  status: string
  created_at?: string
  last_login_at?: string | null
  kyc_status: string
  engine_user_id?: number | null
}

const tab = ref<'users' | 'kyc'>('users')
const users = ref<MarketUser[]>([])
const kycRows = ref<KYCItem[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterStatus = ref('')
const filterKyc = ref('')

const showEdit = ref(false)
const editing = ref<MarketUser | null>(null)
const form = reactive({
  email: '',
  password: '',
  status: 'active',
  is_admin: false,
  kyc_status: 'none',
  kyc_note: '',
})

const showCreate = ref(false)
const createForm = reactive({ email: '', password: '' })

const filteredUsers = computed(() => {
  let list = users.value
  if (filterStatus.value) list = list.filter((u) => u.status === filterStatus.value)
  if (filterKyc.value) list = list.filter((u) => u.kyc_status === filterKyc.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    list = list.filter((u) =>
      [u.id, u.email, u.engine_user_id].map((v) => String(v ?? '').toLowerCase()).some((s) => s.includes(kw)),
    )
  }
  return list
})

const filteredKyc = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return kycRows.value
  return kycRows.value.filter((r) =>
    [r.user_id, r.email, r.real_name, r.id_number_masked]
      .map((v) => String(v ?? '').toLowerCase())
      .some((s) => s.includes(kw)),
  )
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'users') {
      const q = new URLSearchParams()
      if (filterStatus.value) q.set('status', filterStatus.value)
      if (filterKyc.value) q.set('kyc_status', filterKyc.value)
      if (keyword.value.trim()) q.set('q', keyword.value.trim())
      q.set('limit', '500')
      const qs = q.toString()
      users.value =
        (await marketGet<MarketUser[] | null>(`${MARKET_API_PREFIX}/users${qs ? `?${qs}` : ''}`)) ?? []
    } else {
      kycRows.value = (await marketGet<KYCItem[] | null>(`${MARKET_API_PREFIX}/kyc`)) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    users.value = []
    kycRows.value = []
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
    await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openCreate() {
  createForm.email = ''
  createForm.password = ''
  showCreate.value = true
}

async function saveCreate() {
  if (!createForm.email.trim() || !createForm.password) {
    error.value = '请填写邮箱与密码'
    return
  }
  if (
    await run('用户已创建', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/users`, {
        email: createForm.email.trim(),
        password: createForm.password,
      }),
    )
  ) {
    showCreate.value = false
  }
}

function openEdit(u: MarketUser) {
  editing.value = u
  Object.assign(form, {
    email: u.email,
    password: '',
    status: u.status || 'active',
    is_admin: !!u.is_admin,
    kyc_status: u.kyc_status || 'none',
    kyc_note: '',
  })
  showEdit.value = true
}

async function saveEdit() {
  if (!editing.value) return
  const body: Record<string, unknown> = {
    email: form.email.trim(),
    status: form.status,
    is_admin: form.is_admin,
    kyc_status: form.kyc_status,
    kyc_note: form.kyc_note,
  }
  if (form.password.trim()) body.password = form.password.trim()
  if (
    await run(`用户 #${editing.value.id} 已保存`, () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/users/${editing.value!.id}`, body),
    )
  ) {
    showEdit.value = false
    const refreshed = users.value.find((x) => x.id === editing.value!.id)
    if (refreshed) editing.value = refreshed
  }
}

async function setStatus(u: MarketUser, status: string) {
  if (status === 'frozen' && !window.confirm(`冻结用户 #${u.id}？`)) return
  await run(status === 'frozen' ? '已冻结' : '已解冻', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/users/${u.id}/status`, { status }),
  )
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
    /* refreshed */
  }
}

function engineUserLink(id: number) {
  return `/admin/users?q=${id}`
}

watch(tab, () => {
  keyword.value = ''
  load()
})
onMounted(load)
</script>

<template>
  <section id="market-users-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">用户管理</h2>
        <p class="y-page-desc">市场用户全字段编辑、KYC、冻解；引擎账号可跳转管理</p>
      </div>
      <div class="y-header-actions">
        <button v-if="tab === 'users'" type="button" class="y-btn" @click="openCreate">新建用户</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'users' }" @click="tab = 'users'">用户列表</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'kyc' }" @click="tab = 'kyc'">实名审核</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input
              v-model="keyword"
              class="filter-input w-full"
              type="search"
              :placeholder="tab === 'users' ? '邮箱 / ID' : '用户 / 邮箱 / 姓名'"
              @keyup.enter="load"
            />
          </div>
        </div>
        <div v-show="tab === 'users'" class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="filterStatus" class="filter-input flex-1">
            <option value="">全部</option>
            <option value="active">active</option>
            <option value="frozen">frozen</option>
          </select>
        </div>
        <div v-show="tab === 'users'" class="filter-item">
          <label class="filter-label">KYC</label>
          <select v-model="filterKyc" class="filter-input flex-1">
            <option value="">全部</option>
            <option value="none">none</option>
            <option value="pending">pending</option>
            <option value="verified">verified</option>
            <option value="rejected">rejected</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button
            type="button"
            class="btn btn-secondary"
            @click="
              keyword = '';
              filterStatus = '';
              filterKyc = '';
              load()
            "
          >
            重置
          </button>
          <button type="button" class="btn btn-primary" @click="load">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'users'">
        <p v-if="!filteredUsers.length" class="y-msg">暂无用户</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>邮箱</th>
              <th>状态</th>
              <th>KYC</th>
              <th>超管</th>
              <th>引擎用户</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in filteredUsers" :key="u.id">
              <td>#{{ u.id }}</td>
              <td>{{ u.email }}</td>
              <td>
                <span :class="u.status === 'active' ? 'y-chip y-chip--ok' : 'y-chip y-chip--err'">{{ u.status }}</span>
              </td>
              <td>{{ u.kyc_status }}</td>
              <td>{{ u.is_admin ? '是' : '否' }}</td>
              <td>
                <a v-if="u.engine_user_id" class="y-link" :href="engineUserLink(u.engine_user_id)">
                  #{{ u.engine_user_id }}
                </a>
                <span v-else>—</span>
              </td>
              <td class="y-col-op">
                <button type="button" class="y-link" @click="openEdit(u)">编辑</button>
                <button
                  v-if="u.status !== 'frozen'"
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="setStatus(u, 'frozen')"
                >
                  冻结
                </button>
                <button v-else type="button" class="y-link" :disabled="busy" @click="setStatus(u, 'active')">
                  解冻
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ filteredUsers.length }} 条</footer>
      </template>

      <template v-else>
        <p v-if="!filteredKyc.length" class="y-msg">暂无待审实名</p>
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
            <tr v-for="row in filteredKyc" :key="row.user_id">
              <td>#{{ row.user_id }}</td>
              <td>{{ row.email }}</td>
              <td>{{ row.real_name || '—' }}</td>
              <td>{{ row.id_number_masked || '—' }}</td>
              <td>{{ row.submitted_at || '—' }}</td>
              <td class="y-col-op">
                <button type="button" class="y-link" :disabled="busy" @click="review(row, true)">通过</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="review(row, false)">驳回</button>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ filteredKyc.length }} 条</footer>
      </template>
    </div>

    <div v-if="showCreate" class="y-modal-mask" @click.self="showCreate = false">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>新建用户</h3>
          <button type="button" class="y-link" @click="showCreate = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field"><span>邮箱</span><input v-model="createForm.email" class="input w-full" /></label>
          <label class="y-field">
            <span>初始密码</span>
            <input v-model="createForm.password" type="password" class="input w-full" />
          </label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showCreate = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="saveCreate">创建</button>
        </footer>
      </div>
    </div>

    <div v-if="showEdit && editing" class="y-modal-mask" @click.self="showEdit = false">
      <div class="y-modal y-modal--wide">
        <header class="y-modal__head">
          <h3>编辑用户 #{{ editing.id }}</h3>
          <button type="button" class="y-link" @click="showEdit = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field"><span>邮箱</span><input v-model="form.email" class="input w-full" /></label>
          <label class="y-field">
            <span>新密码（留空不改）</span>
            <input v-model="form.password" type="password" class="input w-full" autocomplete="new-password" />
          </label>
          <label class="y-field">
            <span>状态</span>
            <select v-model="form.status" class="input w-full">
              <option value="active">active</option>
              <option value="frozen">frozen</option>
            </select>
          </label>
          <label class="y-check"><input v-model="form.is_admin" type="checkbox" /> 平台超管 is_admin</label>
          <label class="y-field">
            <span>KYC 状态（纠偏）</span>
            <select v-model="form.kyc_status" class="input w-full">
              <option value="none">none（删除档案）</option>
              <option value="pending">pending</option>
              <option value="verified">verified</option>
              <option value="rejected">rejected</option>
            </select>
          </label>
          <label class="y-field"><span>KYC 备注</span><input v-model="form.kyc_note" class="input w-full" /></label>
          <p class="hint">
            引擎用户：
            <a v-if="editing.engine_user_id" class="y-link" :href="engineUserLink(editing.engine_user_id)">
              #{{ editing.engine_user_id }}（改余额/分组/Key）
            </a>
            <span v-else>未绑定，请到「身份对照」ensure/bind</span>
          </p>
          <p class="hint">
            连带：
            <RouterLink class="y-link" :to="`/admin/market/upstreams?q=${editing.id}`">上游</RouterLink>
            ·
            <RouterLink class="y-link" :to="`/admin/market/listings?q=${editing.id}`">挂单</RouterLink>
            ·
            <RouterLink class="y-link" to="/admin/market/recharges">充值</RouterLink>
            ·
            <RouterLink class="y-link" to="/admin/market/risk">风控</RouterLink>
          </p>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showEdit = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="saveEdit">保存</button>
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
.hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
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
.y-modal--wide {
  width: min(520px, 100%);
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
.y-field > span:first-child {
  color: var(--color-text-secondary, #868686);
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
</style>
