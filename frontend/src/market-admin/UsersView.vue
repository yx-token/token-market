<script setup lang="ts">
/**
 * 用户与实名：TablePageLayout + filter-grid + DataTable；邮箱为主要身份。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { sellerAccount } from './sellerDisplay'

defineOptions({ name: 'MarketAdminUsers' })

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

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'active', label: '正常' },
  { value: 'frozen', label: '冻结' },
]

const KYC_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'none', label: '未实名' },
  { value: 'pending', label: '待审' },
  { value: 'verified', label: '已通过' },
  { value: 'rejected', label: '已驳回' },
]

const ADMIN_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'yes', label: '超管' },
  { value: 'no', label: '普通' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  kyc_status: '',
  is_admin: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const tab = ref<'users' | 'kyc'>('users')
const users = ref<MarketUser[]>([])
const kycRows = ref<KYCItem[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

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

const userColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'email', label: '用户', class: 'col-user' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'kyc_status', label: 'KYC', class: 'col-kyc' },
  { key: 'is_admin', label: '超管', class: 'col-admin' },
  { key: 'engine', label: '引擎用户', class: 'col-engine' },
  { key: 'created_at', label: '注册时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const kycColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'email', label: '用户', class: 'col-user' },
  { key: 'real_name', label: '姓名', class: 'col-name' },
  { key: 'id_number', label: '证件号', class: 'col-idnum' },
  { key: 'submitted_at', label: '提交时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const columns = computed(() => (tab.value === 'users' ? userColumns : kycColumns))

const filteredUsers = computed(() => {
  let list = users.value
  if (queryParams.value.status) list = list.filter((u) => u.status === queryParams.value.status)
  if (queryParams.value.kyc_status) list = list.filter((u) => u.kyc_status === queryParams.value.kyc_status)
  if (queryParams.value.is_admin === 'yes') list = list.filter((u) => !!u.is_admin)
  if (queryParams.value.is_admin === 'no') list = list.filter((u) => !u.is_admin)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((u) =>
      [u.id, u.email, u.engine_user_id, sellerAccount(u.email, u.id)]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const filteredKyc = computed(() => {
  let list = kycRows.value
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((r) =>
      [r.user_id, r.email, r.real_name, r.id_number_masked, sellerAccount(r.email, r.user_id)]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const pageSource = computed(() => (tab.value === 'users' ? filteredUsers.value : filteredKyc.value))
const total = computed(() => pageSource.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return pageSource.value.slice(start, start + queryParams.value.pageSize) as Array<MarketUser & KYCItem>
})

const allStats = computed(() => {
  const all = users.value
  return {
    total: all.length,
    active: all.filter((u) => u.status === 'active').length,
    frozen: all.filter((u) => u.status === 'frozen').length,
    pendingKyc: all.filter((u) => u.kyc_status === 'pending').length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.status,
    queryParams.value.kyc_status,
    queryParams.value.is_admin,
    queryParams.value.pageSize,
    tab.value,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function userLabel(email?: string | null, id?: number | null) {
  return sellerAccount(email, id)
}

function statusLabel(s: string) {
  return ({ active: '正常', frozen: '冻结' } as Record<string, string>)[s] || s || '—'
}

function kycLabel(s: string) {
  return (
    ({ none: '未实名', pending: '待审', verified: '已通过', rejected: '已驳回' } as Record<string, string>)[s] ||
    s ||
    '—'
  )
}

function statusTone(s: string) {
  if (s === 'active') return 'ok'
  if (s === 'frozen') return 'err'
  return 'idle'
}

function fmtTime(v?: string | null) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function handleQuery() {
  queryParams.value.pageNum = 1
  load()
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'users') {
      const q = new URLSearchParams()
      if (queryParams.value.status) q.set('status', queryParams.value.status)
      if (queryParams.value.kyc_status) q.set('kyc_status', queryParams.value.kyc_status)
      if (queryParams.value.keyword.trim()) q.set('q', queryParams.value.keyword.trim())
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

function closeCreate() {
  showCreate.value = false
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
    closeCreate()
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

function closeEdit() {
  showEdit.value = false
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
    await run(`用户已保存`, () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/users/${editing.value!.id}`, body),
    )
  ) {
    closeEdit()
    const refreshed = users.value.find((x) => x.id === editing.value!.id)
    if (refreshed) editing.value = refreshed
  }
}

async function setStatus(u: MarketUser, status: string) {
  const label = userLabel(u.email, u.id)
  if (status === 'frozen' && !window.confirm(`冻结用户 ${label}？`)) return
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
  await run(approve ? '已通过' : '已驳回', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/kyc/${item.user_id}/review`, { approve, note }),
  )
}

function engineUserLink(id: number) {
  return `/admin/users?q=${id}`
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
})

onMounted(load)
</script>

<template>
  <section id="market-users-page" class="users-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'users' ? '用户列表' : '实名审核' }}</h2>
          <p class="y-page-desc">
            {{
              tab === 'users'
                ? '市场用户全字段编辑、冻解；引擎账号可跳转管理'
                : '待审实名资料，通过或驳回'
            }}
          </p>
        </div>
      </template>

      <template #actions>
        <button
          v-if="tab === 'users'"
          type="button"
          class="btn btn-primary page-action-btn"
          :disabled="busy"
          @click="openCreate"
        >
          <Icon name="plus" size="sm" />
          新建用户
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>

        <nav class="y-tabs mb-3">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'users' }" @click="tab = 'users'">
            用户列表
          </button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'kyc' }" @click="tab = 'kyc'">
            实名审核
          </button>
        </nav>

        <div v-if="tab === 'users'" class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">用户总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">正常</div>
                <div class="stat-card__value">{{ allStats.active.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">冻结</div>
                <div class="stat-card__value">{{ allStats.frozen.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">KYC 待审</div>
                <div class="stat-card__value">{{ allStats.pendingKyc.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block" :class="{ 'mt-3': tab === 'users' }">
          <div class="section-title">筛选条件</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  :placeholder="tab === 'users' ? '邮箱 / ID' : '邮箱 / 姓名 / 证件'"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div v-show="tab === 'users'" class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="STATUS_OPTIONS"
              />
            </div>
            <div v-show="tab === 'users'" class="filter-item">
              <label class="filter-label">KYC</label>
              <Select
                v-model="queryParams.kyc_status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="KYC_OPTIONS"
              />
            </div>
            <div v-show="tab === 'users'" class="filter-item">
              <label class="filter-label">角色</label>
              <Select
                v-model="queryParams.is_admin"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ADMIN_OPTIONS"
              />
            </div>
            <div class="filter-actions lg:col-span-4 sm:col-span-2">
              <button type="button" class="btn btn-secondary" @click="resetQuery">
                <Icon name="refresh" size="sm" />
                重置
              </button>
              <button type="button" class="btn btn-primary" @click="handleQuery">
                <Icon name="search" size="sm" />
                搜索
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{
              getSerialNumber(
                pageRows.findIndex((r) =>
                  tab === 'users' ? (r as MarketUser).id === (row as MarketUser).id : (r as KYCItem).user_id === (row as KYCItem).user_id,
                ),
              )
            }}
          </template>
          <template #cell-email="{ row }">
            <div>{{ userLabel(row.email, tab === 'users' ? row.id : row.user_id) }}</div>
          </template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ statusLabel(row.status) }}
            </span>
          </template>
          <template #cell-kyc_status="{ row }">{{ kycLabel(row.kyc_status) }}</template>
          <template #cell-is_admin="{ row }">{{ row.is_admin ? '是' : '否' }}</template>
          <template #cell-engine="{ row }">
            <a v-if="row.engine_user_id" class="y-link" :href="engineUserLink(row.engine_user_id)">
              已关联
            </a>
            <span v-else>未绑定</span>
          </template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-real_name="{ row }">{{ row.real_name || '—' }}</template>
          <template #cell-id_number="{ row }">{{ row.id_number_masked || '—' }}</template>
          <template #cell-submitted_at="{ row }">{{ fmtTime(row.submitted_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="tab === 'users'">
                <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
                <button
                  v-if="row.status !== 'frozen'"
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="setStatus(row, 'frozen')"
                >
                  冻结
                </button>
                <button v-else type="button" class="y-link" :disabled="busy" @click="setStatus(row, 'active')">
                  解冻
                </button>
              </template>
              <template v-else>
                <button type="button" class="y-link" :disabled="busy" @click="review(row, true)">通过</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="review(row, false)">
                  驳回
                </button>
              </template>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="tab === 'users' ? '暂无用户' : '暂无待审实名'"
              :description="tab === 'users' ? '调整筛选条件后重试，或新建用户' : '调整筛选条件后重试'"
              :action-text="tab === 'users' ? '新建用户' : undefined"
              @action="openCreate"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="queryParams.pageNum"
          :page-size="queryParams.pageSize"
          :page-size-options="[15, 20, 50]"
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>

    <div v-if="showCreate" class="y-modal-mask" @click.self="closeCreate">
      <div class="y-modal" role="dialog" aria-label="新建用户">
        <header class="y-modal__head">
          <h3>新建用户</h3>
          <button type="button" class="y-link" @click="closeCreate">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field"><span>邮箱</span><input v-model="createForm.email" class="input w-full" /></label>
          <label class="y-field">
            <span>初始密码</span>
            <input v-model="createForm.password" type="password" class="input w-full" />
          </label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="closeCreate">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="saveCreate">创建</button>
        </footer>
      </div>
    </div>

    <div v-if="showEdit && editing" class="y-modal-mask" @click.self="closeEdit">
      <div class="y-modal y-modal--wide" role="dialog" :aria-label="`编辑 ${userLabel(editing.email, editing.id)}`">
        <header class="y-modal__head">
          <h3>编辑 {{ userLabel(editing.email, editing.id) }}</h3>
          <button type="button" class="y-link" @click="closeEdit">关闭</button>
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
              <option value="active">正常</option>
              <option value="frozen">冻结</option>
            </select>
          </label>
          <label class="y-check"><input v-model="form.is_admin" type="checkbox" /> 平台超管 is_admin</label>
          <label class="y-field">
            <span>KYC 状态（纠偏）</span>
            <select v-model="form.kyc_status" class="input w-full">
              <option value="none">未实名（删除档案）</option>
              <option value="pending">待审</option>
              <option value="verified">已通过</option>
              <option value="rejected">已驳回</option>
            </select>
          </label>
          <label class="y-field"><span>KYC 备注</span><input v-model="form.kyc_note" class="input w-full" /></label>
          <p class="hint">
            引擎用户：
            <a v-if="editing.engine_user_id" class="y-link" :href="engineUserLink(editing.engine_user_id)">
              打开引擎账号（改余额/分组/Key）
            </a>
            <span v-else>未绑定，请到「身份对照」同步</span>
          </p>
          <p class="hint">
            连带：
            <RouterLink class="y-link" :to="`/admin/market/upstreams?seller_id=${editing.id}`">上游</RouterLink>
            ·
            <RouterLink class="y-link" :to="`/admin/market/listings?seller_id=${editing.id}`">挂单</RouterLink>
            ·
            <RouterLink class="y-link" to="/admin/market/recharges">充值</RouterLink>
            ·
            <RouterLink class="y-link" to="/admin/market/risk">风控</RouterLink>
          </p>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="closeEdit">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="saveEdit">保存</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
#market-users-page.users-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.page-heading {
  min-width: 0;
}

.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.section-block {
  min-width: 0;
}

.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  color: var(--color-text-primary, #262626);
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.stat-card {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}

.stat-card--blue {
  background: linear-gradient(135deg, #eff6ff, #f8fafc);
}
.stat-card--green {
  background: linear-gradient(135deg, #ecfdf5, #f8fafc);
}
.stat-card--orange {
  background: linear-gradient(135deg, #fff7ed, #f8fafc);
}
.stat-card--cyan {
  background: linear-gradient(135deg, #ecfeff, #f8fafc);
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.2;
}

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

.row-sub {
  margin-top: 2px;
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}
.status-dot[data-tone='ok'] {
  background: #16a34a;
}
.status-dot[data-tone='err'] {
  background: #ef4444;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

.hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}

#market-users-page :deep(.filter-grid .select-trigger),
#market-users-page :deep(.filter-grid .select-trigger-sm),
#market-users-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-users-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-users-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-users-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-users-page :deep(th.col-user),
#market-users-page :deep(td.col-user) {
  min-width: 12rem;
}
#market-users-page :deep(th.col-status),
#market-users-page :deep(td.col-status),
#market-users-page :deep(th.col-kyc),
#market-users-page :deep(td.col-kyc),
#market-users-page :deep(th.col-admin),
#market-users-page :deep(td.col-admin) {
  min-width: 5rem;
}
#market-users-page :deep(th.col-engine),
#market-users-page :deep(td.col-engine),
#market-users-page :deep(th.col-name),
#market-users-page :deep(td.col-name),
#market-users-page :deep(th.col-idnum),
#market-users-page :deep(td.col-idnum) {
  min-width: 6rem;
}
#market-users-page :deep(th.col-time),
#market-users-page :deep(td.col-time) {
  min-width: 9rem;
}
#market-users-page :deep(th.col-ops),
#market-users-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}
#market-users-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-users-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-users-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
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
  max-height: min(92vh, 800px);
  overflow: auto;
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

@media (max-width: 1100px) {
  .stats-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
</style>
