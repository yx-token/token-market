<script setup lang="ts">
/**
 * 评价管理：列表筛选、隐藏/恢复、删除。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { sellerAccount, sellerFilterLabel } from './sellerDisplay'

defineOptions({ name: 'MarketAdminReviews' })

interface ReviewItem {
  id: number
  buyer_id: number
  buyer_mask?: string
  seller_id: number
  listing_id?: number | null
  rating?: number | null
  content: string
  tags: string[]
  status: string
  created_at: string
  hide_reason?: string | null
  follow_up?: ReviewItem | null
}

interface MarketUser {
  id: number
  email: string
}

const items = ref<ReviewItem[]>([])
const totalServer = ref(0)
const userEmailById = ref<Record<number, string>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const pendingAction = ref<{ action: 'hide' | 'delete'; review: ReviewItem } | null>(null)
const detailReview = ref<ReviewItem | null>(null)
const actionMessage = computed(() => {
  const pending = pendingAction.value
  if (!pending) return ''
  const target = `评价 #${pending.review.id}，商家 ${userLabel(pending.review.seller_id)}`
  return pending.action === 'hide'
    ? `确认隐藏${target}？`
    : `确认删除${target}？删除后不计入均分。`
})

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'visible', label: '公开' },
  { value: 'hidden', label: '已隐藏' },
  { value: 'deleted', label: '已删除' },
]

const RATING_OPTIONS = [
  { value: '', label: '全部' },
  { value: '5', label: '5 星' },
  { value: '4', label: '4 星' },
  { value: '3', label: '3 星' },
  { value: '2', label: '2 星' },
  { value: '1', label: '1 星' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  rating: '',
  seller_id: '',
  buyer_id: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: '编号', class: 'col-id' },
  { key: 'seller', label: '商家', class: 'col-user' },
  { key: 'buyer', label: '买家', class: 'col-user' },
  { key: 'rating', label: '星级', class: 'col-sev' },
  { key: 'content', label: '内容', class: 'col-detail' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'created_at', label: '时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

function userLabel(id?: number | null) {
  if (!id) return '—'
  return sellerAccount(userEmailById.value[id], id)
}

function statusLabel(s: string) {
  return ({ visible: '公开', hidden: '已隐藏', deleted: '已删除' } as Record<string, string>)[s] || s
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function contentText(r: ReviewItem) {
  const tags = (r.tags ?? []).filter(Boolean).join(' · ')
  const body = (r.content || '').trim() || '（无短评）'
  return tags ? `${body}｜${tags}` : body
}

const pageRows = computed(() => items.value)
const total = computed(() => totalServer.value)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function buildQuery() {
  const q = new URLSearchParams()
  q.set('limit', String(queryParams.value.pageSize))
  q.set('offset', String((queryParams.value.pageNum - 1) * queryParams.value.pageSize))
  if (queryParams.value.status) q.set('status', queryParams.value.status)
  if (queryParams.value.keyword.trim()) q.set('q', queryParams.value.keyword.trim())
  if (queryParams.value.seller_id.trim()) q.set('seller_id', queryParams.value.seller_id.trim())
  if (queryParams.value.buyer_id.trim()) q.set('buyer_id', queryParams.value.buyer_id.trim())
  if (queryParams.value.rating) {
    q.set('min_rating', queryParams.value.rating)
    q.set('max_rating', queryParams.value.rating)
  }
  return q.toString()
}

async function loadUsers() {
  try {
    const users = (await marketGet<MarketUser[] | null>(`${MARKET_API_PREFIX}/users?limit=500`)) ?? []
    const map: Record<number, string> = {}
    for (const u of users) map[u.id] = u.email
    userEmailById.value = map
  } catch {
    /* ignore */
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await marketGet<{ items: ReviewItem[] | null; total: number }>(
      `${MARKET_API_PREFIX}/reviews?${buildQuery()}`,
    )
    items.value = res.items ?? []
    totalServer.value = Number(res.total ?? items.value.length)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    items.value = []
    totalServer.value = 0
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
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function hideReview(r: ReviewItem) {
  if (!busy.value) pendingAction.value = { action: 'hide', review: r }
}

function unhideReview(r: ReviewItem) {
  return run('已恢复公开', () => marketSend('POST', `${MARKET_API_PREFIX}/reviews/${r.id}/unhide`))
}

function deleteReview(r: ReviewItem) {
  if (!busy.value) pendingAction.value = { action: 'delete', review: r }
}

