<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

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
const keyword = ref('')

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

const showStaff = ref(false)
const editingStaff = ref<IAMStaff | null>(null)
const staffForm = reactive({
  email: '',
  password: '',
  role_ids: [] as number[],
  org_unit_ids: [] as number[],
})

const kindLabel: Record<string, string> = { dir: '目录', menu: '菜单', action: '功能' }

const filteredUnits = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return units.value
  return units.value.filter((u) => `${u.name} ${u.code}`.toLowerCase().includes(kw))
})

const filteredStaff = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return staff.value
  return staff.value.filter((s) => `${s.id} ${s.email}`.toLowerCase().includes(kw))
})

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
      const q = keyword.value.trim() ? `?q=${encodeURIComponent(keyword.value.trim())}` : ''
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
  editingUnit.value = u ?? null
  Object.assign(unitForm, {
    name: u?.name ?? '',
    code: u?.code ?? '',
    parent_id: u?.parent_id != null ? String(u.parent_id) : '',
    sort: u?.sort ?? 0,
    enabled: u?.enabled ?? true,
  })
  showUnit.value = true
}

async function saveUnit() {
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
  if (ok) showUnit.value = false
}

async function deleteUnit(u: OrgUnit) {
  if (!window.confirm(`删除组织「${u.name}」？`)) return
  await run('已删除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/org/units/${u.id}`))
}

function openRole(r?: IAMRole) {
  editingRole.value = r ?? null
  Object.assign(roleForm, {
    name: r?.name ?? '',
    code: r?.code ?? '',
    description: r?.description ?? '',
  })
  showRole.value = true
}

async function saveRole() {
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
    showRole.value = false
    if (!editingRole.value) {
      const created = roles.value.find((x) => x.code === body.code)
      if (created) await selectRole(created.id)
    }
  }
}

async function deleteRole(r: IAMRole) {
  if (!window.confirm(`删除角色「${r.name}」？`)) return
  const ok = await run('已删除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/org/roles/${r.id}`))
  if (ok && selectedRoleId.value === r.id) selectedRoleId.value = null
}

async function selectRole(id: number) {
  selectedRoleId.value = id
  roleDetailTab.value = 'perms'
  await loadRoleDetail(id)
}

async function loadRoleDetail(id: number) {
  detailLoading.value = true
  try {
    const [res, mem] = await Promise.all([
      marketGet<{ resource_ids: string[] }>(`${MARKET_API_PREFIX}/org/roles/${id}/resources`),
      marketGet<{ members: IAMStaff[] }>(`${MARKET_API_PREFIX}/org/roles/${id}/members`),
    ])
    checked.value = res.resource_ids ?? []
    roleMembers.value = mem.members ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载角色详情失败'
  } finally {
    detailLoading.value = false
  }
}

function toggleRes(id: string) {
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
  if (!selectedRole.value) return
  await run(
    '菜单权限已保存',
    () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/org/roles/${selectedRole.value!.id}/resources`, {
        resource_ids: checked.value,
      }),
    false,
  )
}

async function searchUsers() {
  pickHits.value =
    (await marketGet<IAMStaff[] | null>(
      `${MARKET_API_PREFIX}/org/users?q=${encodeURIComponent(memberQ.value.trim())}`,
    ).catch(() => [])) ?? []
}

function addMember(u: IAMStaff) {
  if (roleMembers.value.some((m) => m.id === u.id)) {
    msg.value = '已在成员列表中'
    return
  }
  roleMembers.value = [...roleMembers.value, u]
  pickHits.value = []
  memberQ.value = ''
}

function removeMember(id: number) {
  roleMembers.value = roleMembers.value.filter((m) => m.id !== id)
}

async function saveMembers() {
  if (!selectedRole.value) return
  const ok = await run(
    '成员已保存（需重新登录生效）',
    () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/org/roles/${selectedRole.value!.id}/members`, {
        user_ids: roleMembers.value.map((m) => m.id),
      }),
    false,
  )
  if (ok) await loadRoleDetail(selectedRole.value.id)
}

function openStaff(s?: IAMStaff) {
  editingStaff.value = s ?? null
  Object.assign(staffForm, {
    email: s?.email ?? '',
    password: '',
    role_ids: (s?.roles ?? []).map((r) => r.id),
    org_unit_ids: (s?.org_units ?? []).map((o) => o.id),
  })
  showStaff.value = true
}

function toggleId(list: number[], id: number) {
  const set = new Set(list)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  return [...set]
}

