<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

export interface MarketCategory {
  id: string
  parent_id?: string | null
  name: string
  description?: string
  sort: number
  enabled: boolean
  seller_pickable: boolean
  icon?: string
  children?: MarketCategory[]
}

type FlatRow = MarketCategory & { depth: number; hasChildren: boolean }

const tree = ref<MarketCategory[]>([])
const collapsed = ref<Set<string>>(new Set())
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterEnabled = ref('')
const filterPickable = ref('')

const creating = ref(false)
const editing = ref<MarketCategory | null>(null)
const form = reactive({
  id: '',
  parent_id: '' as string,
  name: '',
  description: '',
  sort: 0,
  enabled: true,
  seller_pickable: false,
  icon: '',
})

const flatAll = computed(() => {
  const out: FlatRow[] = []
  const walk = (nodes: MarketCategory[], depth: number) => {
    for (const n of nodes) {
      const hasChildren = !!(n.children && n.children.length)
      out.push({ ...n, depth, hasChildren })
      if (hasChildren) walk(n.children!, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

function rowMatches(r: FlatRow) {
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    const hit =
      r.id.toLowerCase().includes(kw) ||
      r.name.toLowerCase().includes(kw) ||
      (r.description || '').toLowerCase().includes(kw)
    if (!hit) return false
  }
  if (filterEnabled.value === 'true' && !r.enabled) return false
  if (filterEnabled.value === 'false' && r.enabled) return false
  if (filterPickable.value === 'true' && !r.seller_pickable) return false
  if (filterPickable.value === 'false' && r.seller_pickable) return false
  return true
}

const hasActiveFilters = computed(
  () => !!(keyword.value.trim() || filterEnabled.value || filterPickable.value),
)

function resetFilters() {
  keyword.value = ''
  filterEnabled.value = ''
  filterPickable.value = ''
}

const filterReveal = computed(() => {
  const show = new Set<string>()
  const forceOpen = new Set<string>()
  if (!hasActiveFilters.value) return { show, forceOpen }
  const byId = new Map(flatAll.value.map((r) => [r.id, r]))
  for (const r of flatAll.value) {
    if (!rowMatches(r)) continue
    let cur: FlatRow | undefined = r
    while (cur) {
      show.add(cur.id)
      if (cur.parent_id) forceOpen.add(cur.parent_id)
      cur = cur.parent_id ? byId.get(cur.parent_id) : undefined
    }
  }
  return { show, forceOpen }
})

const flat = computed(() => {
  const out: FlatRow[] = []
  const filtering = hasActiveFilters.value
  const { show, forceOpen } = filterReveal.value
  const walk = (nodes: MarketCategory[], depth: number) => {
    for (const n of nodes) {
      const hasChildren = !!(n.children && n.children.length)
      if (filtering && !show.has(n.id)) continue
      out.push({ ...n, depth, hasChildren })
      if (!hasChildren) continue
      const open = !collapsed.value.has(n.id) || forceOpen.has(n.id)
      if (open) walk(n.children!, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

const parentOptions = computed(() => [
  { id: '', label: '（根节点）' },
  ...flatAll.value.map((r) => ({
    id: r.id,
    label: `${'— '.repeat(r.depth)}${r.name} (${r.id})`,
  })),
])

const modalOpen = computed(() => creating.value || !!editing.value)

function isExpanded(id: string) {
  return !collapsed.value.has(id) || filterReveal.value.forceOpen.has(id)
}

function toggleExpand(id: string) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

function expandAll() {
  collapsed.value = new Set()
}

function collapseAll() {
  collapsed.value = new Set(flatAll.value.filter((r) => r.hasChildren).map((r) => r.id))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    tree.value = (await marketGet<MarketCategory[] | null>(`${MARKET_API_PREFIX}/market-categories`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    tree.value = []
  } finally {
    loading.value = false
  }
}

function openCreate(parentId = '') {
  creating.value = true
  editing.value = null
  Object.assign(form, {
    id: '',
    parent_id: parentId,
    name: '',
    description: '',
    sort: 10,
    enabled: true,
    seller_pickable: parentId === 'scene' || parentId === 'region',
    icon: '',
  })
}

function openEdit(c: MarketCategory) {
  creating.value = false
  editing.value = c
  Object.assign(form, {
    id: c.id,
    parent_id: c.parent_id ?? '',
    name: c.name,
    description: c.description ?? '',
    sort: c.sort,
    enabled: c.enabled,
    seller_pickable: c.seller_pickable,
    icon: c.icon ?? '',
  })
}

function closeModal() {
  creating.value = false
  editing.value = null
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

function bodyFromForm() {
  return {
    parent_id: form.parent_id || null,
    name: form.name.trim(),
    description: form.description.trim(),
    sort: Number(form.sort) || 0,
    enabled: form.enabled,
    seller_pickable: form.seller_pickable,
    icon: form.icon.trim(),
  }
}

async function save() {
  if (!form.name.trim()) {
    error.value = '请填写名称'
    return
  }
  if (creating.value) {
    if (!/^[a-z0-9_]{1,64}$/.test(form.id.trim())) {
      error.value = 'id 须为小写字母/数字/下划线'
      return
    }
    const ok = await run('已创建', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/market-categories`, {
        id: form.id.trim(),
        ...bodyFromForm(),
      }),
    )
    if (!ok) return
  } else if (editing.value) {
    const ok = await run('已保存', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/market-categories/${editing.value!.id}`, bodyFromForm()),
    )
    if (!ok) return
  }
  closeModal()
  await load()
}

async function remove(c: MarketCategory) {
  if (c.children?.length) {
    error.value = '请先删除子分类'
    return
  }
  if (!window.confirm(`删除分类「${c.name}」(${c.id})？`)) return
  if (await run('已删除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/market-categories/${c.id}`))) {
    await load()
  }
}

async function toggleEnabled(c: MarketCategory) {
  if (
    await run(c.enabled ? '已停用' : '已启用', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/market-categories/${c.id}`, {
        enabled: !c.enabled,
        name: c.name,
        description: c.description ?? '',
        sort: c.sort,
        seller_pickable: c.seller_pickable,
        parent_id: c.parent_id ?? null,
        icon: c.icon ?? '',
      }),
    )
  ) {
    await load()
  }
}

onMounted(load)
</script>

<template>
  <section id="market-categories-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">市场分类</h2>
        <p class="y-page-desc">树状分类；勾选「卖家可选」的节点会出现在发布挂单页</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="expandAll">全部展开</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="collapseAll">全部收起</button>
        <button type="button" class="y-btn" :disabled="busy" @click="openCreate()">＋ 根分类</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input class="filter-input w-full" v-model="keyword" type="search" placeholder="名称 / ID / 说明" />
          </div>
        </div>
        <div class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="filterEnabled" class="filter-input flex-1">
            <option value="">状态：全部</option>
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </div>
        <div class="filter-item">
          <label class="filter-label">卖家可选</label>
          <select v-model="filterPickable" class="filter-input flex-1">
            <option value="">卖家可选：全部</option>
            <option value="true">可选</option>
            <option value="false">不可选</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button type="button" class="btn btn-secondary" @click="resetFilters">重置</button>
          <button type="button" class="btn btn-primary" @click="load">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!flat.length" class="y-msg">
        {{ hasActiveFilters ? '没有符合条件的分类' : '暂无分类' }}
      </p>
      <table v-else>
        <thead>
          <tr>
            <th>分类</th>
            <th>ID</th>
            <th>卖家可选</th>
            <th>状态</th>
            <th>排序</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in flat" :key="r.id" :class="{ 'is-disabled': !r.enabled }">
            <td>
              <div class="cat-name" :style="{ paddingLeft: `${Math.min(r.depth, 4) * 18}px` }">
                <button
                  v-if="r.hasChildren"
                  type="button"
                  class="cat-toggle"
                  :aria-expanded="isExpanded(r.id)"
                  :title="isExpanded(r.id) ? '收起' : '展开'"
                  @click="toggleExpand(r.id)"
                >
                  {{ isExpanded(r.id) ? '▾' : '▸' }}
                </button>
                <span v-else class="cat-leaf" aria-hidden="true">·</span>
                <span class="cat-title" :class="{ root: r.depth === 0 }">{{ r.name }}</span>
                <span v-if="r.description" class="cat-desc">{{ r.description }}</span>
              </div>
            </td>
            <td class="cat-id">{{ r.id }}</td>
            <td>
              <span v-if="r.seller_pickable" class="y-chip y-chip--ok">可选</span>
              <span v-else>—</span>
            </td>
            <td>
              <span :class="r.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ r.enabled ? '启用' : '停用' }}</span>
            </td>
            <td>{{ r.sort }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="openCreate(r.id)">加子级</button>
              <button type="button" class="y-link" :disabled="busy" @click="openEdit(r)">编辑</button>
              <button type="button" class="y-link" :disabled="busy" @click="toggleEnabled(r)">
                {{ r.enabled ? '停用' : '启用' }}
              </button>
              <button type="button" class="y-link y-link--danger" :disabled="busy" @click="remove(r)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="!loading" class="y-footer">共 {{ flat.length }} 条</footer>

    <div v-if="modalOpen" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="creating ? '新建分类' : '编辑分类'">
        <header class="y-modal__head">
          <h3>{{ creating ? '新建分类' : `编辑 ${editing?.id}` }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label v-if="creating" class="y-field">
            <span>ID</span>
            <input v-model="form.id" class="input w-full font-mono" placeholder="low_rate" />
          </label>
          <label class="y-field">
            <span>名称</span>
            <input v-model="form.name" class="input w-full" />
          </label>
          <label class="y-field">
            <span>父分类</span>
            <select v-model="form.parent_id" class="input w-full">
              <option
                v-for="p in parentOptions"
                :key="p.id || 'root'"
                :value="p.id"
                :disabled="!!editing && p.id === editing.id"
              >
                {{ p.label }}
              </option>
            </select>
          </label>
          <label class="y-field">
            <span>说明</span>
            <input v-model="form.description" class="input w-full" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>排序</span>
              <input v-model.number="form.sort" type="number" class="input w-full" />
            </label>
            <label class="y-field">
              <span>图标 key</span>
              <input v-model="form.icon" class="input w-full" placeholder="chat / code" />
            </label>
          </div>
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用</label>
          <label class="y-check"><input v-model="form.seller_pickable" type="checkbox" /> 卖家发布时可勾选</label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="closeModal">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="save">保存</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.cat-name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.35rem;
}
.cat-title {
  font-weight: 600;
}
.cat-title.root {
  font-size: 14px;
}
.cat-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  opacity: 0.65;
}
.cat-id {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.cat-toggle {
  width: 1.4rem;
  height: 1.4rem;
  border: 0;
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  line-height: 1;
}
.cat-leaf {
  display: inline-block;
  width: 1.4rem;
  text-align: center;
  opacity: 0.4;
}
tr.is-disabled {
  opacity: 0.55;
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
  width: min(480px, 100%);
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
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
</style>
