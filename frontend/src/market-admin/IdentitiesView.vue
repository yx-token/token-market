<script setup lang="ts">
/**
 * 身份对照：TablePageLayout + DataTable；邮箱为主识别。
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

defineOptions({ name: 'MarketAdminIdentities' })

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
const loading = ref(false)

const LINK_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'linked', label: '已关联' },
  { value: 'unlinked', label: '未关联' },
]

const ROLE_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'admin', label: '管理员' },
  { value: 'user', label: '普通用户' },
]

const initQueryParams = { pageNum: 1, pageSize: 15, keyword: '', link: '', role: '' }
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'email', label: '邮箱', class: 'col-email' },
  { key: 'market', label: '市场状态', class: 'col-market' },
  { key: 'engine', label: '引擎关联', class: 'col-engine' },
  { key: 'shadow', label: '影子身份', class: 'col-shadow' },
  { key: 'actions', label: '同步', class: 'col-ops' },
]

const linkedCount = computed(() => rows.value.filter((r) => r.linked).length)

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.link === 'linked') list = list.filter((r) => r.linked)
  if (queryParams.value.link === 'unlinked') list = list.filter((r) => !r.linked)
  if (queryParams.value.role === 'admin') list = list.filter((r) => r.is_admin)
  if (queryParams.value.role === 'user') list = list.filter((r) => !r.is_admin)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((r) =>
      [r.email, r.market_user_id, r.engine_user_id, r.market_status]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

watch(
  () => [queryParams.value.keyword, queryParams.value.link, queryParams.value.role, queryParams.value.pageSize],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function needsPassword(row: IdentityRow) {
  return !row.linked || (!row.is_admin && !row.shadow_kind)
}

function done(row: IdentityRow) {
  return row.linked && (row.is_admin || !!row.shadow_kind)
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = await marketGet<IdentityRow[]>(`${MARKET_API_PREFIX}/identities`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
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
    let note = `${out.email} → 引擎用户已关联`
    if (!row.is_admin) {
      const bound = await marketSend<{ shadow_status?: string; engine_user_id?: number; token_ready?: boolean }>(
        'POST',
        `${MARKET_API_PREFIX}/identities/bind`,
        { email: row.email, password },
      )
      note += `，影子 ${bound.shadow_status || 'active'}`
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

onMounted(load)
</script>

<template>
  <section id="market-identities-page" class="identities-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">身份对照</h2>
          <p class="y-page-desc">市场用户按邮箱对齐引擎 users（已关联 {{ linkedCount }}/{{ rows.length }}）</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>
      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>
        <div class="section-block">
          <div class="section-title">身份信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="邮箱 / 状态"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">关联</label>
              <Select v-model="queryParams.link" class="flex-1 w-full min-w-0" size="sm" :options="LINK_OPTIONS" />
            </div>
            <div class="filter-item">
              <label class="filter-label">角色</label>
              <Select v-model="queryParams.role" class="flex-1 w-full min-w-0" size="sm" :options="ROLE_OPTIONS" />
            </div>
            <div class="filter-actions lg:col-span-4">
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
        <p class="hint-line">已存在同邮箱引擎用户时直接标记关联，不会改密码。未关联行需提供初始密码写入引擎 bcrypt。</p>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.market_user_id === row.market_user_id)) }}
          </template>
          <template #cell-email="{ row }">
            <div>{{ row.email }}</div>
            <div class="row-sub">{{ row.is_admin ? '管理员' : '普通用户' }}</div>
          </template>
          <template #cell-market="{ row }">{{ row.market_status || '—' }}</template>
          <template #cell-engine="{ row }">
            <span v-if="row.linked" class="status-cell">
              <i class="status-dot" data-tone="ok" />
              {{ row.engine_role || 'user' }} · {{ row.engine_status || '—' }}
            </span>
            <span v-else class="status-cell">
              <i class="status-dot" data-tone="idle" />
              未关联
            </span>
          </template>
          <template #cell-shadow="{ row }">
            <template v-if="row.shadow_kind">{{ row.shadow_kind }}/{{ row.shadow_status || '—' }}</template>
            <template v-else>—</template>
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <input
                v-if="needsPassword(row)"
                v-model="passwordDraft[row.email]"
                class="input pwd-input"
                type="password"
                placeholder="引擎登录密码"
                autocomplete="new-password"
              />
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="busyEmail === row.email || done(row)"
                @click="ensure(row)"
              >
                {{
                  done(row)
                    ? row.is_admin
                      ? '已同步'
                      : '已绑定'
                    : busyEmail === row.email
                      ? '同步中…'
                      : row.is_admin
                        ? '同步到引擎'
                        : '同步并绑定'
                }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无身份数据" description="调整筛选条件后重试" />
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
  </section>
</template>

<style scoped>
#market-identities-page.identities-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.page-heading { min-width: 0; }
.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.section-block { min-width: 0; }
.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
}
.hint-line {
  margin: 10px 0 0;
  font-size: 12px;
  color: #868686;
}
.row-sub {
  margin-top: 2px;
  font-size: 12px;
  color: #8c8c8c;
}
.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
  min-width: max-content;
}
.pwd-input {
  width: 9rem;
  height: 28px;
  font-size: 12px;
}
.btn-sm {
  height: 28px;
  padding: 0 10px;
  font-size: 12px;
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
.status-dot[data-tone='ok'] { background: #16a34a; }
#market-identities-page :deep(.filter-grid .select-trigger),
#market-identities-page :deep(.filter-grid .select-trigger-sm),
#market-identities-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-identities-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-identities-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
}
#market-identities-page :deep(th.col-ops),
#market-identities-page :deep(td.col-ops) {
  min-width: 16rem;
  width: 16rem;
  box-sizing: border-box;
}
#market-identities-page :deep(thead th.col-ops) { background: #f5f7fa !important; }
#market-identities-page :deep(tbody td.col-ops) { background: #fff !important; }
#market-identities-page :deep(tbody tr:hover td.col-ops) { background: #f5f7fa !important; }
</style>
