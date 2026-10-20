<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { admin } from '@/api'
import type { IAMStaff, OrgUnit } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import '@/styles/console-liquid.css'

const auth = useAuth()
const toast = useToast()
const units = useLoader(() => admin.orgUnits(), [] as OrgUnit[] | null)
const staff = useLoader(() => admin.orgStaff(), [] as IAMStaff[] | null)
const { busy, run } = useAction()

const selectedId = ref<number | null>(null)
/** 收起的节点；默认全部展开 */
const collapsed = ref<Set<number>>(new Set())
const show = ref(false)
const showDelete = ref(false)
const editing = ref<OrgUnit | null>(null)
const pendingDelete = ref<OrgUnit | null>(null)
const form = reactive({ name: '', code: '', parent_id: '' as string, sort: '0', enabled: true })

type TreeRow = OrgUnit & { depth: number; child_count: number; has_children: boolean }

const menuVisible = ref(false)
const menuX = ref(0)
const menuY = ref(0)
const menuTarget = ref<TreeRow | null>(null)

onMounted(async () => {
  await Promise.all([units.load(), staff.load()])
  if (!selectedId.value && treeRows.value.length) selectUnit(treeRows.value[0].id)
  window.addEventListener('keydown', onKey)
})
onUnmounted(() => window.removeEventListener('keydown', onKey))

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') hideMenu()
}

const byId = computed(() => new Map((units.data.value ?? []).map((u) => [u.id, u])))

const byParent = computed(() => {
  const m = new Map<number | null, OrgUnit[]>()
  for (const u of units.data.value ?? []) {
    const p = u.parent_id ?? null
    if (!m.has(p)) m.set(p, [])
    m.get(p)!.push(u)
  }
  for (const arr of m.values()) arr.sort((a, b) => a.sort - b.sort || a.id - b.id)
  return m
})

const childCount = computed(() => {
  const m = new Map<number, number>()
  for (const u of units.data.value ?? []) {
    if (u.parent_id) m.set(u.parent_id, (m.get(u.parent_id) ?? 0) + 1)
  }
  return m
})