async function confirmAction(reason = '') {
  const pending = pendingAction.value
  if (!pending || busy.value) return
  pendingAction.value = null
  const action = pending.action === 'hide' ? 'hide' : 'delete'
  await run(pending.action === 'hide' ? '已隐藏' : '已删除', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/reviews/${pending.review.id}/${action}`, { reason }),
  )
}

function handleQuery() {
  queryParams.value.pageNum = 1
  load()
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
}

watch(
  () => [queryParams.value.pageSize, queryParams.value.status, queryParams.value.rating],
  () => {
    if (queryParams.value.pageNum !== 1) {
      queryParams.value.pageNum = 1
      return
    }
    load()
  },
)

watch(
  () => queryParams.value.pageNum,
  () => load(),
)

onMounted(async () => {
  await loadUsers()
  await load()
})
</script>

<template>
  <section id="market-reviews-page" class="reviews-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">评价管理</h2>
          <p class="y-page-desc">买家对商家的星评与短评；可隐藏违规内容或删除（删除后不计入均分）</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>
      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2" role="alert">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2" role="status" aria-live="polite">{{ msg }}</p>
        <div class="section-block">
          <div class="section-title">筛选条件</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label" for="review-keyword">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  id="review-keyword"
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="短评内容"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label" for="review-status">状态</label>
              <Select id="review-status" v-model="queryParams.status" class="flex-1 w-full min-w-0" size="sm" :options="STATUS_OPTIONS" aria-label="评价状态" />
            </div>
            <div class="filter-item">
              <label class="filter-label" for="review-rating">星级</label>
              <Select id="review-rating" v-model="queryParams.rating" class="flex-1 w-full min-w-0" size="sm" :options="RATING_OPTIONS" aria-label="评价星级" />
            </div>
            <div class="filter-item">
              <label class="filter-label" for="review-seller-id">商家 ID</label>
              <div class="relative flex-1 w-full min-w-0">
                <input id="review-seller-id" v-model="queryParams.seller_id" class="filter-input w-full" placeholder="可选" @keyup.enter="handleQuery" />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label" for="review-buyer-id">买家 ID</label>
              <div class="relative flex-1 w-full min-w-0">
                <input id="review-buyer-id" v-model="queryParams.buyer_id" class="filter-input w-full" placeholder="可选" @keyup.enter="handleQuery" />
              </div>
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
      </template>
      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">{{ row.id }}</template>
          <template #cell-seller="{ row }">
            <span :title="sellerFilterLabel(userEmailById[row.seller_id], row.seller_id)">
              {{ userLabel(row.seller_id) }}
            </span>
          </template>
          <template #cell-buyer="{ row }">
            <span>{{ row.buyer_mask || userLabel(row.buyer_id) }}</span>
          </template>
          <template #cell-rating="{ row }">
            <span v-if="row.rating">{{ row.rating }} ★</span>
            <span v-else>—</span>
          </template>
          <template #cell-content="{ row }">
            <button type="button" class="detail-cell detail-button" :aria-label="`查看评价 #${row.id} 详情`" @click="detailReview = row">
              <span class="detail-cell__text">{{ contentText(row) }}</span>
              <span class="detail-cell__action">查看详情</span>
            </button>
            <div v-if="row.follow_up" class="follow-hint">有追评</div>
            <div v-if="row.listing_id" class="follow-hint">挂单 #{{ row.listing_id }}</div>
          </template>
          <template #cell-status="{ row }">
            <span>{{ statusLabel(row.status) }}</span>
            <div v-if="row.hide_reason" class="follow-hint">{{ row.hide_reason }}</div>
          </template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="row.status === 'visible'">
                <button type="button" class="y-link" :disabled="busy" @click="hideReview(row)">隐藏</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="deleteReview(row)">删除</button>
              </template>
              <template v-else-if="row.status === 'hidden'">
                <button type="button" class="y-link" :disabled="busy" @click="unhideReview(row)">恢复</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="deleteReview(row)">删除</button>
              </template>
              <span v-else>—</span>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无评价" description="调整筛选条件后重试" />
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

    <ConfirmDialog
      :show="pendingAction !== null"
      :title="pendingAction?.action === 'hide' ? '隐藏评价' : '删除评价'"
      :message="actionMessage"
      :confirm-text="pendingAction?.action === 'hide' ? '确认隐藏' : '确认删除'"
      cancel-text="取消"
      :danger="pendingAction?.action === 'delete'"
      :busy="busy"
      reason
      :reason-label="pendingAction?.action === 'hide' ? '隐藏原因（可选）' : '删除原因（可选）'"
      :reason-initial-value="pendingAction?.action === 'hide' ? pendingAction.review.hide_reason || '' : ''"
      @cancel="pendingAction = null"
      @confirm="confirmAction"
    />

    <BaseDialog
      :show="detailReview !== null"
      :title="detailReview ? `评价 #${detailReview.id} 详情` : '评价详情'"
      width="wide"
      @close="detailReview = null"
    >
      <div v-if="detailReview" class="review-detail">
        <p class="review-detail__content">{{ contentText(detailReview) }}</p>
        <dl v-if="detailReview.hide_reason">
          <dt>隐藏原因</dt>
          <dd>{{ detailReview.hide_reason }}</dd>
        </dl>
        <dl v-if="detailReview.follow_up">
          <dt>追评</dt>
          <dd>{{ contentText(detailReview.follow_up) }}</dd>
        </dl>
      </div>
    </BaseDialog>
  </section>
</template>

<style scoped>
.detail-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 3px;
  max-width: 28rem;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
  font-size: 12px;
  line-height: 1.4;
}
.detail-cell__text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  max-width: 100%;
}
.detail-cell__action { color: var(--color-primary, #2563eb); }
.follow-hint {
  margin-top: 2px;
  font-size: 11px;
  color: var(--color-text-secondary, #64748b);
  overflow-wrap: anywhere;
}
.table-ops {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.review-detail { display: grid; gap: 16px; overflow-wrap: anywhere; }
.review-detail__content { margin: 0; white-space: pre-wrap; }
.review-detail dl { margin: 0; }
.review-detail dt { font-size: 12px; font-weight: 600; color: var(--color-text-secondary, #64748b); }
.review-detail dd { margin: 4px 0 0; white-space: pre-wrap; }
@media (max-width: 767px) {
  .detail-cell__text { overflow-wrap: anywhere; word-break: break-word; }
}
</style>
