<script setup lang="ts">
/**
 * 组织管理：TablePageLayout + DataTable（组织/员工）；角色保留左右分栏与权限树。
 */
import { computed, onMounted, onUnmounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminOrg' })

interface OrgUnit {
  id: number
  parent_id?: number | null
  name: string
  code: string
  sort: number
  enabled: boolean
}

interface IAMRole {
  id: number
  name: string
  code: string
  description?: string
  member_count?: number
  enabled?: boolean
}

interface IAMResource {
  id: string
  parent_id?: string | null
  name: string
  kind: 'dir' | 'menu' | 'action'
  path: string
  icon: string
  sort: number
  enabled: boolean
}

interface IAMStaff {
  id: number
  email: string
  status?: string
  roles?: { id: number; name: string }[]
  org_units?: { id: number; name: string }[]
}

const tab = ref<'units' | 'roles' | 'staff'>('units')
const units = ref<OrgUnit[]>([])
const roles = ref<IAMRole[]>([])
const resources = ref<IAMResource[]>([])
const staff = ref<IAMStaff[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const showUnit = ref(false)
const editingUnit = ref<OrgUnit | null>(null)
const unitForm = reactive({ name: '', code: '', parent_id: '', sort: 0, enabled: true })

const showRole = ref(false)
const editingRole = ref<IAMRole | null>(null)
const roleForm = reactive({ name: '', code: '', description: '' })

const selectedRoleId = ref<number | null>(null)
const roleDetailTab = ref<'perms' | 'members'>('perms')
const checked = ref<string[]>([])
const roleMembers = ref<IAMStaff[]>([])
const detailLoading = ref(false)
const memberQ = ref('')
const pickHits = ref<IAMStaff[]>([])
const memberSearchLoading = ref(false)
const memberSearchError = ref('')
const memberSearched = ref(false)
const checkedBaseline = ref<string[]>([])
const membersBaseline = ref<IAMStaff[]>([])
const showDiscardConfirm = ref(false)
let discardAction: (() => void | Promise<void>) | null = null
let roleDetailRequest = 0

const permsDirty = computed(() => {
  const current = [...checked.value].sort()
  const baseline = [...checkedBaseline.value].sort()
  return current.length !== baseline.length || current.some((id, index) => id !== baseline[index])
})
const membersDirty = computed(() => {
  const current = roleMembers.value.map((member) => member.id)
  const baseline = membersBaseline.value.map((member) => member.id)
  return current.length !== baseline.length || current.some((id, index) => id !== baseline[index])
})
const hasRoleDraft = computed(() => permsDirty.value || membersDirty.value)
const formDialogOpen = computed(() => showUnit.value || showRole.value || showStaff.value)
const pendingDelete = ref<{ type: 'unit' | 'role'; unit?: OrgUnit; role?: IAMRole } | null>(null)
const deleteMessage = computed(() => {
  const pending = pendingDelete.value
  if (!pending) return ''
  const target = pending.type === 'unit' ? `组织「${pending.unit?.name}」` : `角色「${pending.role?.name}」`
  const draftWarning = pending.type === 'role' && hasRoleDraft.value ? '该角色的未保存草稿也会丢失。' : ''
  return `确认删除${target}？${draftWarning}`
})

const showStaff = ref(false)
const editingStaff = ref<IAMStaff | null>(null)
const staffForm = reactive({
  email: '',
  password: '',
  role_ids: [] as number[],
  org_unit_ids: [] as number[],
})

const kindLabel: Record<string, string> = { dir: '目录', menu: '菜单', action: '功能' }

const unitColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'name', label: '名称', class: 'col-name' },
  { key: 'code', label: '编码', class: 'col-code' },
  { key: 'parent_id', label: '父级', class: 'col-parent' },
  { key: 'sort', label: '排序', class: 'col-sort' },
  { key: 'enabled', label: '状态', class: 'col-status' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const staffColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'email', label: '邮箱', class: 'col-email' },
  { key: 'roles', label: '角色', class: 'col-roles' },
  { key: 'org_units', label: '组织', class: 'col-orgs' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const memberColumns: Column[] = [
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'email', label: '邮箱', class: 'col-email' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const filteredUnits = computed(() => {
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (!kw) return units.value
  return units.value.filter((u) => `${u.name} ${u.code}`.toLowerCase().includes(kw))
})

const filteredStaff = computed(() => {
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (!kw) return staff.value
  return staff.value.filter((s) => `${s.id} ${s.email}`.toLowerCase().includes(kw))
})

const pageSource = computed(() => (tab.value === 'units' ? filteredUnits.value : filteredStaff.value))
const total = computed(() => (tab.value === 'roles' ? 0 : pageSource.value.length))
const pageRows = computed(() => {
  if (tab.value === 'roles') return []
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return pageSource.value.slice(start, start + queryParams.value.pageSize)
})

const columns = computed(() => (tab.value === 'staff' ? staffColumns : unitColumns))

const selectedRole = computed(() => roles.value.find((r) => r.id === selectedRoleId.value) ?? null)

const resourceByParent = computed(() => {
  const byParent = new Map<string | null, IAMResource[]>()
  for (const r of resources.value) {
    const p = r.parent_id ?? null
    if (!byParent.has(p)) byParent.set(p, [])
    byParent.get(p)!.push(r)
  }
  for (const arr of byParent.values()) arr.sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id))
  return byParent
})

const resourceTree = computed(() => {
  const out: (IAMResource & { depth: number })[] = []
  const walk = (parent: string | null, depth: number) => {
    for (const r of resourceByParent.value.get(parent) ?? []) {
      out.push({ ...r, depth })
      walk(r.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
})

const resourceParentOf = computed(() => {
  const m = new Map<string, string | null>()
  for (const r of resources.value) m.set(r.id, r.parent_id ?? null)
  return m
})

const pageTitle = computed(() => {
  if (tab.value === 'units') return '组织单元'
  if (tab.value === 'roles') return '角色权限'
  return '员工授权'
})

const pageDesc = computed(() => {
  if (tab.value === 'units') return '维护组织树与启用状态'
  if (tab.value === 'roles') return '配置角色菜单权限与成员'
  return '员工账号与角色/组织绑定'
})

watch(
  () => [queryParams.value.keyword, queryParams.value.pageSize, tab.value],
  () => {
    queryParams.value.pageNum = 1
  },
)

function collectDescendantIds(id: string): string[] {
  const out: string[] = []
  const walk = (pid: string) => {
    for (const c of resourceByParent.value.get(pid) ?? []) {
      out.push(c.id)
      walk(c.id)
    }
  }
  walk(id)
  return out
}

function collectAncestorIds(id: string): string[] {
  const out: string[] = []
  let cur = resourceParentOf.value.get(id) ?? null
  while (cur) {
    out.push(cur)
    cur = resourceParentOf.value.get(cur) ?? null
  }
  return out
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function roleNames(row: IAMStaff) {
  return (row.roles || []).map((r) => r.name).join('、') || '—'
}

function orgUnitNames(row: IAMStaff) {
  return (row.org_units || []).map((o) => o.name).join('、') || '—'
}

function handleQuery() {
  queryParams.value.pageNum = 1
  if (tab.value === 'staff') load()
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
  if (tab.value === 'staff') load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'units') {
      units.value = (await marketGet<OrgUnit[] | null>(`${MARKET_API_PREFIX}/org/units`)) ?? []
    } else if (tab.value === 'roles') {
      const [rs, res] = await Promise.all([
        marketGet<IAMRole[] | null>(`${MARKET_API_PREFIX}/org/roles`),
        marketGet<IAMResource[] | null>(`${MARKET_API_PREFIX}/org/resources`).catch(() => []),
      ])
      roles.value = rs ?? []
      resources.value = res ?? []
      if (selectedRoleId.value && !roles.value.some((r) => r.id === selectedRoleId.value)) {
        selectedRoleId.value = null
      }
      if (!selectedRoleId.value && roles.value.length) {
        await selectRole(roles.value[0].id)
      } else if (selectedRoleId.value) {
        await loadRoleDetail(selectedRoleId.value)
      }
    } else {
      const q = queryParams.value.keyword.trim()
        ? `?q=${encodeURIComponent(queryParams.value.keyword.trim())}`
        : ''
      const [st, rs, us] = await Promise.all([
        marketGet<IAMStaff[] | null>(`${MARKET_API_PREFIX}/org/staff${q}`),
        marketGet<IAMRole[] | null>(`${MARKET_API_PREFIX}/org/roles`).catch(() => []),
        marketGet<OrgUnit[] | null>(`${MARKET_API_PREFIX}/org/units`).catch(() => []),
      ])
      staff.value = st ?? []
      roles.value = rs ?? []
      units.value = us ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>, reload = true) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    if (reload) await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openUnit(u?: OrgUnit) {
  if (busy.value) return
  editingUnit.value = u ?? null
  Object.assign(unitForm, {
    name: u?.name ?? '',
    code: u?.code ?? '',
    parent_id: u?.parent_id != null ? String(u.parent_id) : '',
    sort: u?.sort ?? 0,
    enabled: u?.enabled ?? true,
  })
  error.value = ''
  msg.value = ''
  showUnit.value = true
}

function closeUnit() {
  if (!busy.value) showUnit.value = false
}

async function saveUnit() {
  if (busy.value) return
  const body = {
    name: unitForm.name.trim(),
    code: unitForm.code.trim(),
    parent_id: unitForm.parent_id ? Number(unitForm.parent_id) : null,
    sort: Number(unitForm.sort) || 0,
    enabled: unitForm.enabled,
  }
  if (!body.name || !body.code) {
    error.value = '请填写名称与编码'
    return
  }
  const ok = editingUnit.value
    ? await run('组织已保存', () =>
        marketSend('PUT', `${MARKET_API_PREFIX}/org/units/${editingUnit.value!.id}`, body),
      )
    : await run('组织已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/org/units`, body))
  if (ok) closeUnit()
}

function deleteUnit(u: OrgUnit) {
  if (!busy.value) pendingDelete.value = { type: 'unit', unit: u }
}

function openRole(r?: IAMRole) {
  if (busy.value) return
  if (hasRoleDraft.value && !showRole.value) {
    requestDiscard(() => openRole(r))
    return
  }
  editingRole.value = r ?? null
  Object.assign(roleForm, {
    name: r?.name ?? '',
    code: r?.code ?? '',
    description: r?.description ?? '',
  })
  error.value = ''
  msg.value = ''
  showRole.value = true
}

function closeRole() {
  if (!busy.value) showRole.value = false
}

async function saveRole() {
  if (busy.value) return
  if (!roleForm.name.trim() || !roleForm.code.trim()) {
    error.value = '请填写角色名称与编码'
    return
  }
  const body = {
    name: roleForm.name.trim(),
    code: roleForm.code.trim(),
    description: roleForm.description.trim(),
  }
  const ok = editingRole.value
    ? await run('角色已保存', () =>
        marketSend('PUT', `${MARKET_API_PREFIX}/org/roles/${editingRole.value!.id}`, body),
      )
    : await run('角色已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/org/roles`, body))
  if (ok) {
    closeRole()
    if (!editingRole.value) {
      const created = roles.value.find((x) => x.code === body.code)
      if (created) await selectRole(created.id)
    }
  }
}

function deleteRole(r: IAMRole) {
  if (!busy.value) pendingDelete.value = { type: 'role', role: r }
}

async function confirmDelete() {
  const pending = pendingDelete.value
  if (!pending || busy.value) return
  pendingDelete.value = null
  const id = pending.type === 'unit' ? pending.unit!.id : pending.role!.id
  const path = pending.type === 'unit' ? 'units' : 'roles'
  const ok = await run('已删除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/org/${path}/${id}`))
  if (ok && pending.type === 'role' && selectedRoleId.value === id) selectedRoleId.value = null
}

function resetRoleDraft() {
  checked.value = [...checkedBaseline.value]
  roleMembers.value = [...membersBaseline.value]
  pickHits.value = []
  memberSearchError.value = ''
  memberSearched.value = false
}

function requestDiscard(action: () => void | Promise<void>) {
  if (busy.value) return
  if (!hasRoleDraft.value) return action()
  discardAction = action
  showDiscardConfirm.value = true
}

async function confirmDiscard() {
  const action = discardAction
  discardAction = null
  showDiscardConfirm.value = false
  resetRoleDraft()
  if (action) await action()
}

function cancelDiscard() {
  discardAction = null
  showDiscardConfirm.value = false
}

function requestTabChange(next: typeof tab.value) {
  if (next === tab.value) return
  return requestDiscard(() => { tab.value = next })
}

function requestRoleTabChange(next: typeof roleDetailTab.value) {
  if (next === roleDetailTab.value) return
  return requestDiscard(() => { roleDetailTab.value = next })
}

function requestRoleSelection(id: number) {
  if (id === selectedRoleId.value) return
  return requestDiscard(() => selectRole(id))
}

function requestRefresh() {
  if (loading.value || busy.value) return
  return requestDiscard(load)
}

async function selectRole(id: number) {
  selectedRoleId.value = id
  roleDetailTab.value = 'perms'
  checked.value = []
  roleMembers.value = []
  checkedBaseline.value = []
  membersBaseline.value = []
  await loadRoleDetail(id)
}

async function loadRoleDetail(id: number) {
  const requestId = ++roleDetailRequest
  detailLoading.value = true
  try {
    const [res, mem] = await Promise.all([
      marketGet<{ resource_ids: string[] }>(`${MARKET_API_PREFIX}/org/roles/${id}/resources`),
      marketGet<{ members: IAMStaff[] }>(`${MARKET_API_PREFIX}/org/roles/${id}/members`),
    ])
    if (requestId !== roleDetailRequest || selectedRoleId.value !== id) return
    checked.value = [...(res.resource_ids ?? [])]
    roleMembers.value = [...(mem.members ?? [])]
    checkedBaseline.value = [...checked.value]
    membersBaseline.value = [...roleMembers.value]
  } catch (e) {
    if (requestId === roleDetailRequest) error.value = e instanceof Error ? e.message : '加载角色详情失败'
  } finally {
    if (requestId === roleDetailRequest) detailLoading.value = false
  }
}

function toggleRes(id: string) {
  if (busy.value || detailLoading.value) return
  const set = new Set(checked.value)
  const kids = collectDescendantIds(id)
  if (set.has(id)) {
    set.delete(id)
    for (const k of kids) set.delete(k)
    for (const anc of collectAncestorIds(id)) {
      const siblings = resourceByParent.value.get(anc) ?? []
      if (!siblings.some((s) => set.has(s.id) || collectDescendantIds(s.id).some((d) => set.has(d)))) {
        set.delete(anc)
      }
    }
  } else {
    set.add(id)
    for (const k of kids) set.add(k)
    for (const anc of collectAncestorIds(id)) set.add(anc)
  }
  checked.value = [...set]
}

async function savePerm() {
  const role = selectedRole.value
  if (!role || busy.value) return
  const resourceIds = [...checked.value]
  const ok = await run(
    '菜单权限已保存',
    () => marketSend('PUT', `${MARKET_API_PREFIX}/org/roles/${role.id}/resources`, { resource_ids: resourceIds }),
    false,
  )
  if (ok) checkedBaseline.value = [...resourceIds]
}

async function searchUsers() {
  if (busy.value || memberSearchLoading.value) return
  memberSearchLoading.value = true
  memberSearchError.value = ''
  memberSearched.value = true
  try {
    pickHits.value = (await marketGet<IAMStaff[] | null>(
      `${MARKET_API_PREFIX}/org/users?q=${encodeURIComponent(memberQ.value.trim())}`,
    )) ?? []
  } catch (e) {
    pickHits.value = []
    memberSearchError.value = e instanceof Error ? e.message : '成员搜索失败'
  } finally {
    memberSearchLoading.value = false
  }
}

function addMember(u: IAMStaff) {
  if (busy.value) return
  if (roleMembers.value.some((m) => m.id === u.id)) {
    msg.value = '已在成员列表中'
    return
  }
  roleMembers.value = [...roleMembers.value, u]
  pickHits.value = []
  memberQ.value = ''
  memberSearched.value = false
}

function removeMember(id: number) {
  if (!busy.value) roleMembers.value = roleMembers.value.filter((m) => m.id !== id)
}

async function saveMembers() {
  const role = selectedRole.value
  if (!role || busy.value) return
  const members = [...roleMembers.value]
  const ok = await run(
    '成员已保存（需重新登录生效）',
    () => marketSend('PUT', `${MARKET_API_PREFIX}/org/roles/${role.id}/members`, {
      user_ids: members.map((member) => member.id),
    }),
    false,
  )
  if (ok) {
    membersBaseline.value = [...members]
    await loadRoleDetail(role.id)
  }
}

function openStaff(s?: IAMStaff) {
  if (busy.value) return
  editingStaff.value = s ?? null
  Object.assign(staffForm, {
    email: s?.email ?? '',
    password: '',
    role_ids: (s?.roles ?? []).map((r) => r.id),
    org_unit_ids: (s?.org_units ?? []).map((o) => o.id),
  })
  error.value = ''
  msg.value = ''
  showStaff.value = true
}

function closeStaff() {
  if (!busy.value) showStaff.value = false
}

function toggleId(list: number[], id: number) {
  const set = new Set(list)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  return [...set]
}

async function saveStaff() {
  if (busy.value) return
  if (editingStaff.value) {
    if (
      await run('员工授权已保存', () =>
        marketSend('PUT', `${MARKET_API_PREFIX}/org/staff/${editingStaff.value!.id}`, {
          role_ids: staffForm.role_ids,
          org_unit_ids: staffForm.org_unit_ids,
        }),
      )
    ) {
      closeStaff()
    }
    return
  }
  if (!staffForm.email.trim() || !staffForm.password) {
    error.value = '请填写邮箱与初始密码'
    return
  }
  if (
    await run('员工已创建', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/org/staff`, {
        email: staffForm.email.trim(),
        password: staffForm.password,
        role_ids: staffForm.role_ids,
        org_unit_ids: staffForm.org_unit_ids,
      }),
    )
  ) {
    closeStaff()
  }
}

function openCreate() {
  if (tab.value === 'units') openUnit()
  else if (tab.value === 'roles') openRole()
  else openStaff()
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
})

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!hasRoleDraft.value) return
  event.preventDefault()
  event.returnValue = ''
}

onMounted(() => {
  window.addEventListener('beforeunload', handleBeforeUnload)
  load()
})
onUnmounted(() => {
  roleDetailRequest += 1
  window.removeEventListener('beforeunload', handleBeforeUnload)
})
</script>

<template>
  <section id="market-org-page" class="org-shell">
    <TablePageLayout content-mode="content">
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ pageTitle }}</h2>
          <p class="y-page-desc">{{ pageDesc }}</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy" @click="openCreate">
          <Icon name="plus" size="sm" />
          {{ tab === 'units' ? '新建组织' : tab === 'roles' ? '新建角色' : '新建员工' }}
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading || busy" @click="requestRefresh">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error && !formDialogOpen" class="y-msg y-msg--error mb-2" role="alert">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2" role="status" aria-live="polite">{{ msg }}</p>

        <nav class="y-tabs mb-3" aria-label="组织管理视图">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'units' }" :aria-pressed="tab === 'units'" :disabled="busy" @click="requestTabChange('units')">组织</button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'roles' }" :aria-pressed="tab === 'roles'" :disabled="busy" @click="requestTabChange('roles')">角色</button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'staff' }" :aria-pressed="tab === 'staff'" :disabled="busy" @click="requestTabChange('staff')">员工</button>
        </nav>

        <div v-if="tab !== 'roles'" class="section-block">
          <div class="section-title">筛选条件</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label" for="org-keyword">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  id="org-keyword"
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  :placeholder="tab === 'staff' ? '邮箱 / ID' : '名称 / 编码'"
                  @keyup.enter="handleQuery"
                />
              </div>
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
        <template v-if="tab === 'roles'">
          <div class="org-role-layout">
            <div class="org-split">
              <aside class="org-side" aria-label="角色列表">
                <p v-if="loading" class="y-msg" role="status">加载中…</p>
                <EmptyState v-else-if="!roles.length" title="暂无角色" description="点右上角「新建角色」创建" />
                <button
                  v-for="r in roles"
                  v-else
                  :key="r.id"
                  type="button"
                  class="org-side__item"
                  :class="{ 'is-on': r.id === selectedRoleId }"
                  :aria-pressed="r.id === selectedRoleId"
                  :disabled="busy || detailLoading"
                  @click="requestRoleSelection(r.id)"
                >
                  <span class="org-side__title">{{ r.name }}</span>
                  <span class="org-side__meta">{{ r.code }} · {{ r.member_count ?? 0 }} 人</span>
                </button>
              </aside>
              <section class="org-main" :aria-busy="detailLoading || busy">
                <p v-if="!selectedRole" class="y-msg">请选择左侧角色</p>
                <template v-else>
                  <div class="org-main__head">
                    <div class="org-main__title">
                      <h3>{{ selectedRole.name }}</h3>
                      <p class="hint">{{ selectedRole.description || '暂无说明' }}</p>
                    </div>
                    <div class="role-head-actions">
                      <button type="button" class="btn btn-secondary page-action-btn" :disabled="busy" @click="openRole(selectedRole)">
                        编辑
                      </button>
                      <button
                        type="button"
                        class="btn btn-secondary page-action-btn"
                        :disabled="busy"
                        @click="deleteRole(selectedRole)"
                      >
                        删除
                      </button>
                    </div>
                  </div>
                  <p v-if="hasRoleDraft" class="draft-hint" role="status" aria-live="polite">有未保存的权限或成员草稿</p>
                  <nav class="y-tabs mb-3" aria-label="角色详情视图">
                    <button
                      type="button"
                      class="y-tab"
                      :class="{ 'is-on': roleDetailTab === 'perms' }"
                      :aria-pressed="roleDetailTab === 'perms'"
                      :disabled="busy"
                      @click="requestRoleTabChange('perms')"
                    >
                      权限
                    </button>
                    <button
                      type="button"
                      class="y-tab"
                      :class="{ 'is-on': roleDetailTab === 'members' }"
                      :aria-pressed="roleDetailTab === 'members'"
                      :disabled="busy"
                      @click="requestRoleTabChange('members')"
                    >
                      成员
                    </button>
                  </nav>
                  <p v-if="detailLoading" class="y-msg" role="status">加载详情…</p>
                  <template v-else-if="roleDetailTab === 'perms'">
                    <p v-if="!resourceTree.length" class="y-msg">暂无资源树</p>
                    <div v-else class="perm-list">
                      <label
                        v-for="r in resourceTree"
                        :key="r.id"
                        class="y-check perm-item"
                        :style="{ paddingLeft: `${8 + r.depth * 16}px` }"
                      >
                        <input type="checkbox" :checked="checked.includes(r.id)" :disabled="busy" @change="toggleRes(r.id)" />
                        <span>{{ r.name }}</span>
                        <span class="perm-kind">{{ kindLabel[r.kind] || r.kind }}</span>
                      </label>
                    </div>
                    <div class="pay-actions">
                      <button type="button" class="btn btn-primary" :disabled="busy" @click="savePerm">保存权限</button>
                    </div>
                  </template>
                  <template v-else>
                    <div class="filter-grid member-search mb-3" :aria-busy="memberSearchLoading">
                      <div class="filter-item">
                        <label class="filter-label" for="org-member-search">关键词</label>
                        <div class="relative flex-1 w-full min-w-0">
                          <input
                            id="org-member-search"
                            v-model="memberQ"
                            class="filter-input w-full"
                            type="search"
                            placeholder="搜索用户邮箱"
                            :disabled="busy || memberSearchLoading"
                            @keyup.enter="searchUsers"
                          />
                        </div>
                      </div>
                      <div class="filter-actions lg:col-span-4 sm:col-span-2">
                        <button type="button" class="btn btn-secondary" :disabled="busy || memberSearchLoading" @click="memberQ = ''; searchUsers()">
                          <Icon name="refresh" size="sm" />
                          重置
                        </button>
                        <button type="button" class="btn btn-primary" :disabled="busy || memberSearchLoading" @click="searchUsers">
                          <Icon name="search" size="sm" />
                          {{ memberSearchLoading ? '查询中…' : '查询' }}
                        </button>
                      </div>
                    </div>
                    <p v-if="memberSearchLoading" class="y-msg" role="status" aria-live="polite">正在搜索成员…</p>
                    <p v-else-if="memberSearchError" class="y-msg y-msg--error" role="alert">{{ memberSearchError }}</p>
                    <p v-else-if="memberSearched && !pickHits.length" class="y-msg" role="status" aria-live="polite">未找到匹配用户</p>
                    <div v-if="pickHits.length" class="pick-hits">
                      <button
                        v-for="u in pickHits"
                        :key="u.id"
                        type="button"
                        class="y-link pick-hit"
                        :disabled="busy"
                        @click="addMember(u)"
                      >
                        添加 <span class="email-cell">{{ u.email }}</span>
                      </button>
                    </div>
                    <DataTable
                      :columns="memberColumns"
                      :data="roleMembers"
                      :loading="false"
                      :expandable-actions="false"
                    >
                      <template #cell-id="{ row }">{{ row.id }}</template>
                      <template #cell-email="{ row }"><span class="email-cell">{{ row.email }}</span></template>
                      <template #cell-actions="{ row }">
                        <button type="button" class="y-link y-link--danger" :disabled="busy" @click="removeMember(row.id)">移除</button>
                      </template>
                      <template #empty>
                        <EmptyState title="暂无成员" description="上方搜索用户后添加" />
                      </template>
                    </DataTable>
                    <div class="pay-actions">
                      <button type="button" class="btn btn-primary" :disabled="busy" @click="saveMembers">保存成员</button>
                    </div>
                  </template>
                </template>
              </section>
            </div>
          </div>
        </template>

        <DataTable
          v-else
          :columns="columns"
          :data="pageRows"
          :loading="loading"
          :expandable-actions="false"
        >
          <template #cell-serial="{ row }">
            {{
              getSerialNumber(
                pageRows.findIndex((r) => ('id' in r ? r.id : 0) === row.id),
              )
            }}
          </template>
          <template #cell-id="{ row }">{{ row.id }}</template>
          <template #cell-name="{ row }">{{ row.name }}</template>
          <template #cell-code="{ row }">
            <span class="mono">{{ row.code }}</span>
          </template>
          <template #cell-parent_id="{ row }">{{ row.parent_id ?? '—' }}</template>
          <template #cell-sort="{ row }">{{ row.sort }}</template>
          <template #cell-enabled="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '启用' : '停用' }}
            </span>
          </template>
          <template #cell-email="{ row }"><span class="email-cell">{{ row.email }}</span></template>
          <template #cell-roles="{ row }"><span class="role-list">{{ roleNames(row) }}</span></template>
          <template #cell-org_units="{ row }"><span class="role-list">{{ orgUnitNames(row) }}</span></template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="tab === 'units'">
                <button type="button" class="y-link" @click="openUnit(row)">编辑</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="deleteUnit(row)">
                  删除
                </button>
              </template>
              <button v-else type="button" class="y-link" @click="openStaff(row)">授权</button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="tab === 'units' ? '暂无组织' : '暂无员工'"
              description="调整筛选条件后重试，或点右上角新建"
              :action-text="tab === 'units' ? '新建组织' : '新建员工'"
              @action="openCreate"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="tab !== 'roles' && total > 0"
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
      :show="showDiscardConfirm"
      title="放弃未保存草稿？"
      message="当前角色有未保存的权限或成员修改。继续切换或刷新将丢失这些草稿。"
      confirm-text="放弃草稿"
      cancel-text="取消"
      danger
      :busy="busy"
      @cancel="cancelDiscard"
      @confirm="confirmDiscard"
    />

    <BaseDialog
      :show="showUnit"
      :title="editingUnit ? `编辑组织 ${editingUnit.name}` : '新建组织'"
      :close-on-escape="!busy"
      @close="closeUnit"
    >
      <form id="org-unit-form" class="dialog-form" @submit.prevent="saveUnit">
        <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
        <label class="y-field" for="org-unit-name"><span>名称</span><input id="org-unit-name" v-model="unitForm.name" class="input w-full" /></label>
        <label class="y-field" for="org-unit-code">
          <span>编码</span>
          <input id="org-unit-code" v-model="unitForm.code" class="input w-full" :disabled="!!editingUnit" />
        </label>
        <label class="y-field" for="org-unit-parent">
          <span>父组织 ID（可空）</span>
          <input id="org-unit-parent" v-model="unitForm.parent_id" class="input w-full" />
        </label>
        <label class="y-field" for="org-unit-sort">
          <span>排序</span>
          <input id="org-unit-sort" v-model.number="unitForm.sort" type="number" class="input w-full" />
        </label>
        <label class="y-check" for="org-unit-enabled"><input id="org-unit-enabled" v-model="unitForm.enabled" type="checkbox" /> 启用</label>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeUnit">取消</button>
        <button type="submit" form="org-unit-form" class="btn btn-primary" :disabled="busy">保存</button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showRole"
      :title="editingRole ? `编辑角色 ${editingRole.name}` : '新建角色'"
      :close-on-escape="!busy"
      @close="closeRole"
    >
      <form id="org-role-form" class="dialog-form" @submit.prevent="saveRole">
        <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
        <label class="y-field" for="org-role-name"><span>名称</span><input id="org-role-name" v-model="roleForm.name" class="input w-full" /></label>
        <label class="y-field" for="org-role-code">
          <span>编码</span>
          <input id="org-role-code" v-model="roleForm.code" class="input w-full" :disabled="!!editingRole" />
        </label>
        <label class="y-field" for="org-role-description">
          <span>说明</span>
          <input id="org-role-description" v-model="roleForm.description" class="input w-full" />
        </label>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeRole">取消</button>
        <button type="submit" form="org-role-form" class="btn btn-primary" :disabled="busy">保存</button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showStaff"
      :title="editingStaff ? `授权 ${editingStaff.email}` : '新建员工'"
      width="wide"
      :close-on-escape="!busy"
      @close="closeStaff"
    >
      <form id="org-staff-form" class="dialog-form" @submit.prevent="saveStaff">
        <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
        <template v-if="!editingStaff">
          <label class="y-field" for="org-staff-email">
            <span>邮箱</span>
            <input id="org-staff-email" v-model="staffForm.email" type="email" autocomplete="email" class="input w-full" />
          </label>
          <label class="y-field" for="org-staff-password">
            <span>初始密码</span>
            <input id="org-staff-password" v-model="staffForm.password" type="password" autocomplete="new-password" class="input w-full" />
          </label>
        </template>
        <div class="y-field">
          <span>角色</span>
          <div class="auth-list">
            <label v-for="r in roles" :key="r.id" class="y-check">
              <input
                type="checkbox"
                :checked="staffForm.role_ids.includes(r.id)"
                :disabled="busy"
                @change="staffForm.role_ids = toggleId(staffForm.role_ids, r.id)"
              />
              <span>{{ r.name }}</span>
            </label>
          </div>
        </div>
        <div class="y-field">
          <span>组织</span>
          <div class="auth-list">
            <label v-for="u in units" :key="u.id" class="y-check">
              <input
                type="checkbox"
                :checked="staffForm.org_unit_ids.includes(u.id)"
                :disabled="busy"
                @change="staffForm.org_unit_ids = toggleId(staffForm.org_unit_ids, u.id)"
              />
              <span>{{ u.name }}</span>
            </label>
          </div>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeStaff">取消</button>
        <button type="submit" form="org-staff-form" class="btn btn-primary" :disabled="busy">保存</button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingDelete !== null"
      :title="pendingDelete?.type === 'unit' ? '删除组织' : '删除角色'"
      :message="deleteMessage"
      confirm-text="确认删除"
      cancel-text="取消"
      danger
      :busy="busy"
      @cancel="pendingDelete = null"
      @confirm="confirmDelete"
    />
  </section>
</template>

<style scoped>
#market-org-page.org-shell {
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

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
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
.status-dot[data-tone='ok'] {
  background: #16a34a;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.auth-list .y-check { min-width: 0; overflow-wrap: anywhere; }
.dialog-form { display: grid; gap: 12px; }
:global(.modal-header .modal-title) { min-width: 0; overflow-wrap: anywhere; }
.hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
  overflow-wrap: anywhere;
}
.org-role-layout { container-type: inline-size; min-width: 0; }
.org-split {
  display: grid;
  grid-template-columns: minmax(180px, 240px) minmax(0, 1fr);
  gap: 12px;
  min-height: 360px;
  min-width: 0;
  padding: 12px;
}
.org-side {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 10px;
  padding: 8px;
  overflow: auto;
  max-height: 70vh;
  min-width: 0;
}
.org-side__item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  min-width: 0;
  text-align: left;
  padding: 8px 10px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--color-text-primary, #1e293b);
  cursor: pointer;
}
.org-side__item.is-on {
  background: var(--color-bg-secondary, #f1f5f9);
}
.org-side__title {
  font-size: 13px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.org-side__meta {
  font-size: 11px;
  color: var(--color-text-secondary, #868686);
  overflow-wrap: anywhere;
}
.org-main {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 10px;
  padding: 12px;
  min-width: 0;
}
.org-main__head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}
.org-main__title { min-width: 0; }
.org-main__head h3 {
  margin: 0;
  font-size: 16px;
  overflow-wrap: anywhere;
}
.draft-hint { margin: 0 0 10px; font-size: 12px; color: var(--color-text-secondary, #64748b); }
.role-head-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.perm-list {
  display: grid;
  gap: 4px;
  max-height: 420px;
  overflow: auto;
  margin-bottom: 12px;
}
.perm-item {
  min-width: 0;
  padding: 4px 0;
  overflow-wrap: anywhere;
}
.perm-kind {
  margin-left: 6px;
  font-size: 11px;
  color: var(--color-text-secondary, #868686);
}
.pick-hits {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}
.pick-hit { display: inline-flex; min-width: 0; max-width: 100%; gap: 4px; }
.email-cell,
.role-list {
  display: inline-block;
  max-width: 20rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.pay-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

#market-org-page :deep(.filter-grid .select-trigger),
#market-org-page :deep(.filter-grid .select-trigger-sm),
#market-org-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-org-page :deep(.table-scroll-container table thead th) {
  background: var(--color-bg-secondary, #f5f7fa);
  color: var(--color-text-secondary, #606266);
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid var(--color-border, #ebeef5);
}

#market-org-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid var(--color-border, #ebeef5);
  font-size: 13px;
  color: var(--color-text-primary, #303133);
}

#market-org-page :deep(th.col-ops),
#market-org-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}

.y-field {
  display: grid;
  gap: 4px;
  min-width: 0;
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
@container (max-width: 700px) {
  .org-split { grid-template-columns: minmax(0, 1fr); }
  .org-side { max-height: 240px; }
  .org-main .member-search { grid-template-columns: minmax(0, 1fr); }
  .member-search .filter-actions { grid-column: 1 / -1; flex-wrap: wrap; }
}
@media (max-width: 767px) {
  .email-cell,
  .role-list {
    max-width: min(20rem, 58vw);
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .pay-actions { flex-wrap: wrap; }
}
</style>