const treeRows = computed(() => {
  const out: TreeRow[] = []
  function walk(parent: number | null, depth: number) {
    const kids = byParent.value.get(parent) ?? []
    for (const u of kids) {
      const n = childCount.value.get(u.id) ?? 0
      out.push({ ...u, depth, child_count: n, has_children: n > 0 })
      if (n > 0 && !collapsed.value.has(u.id)) walk(u.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
})

const allRows = computed(() => {
  const out: TreeRow[] = []
  function walk(parent: number | null, depth: number) {
    for (const u of byParent.value.get(parent) ?? []) {
      const n = childCount.value.get(u.id) ?? 0
      out.push({ ...u, depth, child_count: n, has_children: n > 0 })
      walk(u.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
})

const selected = computed(() => allRows.value.find((u) => u.id === selectedId.value) ?? null)

const children = computed(() => (byParent.value.get(selectedId.value) ?? []).map((u) => ({
  ...u,
  child_count: childCount.value.get(u.id) ?? 0,
})))

function isExpanded(id: number) {
  return !collapsed.value.has(id)
}
function toggleExpand(id: number) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}
function expandAll() {
  collapsed.value = new Set()
}
function collapseAll() {
  collapsed.value = new Set(
    (units.data.value ?? []).filter((u) => (childCount.value.get(u.id) ?? 0) > 0).map((u) => u.id),
  )
}

const unitStaff = computed(() => {
  if (!selectedId.value) return []
  return (staff.data.value ?? []).filter((s) => (s.org_units ?? []).some((o) => o.id === selectedId.value))
})

const parentName = computed(() => {
  const p = selected.value?.parent_id
  if (!p) return '—'
  return byId.value.get(p)?.name ?? `#${p}`
})

function selectUnit(id: number) {
  selectedId.value = id
}

function showMenu(e: MouseEvent, row: TreeRow | null) {
  if (!auth.isAdmin) return
  e.preventDefault()
  if (row) selectUnit(row.id)
  menuTarget.value = row
  const pad = 8
  const w = 180
  const h = row ? 220 : 56
  menuX.value = Math.min(e.clientX, window.innerWidth - w - pad)
  menuY.value = Math.min(e.clientY, window.innerHeight - h - pad)
  menuVisible.value = true
}
function hideMenu() {
  menuVisible.value = false
  menuTarget.value = null
}
function runMenu(fn: () => void) {
  hideMenu()
  fn()
}

watch(
  () => units.data.value,
  () => {
    if (!allRows.value.length) {
      selectedId.value = null
      return
    }
    if (!selectedId.value || !allRows.value.some((u) => u.id === selectedId.value)) {
      selectUnit(allRows.value[0].id)
    }
  },
)

/** parentId: null=根；number=挂到该上级 */
function openNew(parentId: number | null = null) {
  hideMenu()
  editing.value = null
  Object.assign(form, {
    name: '',
    code: '',
    parent_id: parentId != null ? String(parentId) : '',
    sort: '0',
    enabled: true,
  })
  show.value = true
}
function openEdit(u?: OrgUnit | null) {
  hideMenu()
  const target = u ?? selected.value
  if (!target) return
  selectUnit(target.id)
  editing.value = target
  Object.assign(form, {
    name: target.name,
    code: target.code,
    parent_id: target.parent_id ? String(target.parent_id) : '',
    sort: String(target.sort),
    enabled: target.enabled,
  })
  show.value = true
}
async function save() {
  const body = {
    name: form.name.trim(),
    code: form.code.trim(),
    parent_id: form.parent_id ? Number(form.parent_id) : null,
    sort: Number(form.sort) || 0,
    enabled: form.enabled,
  }
  const ok = editing.value
    ? await run(() => admin.updateOrgUnit(editing.value!.id, body), '已保存')
    : await run(() => admin.createOrgUnit(body), '已创建')
  if (ok) {
    show.value = false
    await units.load()
    if (!editing.value) {
      const created = (units.data.value ?? []).find((u) => u.code === body.code)
      if (created) selectUnit(created.id)
    }
  }
}
function askRemove(u?: TreeRow | null) {
  hideMenu()
  const target = u ?? selected.value
  if (!target) return
  selectUnit(target.id)
  const kids = target.child_count
  if (kids > 0) {
    toast.error(`「${target.name}」下还有 ${kids} 个下级组织，请先删除或改挂下级后再删。`)
    return
  }
  const staffN = (staff.data.value ?? []).filter((s) => (s.org_units ?? []).some((o) => o.id === target.id)).length
  if (staffN > 0) {
    toast.error(`「${target.name}」下还有 ${staffN} 名员工归属，请先在员工授权中改挂。`)
    return
  }
  pendingDelete.value = target
  showDelete.value = true
}
async function confirmRemove() {
  const u = pendingDelete.value
  if (!u) return
  if (await run(() => admin.deleteOrgUnit(u.id), '已删除')) {
    showDelete.value = false
    pendingDelete.value = null
    selectedId.value = null
    await units.load()
    await staff.load()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-org-units"
    plain
    title="组织架构"
    description="左侧组织树可右键新建 / 编辑 / 删除；右侧看下级与员工归属。"
  >
    <template #actions>
      <button type="button" class="btn btn-secondary btn-sm" @click="expandAll">全部展开</button>
      <button type="button" class="btn btn-secondary btn-sm" @click="collapseAll">全部收起</button>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="!selected || !auth.isAdmin"
        @click="openNew(selectedId)"
      >
        新建下级
      </button>
      <button type="button" class="btn btn-primary" :disabled="!auth.isAdmin" @click="openNew(null)">新建组织</button>
    </template>

    <div class="org-split">
      <aside class="org-split__side" @contextmenu.prevent="showMenu($event, null)">
        <div class="org-split__side-head">组织树 · 右键操作</div>
        <div v-if="units.loading.value" class="org-split__empty">加载中…</div>
        <div
          v-for="u in treeRows"
          :key="u.id"
          class="org-tree-row"
          :style="{ '--depth': u.depth }"
          @contextmenu.prevent.stop="showMenu($event, u)"
        >
          <button
            v-if="u.has_children"
            type="button"
            class="org-tree-toggle"
            :aria-expanded="isExpanded(u.id)"
            :title="isExpanded(u.id) ? '收起' : '展开'"
            @click.stop="toggleExpand(u.id)"
          >
            <svg class="org-tree-chevron" :class="{ open: isExpanded(u.id) }" viewBox="0 0 16 16" aria-hidden="true">
              <path
                d="M6.2 3.8a1 1 0 0 1 1.4 0l3.6 3.6a1 1 0 0 1 0 1.4l-3.6 3.6a1 1 0 1 1-1.4-1.4L9.1 8 6.2 5.2a1 1 0 0 1 0-1.4z"
                fill="currentColor"
              />
            </svg>
          </button>
          <span v-else class="org-tree-toggle org-tree-toggle--leaf" aria-hidden="true">
            <span class="org-tree-dot" />
          </span>
          <button
            type="button"
            class="org-split__item org-split__item--tree"
            :class="{ 'org-split__item--active': u.id === selectedId, 'is-disabled': !u.enabled }"
            @click="selectUnit(u.id)"
          >
            <span class="org-split__item-title">{{ u.name }}</span>
            <span class="org-split__item-meta">
              {{ u.code }}
              <template v-if="u.has_children && !isExpanded(u.id)"> · {{ u.child_count }}</template>
              <span v-if="!u.enabled" class="lg-chip is-off ml-1">停用</span>
            </span>
          </button>
        </div>
        <div v-if="!treeRows.length && !units.loading.value" class="org-split__empty">暂无组织，右键空白处可新建</div>
      </aside>

      <section class="org-split__main">
        <div v-if="!selected" class="org-split__empty org-split__empty--main">请选择左侧组织</div>
        <template v-else>
          <div class="org-split__main-head">
            <div class="min-w-0">
              <h2 class="org-split__title">{{ selected.name }}</h2>
              <p class="org-split__desc">编码 {{ selected.code }} · 上级 {{ parentName }}</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="!auth.isAdmin" @click="openEdit()">编辑</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !auth.isAdmin" @click="askRemove()">
                删除
              </button>
            </div>
          </div>

          <div class="org-stats">
            <div class="org-stat">
              <div class="org-stat__n">{{ selected.child_count }}</div>
              <div class="org-stat__l">直属下级</div>
            </div>
            <div class="org-stat">
              <div class="org-stat__n">{{ unitStaff.length }}</div>
              <div class="org-stat__l">归属员工</div>
            </div>
            <div class="org-stat">
              <div class="org-stat__n org-stat__n--tag">
                <span class="lg-chip" :class="selected.enabled ? 'is-ok' : 'is-off'">
                  {{ selected.enabled ? '启用' : '停用' }}
                </span>
              </div>
              <div class="org-stat__l">状态</div>
            </div>
          </div>

          <div class="org-panel">
            <h3 class="org-section-title">下级组织</h3>
            <div class="overflow-hidden rounded-xl border border-black/5">
              <table class="w-full text-left text-sm">
                <thead class="bg-black/[0.02] text-xs text-gray-500">
                  <tr>
                    <th class="px-3 py-2 font-semibold">名称</th>
                    <th class="px-3 py-2 font-semibold">编码</th>
                    <th class="px-3 py-2 font-semibold">排序</th>
                    <th class="px-3 py-2 font-semibold">状态</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="c in children"
                    :key="c.id"
                    class="cursor-pointer border-t border-black/5 hover:bg-black/[0.02]"
                    @click="selectUnit(c.id)"
                  >
                    <td class="px-3 py-2.5 font-medium">{{ c.name }}</td>
                    <td class="px-3 py-2.5">{{ c.code }}</td>
                    <td class="px-3 py-2.5">{{ c.sort }}</td>
                    <td class="px-3 py-2.5">
                      <span class="lg-chip" :class="c.enabled ? 'is-ok' : 'is-off'">{{ c.enabled ? '启用' : '停用' }}</span>
                    </td>
                  </tr>
                  <tr v-if="!children.length">
                    <td colspan="4" class="px-3 py-6 text-center text-gray-400">无下级；可点「新建下级」</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <h3 class="org-section-title mt-5">归属员工</h3>
            <div class="overflow-hidden rounded-xl border border-black/5">
              <table class="w-full text-left text-sm">
                <thead class="bg-black/[0.02] text-xs text-gray-500">
                  <tr>
                    <th class="px-3 py-2 font-semibold">ID</th>
                    <th class="px-3 py-2 font-semibold">邮箱</th>
                    <th class="px-3 py-2 font-semibold">角色</th>
                    <th class="px-3 py-2 font-semibold">状态</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="s in unitStaff" :key="s.id" class="border-t border-black/5">
                    <td class="px-3 py-2.5">{{ s.id }}</td>
                    <td class="px-3 py-2.5">{{ s.email }}</td>
                    <td class="px-3 py-2.5">
                      <span v-if="!(s.roles ?? []).length" class="text-gray-400">—</span>
                      <span v-for="r in s.roles ?? []" :key="r.id" class="lg-chip is-info mr-1">{{ r.name }}</span>
                    </td>
                    <td class="px-3 py-2.5">
                      <span
                        class="lg-chip"
                        :class="s.status === 'active' ? 'is-ok' : s.status === 'frozen' ? 'is-danger' : 'is-off'"
                      >
                        {{ s.status === 'active' ? '正常' : s.status === 'frozen' ? '冻结' : s.status }}
                      </span>
                    </td>
                  </tr>
                  <tr v-if="!unitStaff.length">
                    <td colspan="4" class="px-3 py-6 text-center text-gray-400">暂无员工挂在此部门，请到「员工授权」分配</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>
      </section>
    </div>
  </ConsoleListPage>

  <ModalDialog v-model:open="show" :title="editing ? '编辑组织' : '新建组织'">
    <div>
      <label class="label" for="oname">名称</label>
      <input id="oname" v-model="form.name" class="input" />
    </div>
    <div>
      <label class="label" for="ocode">编码</label>
      <input id="ocode" v-model="form.code" class="input" :disabled="!!editing" />
    </div>
    <div>
      <label class="label" for="oparent">上级</label>
      <select id="oparent" v-model="form.parent_id" class="input">
        <option value="">无（根）</option>
        <option v-for="u in units.data.value ?? []" :key="u.id" :value="String(u.id)" :disabled="editing?.id === u.id">
          {{ u.name }}
        </option>
      </select>
    </div>
    <div>
      <label class="label" for="osort">排序</label>
      <input id="osort" v-model="form.sort" class="input" inputmode="numeric" />
    </div>
    <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" /> 启用</label>
    <template #footer>
      <button class="btn-secondary" @click="show = false">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showDelete" title="删除组织">
    <p class="text-sm text-gray-700">确认删除组织「{{ pendingDelete?.name }}」？此操作不可撤销。</p>
    <template #footer>
      <button class="btn-secondary" @click="showDelete = false">取消</button>
      <button class="btn-danger" :disabled="busy" @click="confirmRemove">删除</button>
    </template>
  </ModalDialog>

  <Teleport to="body">
    <div v-if="menuVisible" class="org-ctx-overlay" @click="hideMenu" @contextmenu.prevent="hideMenu" />
    <div v-if="menuVisible" class="org-ctx-menu" :style="{ left: `${menuX}px`, top: `${menuY}px` }" @click.stop>
      <template v-if="menuTarget">
        <button type="button" class="org-ctx-item" @click="runMenu(() => openNew(menuTarget!.id))">新建下级</button>
        <button
          type="button"
          class="org-ctx-item"
          @click="runMenu(() => openNew(menuTarget!.parent_id ?? null))"
        >
          新建同级
        </button>
        <div class="org-ctx-sep" />
        <button type="button" class="org-ctx-item" @click="runMenu(() => openEdit(menuTarget))">编辑</button>
        <button
          v-if="menuTarget.has_children"
          type="button"
          class="org-ctx-item"
          @click="runMenu(() => toggleExpand(menuTarget!.id))"
        >
          {{ isExpanded(menuTarget.id) ? '收起' : '展开' }}
        </button>
        <div class="org-ctx-sep" />
        <button type="button" class="org-ctx-item org-ctx-item--danger" @click="runMenu(() => askRemove(menuTarget))">
          删除
        </button>
      </template>
      <template v-else>
        <button type="button" class="org-ctx-item" @click="runMenu(() => openNew(null))">新建根组织</button>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.org-split {
  display: flex;
  gap: 12px;
  min-height: min(70vh, 720px);
  align-items: stretch;
}
.org-split__side {
  width: 280px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  border-radius: 1rem;
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid rgba(15, 23, 42, 0.08);
  overflow: auto;
}
.org-split__side-head {
  font-size: 12px;
  font-weight: 600;
  color: #868686;
  padding: 4px 8px 8px;
}
.org-tree-row {
  display: flex;
  align-items: flex-start;
  gap: 2px;
  padding-left: calc(var(--depth, 0) * 16px);
}
.org-tree-toggle {
  flex-shrink: 0;
  width: 22px;
  height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: #868686;
  cursor: pointer;
  border-radius: 6px;
  padding: 0;
}
.org-tree-toggle:hover {
  background: rgba(15, 23, 42, 0.06);
  color: #0555ce;
}
.org-tree-toggle--leaf {
  cursor: default;
  pointer-events: none;
}
.org-tree-chevron {
  width: 14px;
  height: 14px;
  transition: transform 0.15s ease;
}
.org-tree-chevron.open {
  transform: rotate(90deg);
}
.org-tree-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #c4c4c4;
}
.org-split__item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  width: 100%;
  text-align: left;
  padding: 8px 10px;
  border-radius: 10px;
  border: none;
  background: transparent;
  cursor: pointer;
  min-width: 0;
}
.org-split__item--tree {
  flex: 1;
}
.org-split__item:hover {
  background: rgba(5, 85, 206, 0.06);
}
.org-split__item--active {
  background: rgba(5, 85, 206, 0.1);
}
.org-split__item.is-disabled .org-split__item-title {
  color: #a3a3a3;
}
.org-split__item-title {
  font-size: 14px;
  font-weight: 600;
  color: #262626;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}
.org-split__item-meta {
  font-size: 12px;
  color: #868686;
}
.org-split__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 20px 20px;
  border-radius: 1rem;
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid rgba(15, 23, 42, 0.08);
  overflow: auto;
}
.org-split__main-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.org-split__title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: #262626;
}
.org-split__desc {
  margin: 4px 0 0;
  font-size: 13px;
  color: #868686;
}
.org-split__empty {
  padding: 16px;
  font-size: 13px;
  color: #868686;
  text-align: center;
}
.org-split__empty--main {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.org-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.org-stat {
  padding: 12px 14px;
  border-radius: 12px;
  background: rgba(5, 85, 206, 0.04);
  border: 1px solid rgba(5, 85, 206, 0.08);
}
.org-stat__n {
  font-size: 22px;
  font-weight: 650;
  color: #0555ce;
  line-height: 1.2;
}
.org-stat__n--tag {
  font-size: inherit;
  font-weight: inherit;
  min-height: 28px;
  display: flex;
  align-items: center;
}
.org-stat__l {
  margin-top: 2px;
  font-size: 12px;
  color: #868686;
}
.org-panel {
  flex: 1;
  min-height: 0;
}
.org-section-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: #525252;
}
@media (max-width: 900px) {
  .org-split {
    flex-direction: column;
    min-height: 0;
  }
  .org-split__side {
    width: 100%;
    max-height: 220px;
  }
}
</style>

<style>
.org-ctx-overlay {
  position: fixed;
  inset: 0;
  z-index: 80;
}
.org-ctx-menu {
  position: fixed;
  z-index: 81;
  min-width: 160px;
  padding: 6px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(15, 23, 42, 0.1);
  box-shadow: 0 8px 28px rgba(15, 23, 42, 0.14);
}
.org-ctx-item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 8px 12px;
  border: none;
  border-radius: 7px;
  background: transparent;
  font-size: 13px;
  color: #262626;
  cursor: pointer;
}
.org-ctx-item:hover {
  background: rgba(5, 85, 206, 0.08);
  color: #0555ce;
}
.org-ctx-item--danger {
  color: #b91c1c;
}
.org-ctx-item--danger:hover {
  background: rgba(185, 28, 28, 0.08);
  color: #b91c1c;
}
.org-ctx-sep {
  height: 1px;
  margin: 4px 6px;
  background: rgba(15, 23, 42, 0.08);
}
</style>
