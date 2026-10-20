<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { MarketCategory } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import '@/styles/console-liquid.css'

type FlatRow = MarketCategory & { depth: number; rootId: string; hasChildren: boolean }

const toast = useToast()
const { busy, run } = useAction()
const tree = ref<MarketCategory[]>([])
/** 收起的节点 id（默认全部展开） */
const collapsed = ref<Set<string>>(new Set())
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  root: '',
  enabled: '',
  pickable: '',
  depth: '',
})

const flatAll = computed(() => {
  const out: FlatRow[] = []
  const walk = (nodes: MarketCategory[], depth: number, rootId: string) => {
    for (const n of nodes) {
      const rid = depth === 0 ? n.id : rootId
      const hasChildren = !!(n.children && n.children.length)
      out.push({ ...n, depth, rootId: rid, hasChildren, children: n.children })
      if (hasChildren) walk(n.children!, depth + 1, rid)
    }
  }
  walk(tree.value, 0, '')
  return out
})

function rowMatches(r: FlatRow) {
  const kw = queryParams.keyword.trim().toLowerCase()
  if (kw) {
    const hit =
      r.id.toLowerCase().includes(kw) ||
      r.name.toLowerCase().includes(kw) ||
      (r.description || '').toLowerCase().includes(kw)
    if (!hit) return false
  }
  if (queryParams.root && r.rootId !== queryParams.root) return false
  if (queryParams.enabled === 'true' && !r.enabled) return false
  if (queryParams.enabled === 'false' && r.enabled) return false
  if (queryParams.pickable === 'true' && !r.seller_pickable) return false
  if (queryParams.pickable === 'false' && r.seller_pickable) return false
  if (queryParams.depth === '3+') {
    if (r.depth < 3) return false
  } else if (queryParams.depth !== '') {
    const d = Number(queryParams.depth)
    if (!Number.isNaN(d) && r.depth !== d) return false
  }
  return true
}

const hasActiveFilters = computed(
  () =>
    !!(
      queryParams.keyword.trim() ||
      queryParams.root ||
      queryParams.enabled ||
      queryParams.pickable ||
      queryParams.depth
    ),
)

/** 筛选命中节点及其祖先（用于展示上下文）；并强制展开祖先 */
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
  const walk = (nodes: MarketCategory[], depth: number, rootId: string) => {
    for (const n of nodes) {
      const rid = depth === 0 ? n.id : rootId
      const hasChildren = !!(n.children && n.children.length)
      if (filtering && !show.has(n.id)) continue
      out.push({ ...n, depth, rootId: rid, hasChildren, children: n.children })
      if (!hasChildren) continue
      const open = !collapsed.value.has(n.id) || forceOpen.has(n.id)
      if (open) walk(n.children!, depth + 1, rid)
    }
  }
  walk(tree.value, 0, '')
  return out
})

const rootOptions = computed(() => tree.value.map((r) => ({ id: r.id, name: r.name })))

const parentOptions = computed(() => [
  { id: '', label: '（根节点）' },
  ...flatAll.value.map((r) => ({ id: r.id, label: `${'— '.repeat(r.depth)}${r.name} (${r.id})` })),
])

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

function search() {
  handleQuery()
}
function onReset() {
  resetQuery()
}

const editing = ref<MarketCategory | null>(null)
const creating = ref(false)
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

onMounted(load)