async function saveStaff() {
  if (editingStaff.value) {
    if (
      await run('员工授权已保存', () =>
        marketSend('PUT', `${MARKET_API_PREFIX}/org/staff/${editingStaff.value!.id}`, {
          role_ids: staffForm.role_ids,
          org_unit_ids: staffForm.org_unit_ids,
        }),
      )
    ) {
      showStaff.value = false
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
    showStaff.value = false
  }
}

watch(tab, () => {
  keyword.value = ''
  load()
})
onMounted(load)
</script>

<template>
  <section id="market-org-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">组织管理</h2>
        <p class="y-page-desc">组织单元、角色权限与员工授权</p>
      </div>
      <div class="y-header-actions">
        <button v-if="tab === 'units'" type="button" class="y-btn" @click="openUnit()">新建组织</button>
        <button v-else-if="tab === 'roles'" type="button" class="y-btn" @click="openRole()">新建角色</button>
        <button v-else type="button" class="y-btn" @click="openStaff()">新建员工</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'units' }" @click="tab = 'units'">组织</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'roles' }" @click="tab = 'roles'">角色</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'staff' }" @click="tab = 'staff'">员工</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <template v-if="tab !== 'roles'">
        <div class="y-search">
          <input
            v-model="keyword"
            type="search"
            :placeholder="tab === 'staff' ? '邮箱 / ID' : '名称 / 编码'"
            @keyup.enter="load"
          />
          <button v-if="tab === 'staff'" type="button" class="y-btn" @click="load">查询</button>
        </div>
      </template>

      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'units'">
        <p v-if="!filteredUnits.length" class="y-msg">暂无组织</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>名称</th>
              <th>编码</th>
              <th>父级</th>
              <th>排序</th>
              <th>状态</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in filteredUnits" :key="u.id">
              <td>{{ u.id }}</td>
              <td>{{ u.name }}</td>
              <td class="mono">{{ u.code }}</td>
              <td>{{ u.parent_id ?? '—' }}</td>
              <td>{{ u.sort }}</td>
              <td>
                <span :class="u.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ u.enabled ? '启用' : '停用' }}</span>
              </td>
              <td class="y-col-op">
                <button type="button" class="y-link" @click="openUnit(u)">编辑</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="deleteUnit(u)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </template>

      <template v-else-if="tab === 'roles'">
        <div class="org-split">
          <aside class="org-side">
            <p v-if="!roles.length" class="y-msg">暂无角色</p>
            <button
              v-for="r in roles"
              :key="r.id"
              type="button"
              class="org-side__item"
              :class="{ 'is-on': r.id === selectedRoleId }"
              @click="selectRole(r.id)"
            >
              <span class="org-side__title">{{ r.name }}</span>
              <span class="org-side__meta">{{ r.code }} · {{ r.member_count ?? 0 }} 人</span>
            </button>
          </aside>
          <section class="org-main">
            <p v-if="!selectedRole" class="y-msg">请选择左侧角色</p>
            <template v-else>
              <div class="org-main__head">
                <div>
                  <h3>{{ selectedRole.name }}</h3>
                  <p class="hint">{{ selectedRole.description || '暂无说明' }}</p>
                </div>
                <div class="y-header-actions">
                  <button type="button" class="y-btn y-btn--ghost" @click="openRole(selectedRole)">编辑</button>
                  <button type="button" class="y-btn y-btn--ghost" :disabled="busy" @click="deleteRole(selectedRole)">
                    删除
                  </button>
                </div>
              </div>
              <nav class="y-tabs">
                <button
                  type="button"
                  class="y-tab"
                  :class="{ 'is-on': roleDetailTab === 'perms' }"
                  @click="roleDetailTab = 'perms'"
                >
                  权限
                </button>
                <button
                  type="button"
                  class="y-tab"
                  :class="{ 'is-on': roleDetailTab === 'members' }"
                  @click="roleDetailTab = 'members'"
                >
                  成员
                </button>
              </nav>
              <p v-if="detailLoading" class="y-msg">加载详情…</p>
              <template v-else-if="roleDetailTab === 'perms'">
                <p v-if="!resourceTree.length" class="y-msg">暂无资源树</p>
                <div v-else class="perm-list">
                  <label
                    v-for="r in resourceTree"
                    :key="r.id"
                    class="y-check perm-item"
                    :style="{ paddingLeft: `${8 + r.depth * 16}px` }"
                  >
                    <input type="checkbox" :checked="checked.includes(r.id)" @change="toggleRes(r.id)" />
                    <span>{{ r.name }}</span>
                    <span class="perm-kind">{{ kindLabel[r.kind] || r.kind }}</span>
                  </label>
                </div>
                <div class="pay-actions">
                  <button type="button" class="y-btn" :disabled="busy" @click="savePerm">保存权限</button>
                </div>
              </template>
              <template v-else>
                <div class="y-search">
                  <input v-model="memberQ" type="search" placeholder="搜索用户邮箱" @keyup.enter="searchUsers" />
                  <button type="button" class="y-btn" @click="searchUsers">搜索</button>
                </div>
                <div v-if="pickHits.length" class="pick-hits">
                  <button
                    v-for="u in pickHits"
                    :key="u.id"
                    type="button"
                    class="y-link"
                    @click="addMember(u)"
                  >
                    添加 {{ u.email }} (#{{ u.id }})
                  </button>
                </div>
                <p v-if="!roleMembers.length" class="y-msg">暂无成员</p>
                <table v-else>
                  <thead>
                    <tr>
                      <th>ID</th>
                      <th>邮箱</th>
                      <th class="y-col-op">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="m in roleMembers" :key="m.id">
                      <td>{{ m.id }}</td>
                      <td>{{ m.email }}</td>
                      <td class="y-col-op">
                        <button type="button" class="y-link y-link--danger" @click="removeMember(m.id)">移除</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <div class="pay-actions">
                  <button type="button" class="y-btn" :disabled="busy" @click="saveMembers">保存成员</button>
                </div>
              </template>
            </template>
          </section>
        </div>
      </template>

      <template v-else>
        <p v-if="!filteredStaff.length" class="y-msg">暂无员工</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>邮箱</th>
              <th>角色</th>
              <th>组织</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in filteredStaff" :key="s.id">
              <td>{{ s.id }}</td>
              <td>{{ s.email }}</td>
              <td>{{ (s.roles || []).map((r) => r.name).join('、') || '—' }}</td>
              <td>{{ (s.org_units || []).map((o) => o.name).join('、') || '—' }}</td>
              <td class="y-col-op">
                <button type="button" class="y-link" @click="openStaff(s)">授权</button>
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>

    <div v-if="showUnit" class="y-modal-mask" @click.self="showUnit = false">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>{{ editingUnit ? '编辑组织' : '新建组织' }}</h3>
          <button type="button" class="y-link" @click="showUnit = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field"><span>名称</span><input v-model="unitForm.name" class="input w-full" /></label>
          <label class="y-field"><span>编码</span><input v-model="unitForm.code" class="input w-full" :disabled="!!editingUnit" /></label>
          <label class="y-field">
            <span>父组织 ID（可空）</span>
            <input v-model="unitForm.parent_id" class="input w-full" />
          </label>
          <label class="y-field"><span>排序</span><input v-model.number="unitForm.sort" type="number" class="input w-full" /></label>
          <label class="y-check"><input v-model="unitForm.enabled" type="checkbox" /> 启用</label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showUnit = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="saveUnit">保存</button>
        </footer>
      </div>
    </div>

    <div v-if="showRole" class="y-modal-mask" @click.self="showRole = false">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>{{ editingRole ? '编辑角色' : '新建角色' }}</h3>
          <button type="button" class="y-link" @click="showRole = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field"><span>名称</span><input v-model="roleForm.name" class="input w-full" /></label>
          <label class="y-field">
            <span>编码</span>
            <input v-model="roleForm.code" class="input w-full" :disabled="!!editingRole" />
          </label>
          <label class="y-field"><span>说明</span><input v-model="roleForm.description" class="input w-full" /></label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showRole = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="saveRole">保存</button>
        </footer>
      </div>
    </div>

    <div v-if="showStaff" class="y-modal-mask" @click.self="showStaff = false">
      <div class="y-modal">
        <header class="y-modal__head">
          <h3>{{ editingStaff ? `授权 ${editingStaff.email}` : '新建员工' }}</h3>
          <button type="button" class="y-link" @click="showStaff = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <template v-if="!editingStaff">
            <label class="y-field"><span>邮箱</span><input v-model="staffForm.email" class="input w-full" /></label>
            <label class="y-field"><span>初始密码</span><input v-model="staffForm.password" type="password" class="input w-full" /></label>
          </template>
          <div class="y-field">
            <span>角色</span>
            <div class="auth-list">
              <label v-for="r in roles" :key="r.id" class="y-check">
                <input
                  type="checkbox"
                  :checked="staffForm.role_ids.includes(r.id)"
                  @change="staffForm.role_ids = toggleId(staffForm.role_ids, r.id)"
                />
                {{ r.name }}
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
                  @change="staffForm.org_unit_ids = toggleId(staffForm.org_unit_ids, u.id)"
                />
                {{ u.name }}
              </label>
            </div>
          </div>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showStaff = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="saveStaff">保存</button>
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
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.y-link--danger {
  color: var(--color-error, #ff4d4f);
}
.hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.org-split {
  display: grid;
  grid-template-columns: minmax(180px, 240px) 1fr;
  gap: 12px;
  min-height: 360px;
}
.org-side {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 10px;
  padding: 8px;
  overflow: auto;
  max-height: 70vh;
}
.org-side__item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  text-align: left;
  padding: 8px 10px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  cursor: pointer;
}
.org-side__item.is-on {
  background: var(--color-bg-secondary, #f1f5f9);
}
.org-side__title {
  font-size: 13px;
  font-weight: 600;
}
.org-side__meta {
  font-size: 11px;
  color: var(--color-text-secondary, #868686);
}
.org-main {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 10px;
  padding: 12px;
}
.org-main__head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}
.org-main__head h3 {
  margin: 0;
  font-size: 16px;
}
.perm-list {
  display: grid;
  gap: 4px;
  max-height: 420px;
  overflow: auto;
  margin-bottom: 12px;
}
.perm-item {
  padding: 4px 0;
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
.pay-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
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
  max-height: min(90vh, 720px);
  overflow: auto;
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
.y-field > span:first-child {
  color: var(--color-text-secondary, #868686);
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
@media (max-width: 800px) {
  .org-split {
    grid-template-columns: 1fr;
  }
}
</style>