async function load() {
  try {
    tree.value = (await admin.marketCategories()) ?? []
  } catch (e) {
    toast.error(e)
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

async function save() {
  if (!form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  if (creating.value) {
    if (!/^[a-z0-9_]{1,64}$/.test(form.id.trim())) {
      toast.error('id 须为小写字母/数字/下划线')
      return
    }
    const ok = await run(
      () =>
        admin.createMarketCategory({
          id: form.id.trim(),
          parent_id: form.parent_id || null,
          name: form.name.trim(),
          description: form.description.trim(),
          sort: Number(form.sort) || 0,
          enabled: form.enabled,
          seller_pickable: form.seller_pickable,
          icon: form.icon.trim(),
        } as never),
      '已创建',
    )
    if (!ok) return
  } else if (editing.value) {
    const ok = await run(
      () =>
        admin.updateMarketCategory(editing.value!.id, {
          parent_id: form.parent_id || null,
          name: form.name.trim(),
          description: form.description.trim(),
          sort: Number(form.sort) || 0,
          enabled: form.enabled,
          seller_pickable: form.seller_pickable,
          icon: form.icon.trim(),
        } as never),
      '已保存',
    )
    if (!ok) return
  }
  closeModal()
  await load()
}

async function remove(c: MarketCategory) {
  if (c.children?.length) {
    toast.error('请先删除子分类')
    return
  }
  if (!confirm(`删除分类「${c.name}」(${c.id})？`)) return
  if (await run(() => admin.deleteMarketCategory(c.id), '已删除')) await load()
}

async function toggleEnabled(c: MarketCategory) {
  await run(
    () =>
      admin.updateMarketCategory(c.id, {
        enabled: !c.enabled,
        name: c.name,
        description: c.description,
        sort: c.sort,
        seller_pickable: c.seller_pickable,
        parent_id: c.parent_id ?? null,
        icon: c.icon ?? '',
      } as never),
    c.enabled ? '已停用' : '已启用',
  )
  await load()
}

function depthClass(depth: number) {
  const n = Math.min(depth, 3)
  return `cat-depth-${n}`
}
</script>

<template>
  <ConsoleListPage
    id="console-market-categories-page"
    title="市场分类"
    description="树状分类由控制台维护。勾选「卖家可选」的节点会出现在发布挂单页。"
    plain
  >
    <template #actions>
      <button type="button" class="btn btn-secondary" @click="expandAll">全部展开</button>
      <button type="button" class="btn btn-secondary" @click="collapseAll">全部收起</button>
      <button type="button" class="btn btn-primary" :disabled="busy" @click="openCreate()">＋ 根分类</button>
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input
            v-model="queryParams.keyword"
            class="filter-input w-full"
            placeholder="名称 / ID / 说明"
            @keyup.enter="search"
          />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">根分类</label>
        <select v-model="queryParams.root" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="r in rootOptions" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">层级</label>
        <select v-model="queryParams.depth" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="0">根</option>
          <option value="1">一级子类</option>
          <option value="2">二级子类</option>
          <option value="3+">更深层级</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.enabled" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">启用</option>
          <option value="false">停用</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">卖家可选</label>
        <select v-model="queryParams.pickable" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">可选</option>
          <option value="false">不可选</option>
        </select>
      </div>
      <div class="filter-actions lg:col-span-3">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <div class="lg-stage">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap gap-3 text-[11px] text-slate-500">
          <span class="inline-flex items-center gap-1.5"><i class="cat-swatch cat-depth-0" />根分类</span>
          <span class="inline-flex items-center gap-1.5"><i class="cat-swatch cat-depth-1" />一级子类</span>
          <span class="inline-flex items-center gap-1.5"><i class="cat-swatch cat-depth-2" />二级子类</span>
          <span class="inline-flex items-center gap-1.5"><i class="cat-swatch cat-depth-3" />更深层级</span>
        </div>
        <span class="lg-chip">共 {{ flat.length }} 条</span>
      </div>

      <div class="cat-head" aria-hidden="true">
        <span>分类</span>
        <span>ID</span>
        <span>卖家可选</span>
        <span>状态</span>
        <span>排序</span>
        <span>操作</span>
      </div>

      <div class="cat-steps">
        <div
          v-for="r in flat"
          :key="r.id"
          class="cat-step"
          :class="[depthClass(r.depth), { 'is-disabled': !r.enabled }]"
          :style="{ '--step': Math.min(r.depth, 4) }"
        >
          <div class="lg-card cat-step__bar">
            <div class="cat-name">
              <button
                v-if="r.hasChildren"
                type="button"
                class="cat-toggle"
                :aria-expanded="isExpanded(r.id)"
                :title="isExpanded(r.id) ? '收起' : '展开'"
                @click="toggleExpand(r.id)"
              >
                <svg class="cat-chevron" :class="{ open: isExpanded(r.id) }" viewBox="0 0 16 16" aria-hidden="true">
                  <path
                    d="M6.2 3.8a1 1 0 0 1 1.4 0l3.6 3.6a1 1 0 0 1 0 1.4l-3.6 3.6a1 1 0 1 1-1.4-1.4L9.1 8 6.2 5.2a1 1 0 0 1 0-1.4z"
                    fill="currentColor"
                  />
                </svg>
              </button>
              <span v-else class="cat-toggle cat-toggle--leaf" aria-hidden="true">
                <span class="cat-dot" />
              </span>
              <span class="cat-title" :class="{ root: r.depth === 0 }">{{ r.name }}</span>
              <span v-if="r.hasChildren && !isExpanded(r.id)" class="lg-chip">{{ r.children?.length ?? 0 }}</span>
              <span v-if="r.description" class="cat-desc">{{ r.description }}</span>
            </div>
            <div class="cat-id">{{ r.id }}</div>
            <div class="cat-cell">
              <span v-if="r.seller_pickable" class="lg-chip is-ok">可选</span>
              <span v-else class="text-slate-300">—</span>
            </div>
            <div class="cat-cell text-slate-600">{{ r.enabled ? '启用' : '停用' }}</div>
            <div class="cat-cell text-slate-600">{{ r.sort }}</div>
            <div class="cat-actions">
              <button type="button" class="btn-ghost btn-sm" :disabled="busy" @click="openCreate(r.id)">加子级</button>
              <button type="button" class="btn-ghost btn-sm" :disabled="busy" @click="openEdit(r)">编辑</button>
              <button type="button" class="btn-ghost btn-sm" :disabled="busy" @click="toggleEnabled(r)">
                {{ r.enabled ? '停用' : '启用' }}
              </button>
              <button type="button" class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="remove(r)">删除</button>
            </div>
          </div>
        </div>
      </div>

      <p v-if="!flat.length" class="py-6 text-sm text-slate-500">
        {{
          hasActiveFilters
            ? '没有符合条件的分类'
            : '暂无分类（重启 API 以应用迁移种子）。'
        }}
      </p>
    </div>

    <ModalDialog :open="creating || !!editing" :title="creating ? '新建分类' : `编辑 ${editing?.id}`" @update:open="(v: boolean) => !v && closeModal()">
      <div class="grid gap-3">
        <div v-if="creating">
          <label class="label">ID</label>
          <input v-model="form.id" class="input font-mono" placeholder="low_rate" />
        </div>
        <div>
          <label class="label">名称</label>
          <input v-model="form.name" class="input" />
        </div>
        <div>
          <label class="label">父分类</label>
          <select v-model="form.parent_id" class="input">
            <option v-for="p in parentOptions" :key="p.id || 'root'" :value="p.id" :disabled="!!editing && p.id === editing.id">
              {{ p.label }}
            </option>
          </select>
        </div>
        <div>
          <label class="label">说明</label>
          <input v-model="form.description" class="input" />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="label">排序</label>
            <input v-model.number="form.sort" type="number" class="input" />
          </div>
          <div>
            <label class="label">图标 key</label>
            <input v-model="form.icon" class="input" placeholder="chat / code" />
          </div>
        </div>
        <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" /> 启用</label>
        <label class="flex items-center gap-2 text-sm"><input v-model="form.seller_pickable" type="checkbox" /> 卖家发布时可勾选</label>
      </div>
      <template #footer>
        <button class="btn-secondary" type="button" @click="closeModal">取消</button>
        <button class="btn-primary" type="button" :disabled="busy" @click="save">保存</button>
      </template>
    </ModalDialog>
  </ConsoleListPage>
</template>

<style scoped>
.cat-swatch {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 999px;
  border: 1px solid rgb(0 0 0 / 6%);
}
.cat-swatch.cat-depth-0 {
  background: #e8eef8;
}
.cat-swatch.cat-depth-1 {
  background: #eef1f5;
}
.cat-swatch.cat-depth-2 {
  background: #f5f0e8;
}
.cat-swatch.cat-depth-3 {
  background: #efeaf6;
}

.cat-head {
  display: grid;
  grid-template-columns: minmax(180px, 1.6fr) minmax(88px, 0.7fr) 72px 56px 48px minmax(200px, 1.1fr);
  align-items: center;
  gap: 0.5rem 0.75rem;
  padding: 0 0.9rem 0.55rem;
  font-size: 12px;
  font-weight: 500;
  color: #94a3b8;
}

.cat-steps {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.cat-step {
  --step: 0;
  /* 阶梯只缩左侧，右侧对齐 */
  margin-left: calc(var(--step) * 22px);
  margin-right: 0;
  transition: margin-left 0.18s ease;
}
.cat-step__bar {
  display: grid;
  min-height: 48px;
  grid-template-columns: minmax(180px, 1.6fr) minmax(88px, 0.7fr) 72px 56px 48px minmax(200px, 1.1fr);
  align-items: center;
  gap: 0.5rem 0.75rem;
  padding: 0.55rem 0.85rem;
}
.cat-step.is-disabled {
  opacity: 0.55;
}

.cat-name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.35rem;
}
.cat-title {
  font-weight: 600;
  color: #334155;
}
.cat-title.root {
  font-size: 14px;
  color: #1e293b;
}
.cat-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #94a3b8;
}
.cat-id {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  color: #64748b;
}
.cat-cell {
  font-size: 13px;
}
.cat-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.15rem;
}

.cat-toggle {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 1.5rem;
  height: 1.5rem;
  border: 0;
  border-radius: 999px;
  background: rgb(255 255 255 / 75%);
  color: #64748b;
  cursor: pointer;
  box-shadow: 0 0 0 1px rgb(0 0 0 / 4%);
}
.cat-toggle:hover {
  background: #fff;
  color: #334155;
}
.cat-toggle--leaf {
  background: transparent;
  box-shadow: none;
  cursor: default;
  pointer-events: none;
}
.cat-dot {
  width: 5px;
  height: 5px;
  border-radius: 999px;
  background: #cbd5e1;
}
.cat-chevron {
  display: block;
  width: 12px;
  height: 12px;
  transition: transform 0.18s ease;
}
.cat-chevron.open {
  transform: rotate(90deg);
}
@media (max-width: 960px) {
  .cat-head {
    display: none;
  }
  .cat-step__bar {
    grid-template-columns: 1fr;
    gap: 0.35rem;
  }
  .cat-actions {
    justify-content: flex-start;
  }
}
</style>
