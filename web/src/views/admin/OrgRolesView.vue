<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { admin } from '@/api'
import type { IAMResource, IAMRole, IAMStaff } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import '@/styles/console-liquid.css'

const auth = useAuth()
const toast = useToast()
const roles = useLoader(() => admin.orgRoles(), [] as IAMRole[] | null)
const resources = useLoader(() => admin.orgResources(), [] as IAMResource[] | null)
const { busy, run } = useAction()

const selectedId = ref<number | null>(null)
const detailTab = ref<'info' | 'perms' | 'members'>('info')
const checked = ref<string[]>([])
const members = ref<IAMStaff[]>([])
const memberQ = ref('')
const pickHits = ref<IAMStaff[]>([])
const detailLoading = ref(false)

const showEdit = ref(false)
const showMenus = ref(false)
const showResEdit = ref(false)
const showDeleteRole = ref(false)
const editing = ref<IAMRole | null>(null)
const editingRes = ref<IAMResource | null>(null)
const pendingDeleteRole = ref<IAMRole | null>(null)
const form = reactive({ code: '', name: '', description: '' })
const resForm = reactive({ name: '', sort: '0', enabled: true, path: '', icon: '' })

const kindLabel: Record<string, string> = { dir: '目录', menu: '菜单', action: '功能' }

onMounted(async () => {
  await Promise.all([roles.load(), resources.load()])
  if (!selectedId.value && (roles.data.value?.length ?? 0) > 0) {
    selectRole(roles.data.value![0].id)
  }
})

const selected = computed(() => (roles.data.value ?? []).find((r) => r.id === selectedId.value) ?? null)

const resourceByParent = computed(() => {
  const list = resources.data.value ?? []
  const byParent = new Map<string | null, IAMResource[]>()
  for (const r of list) {
    const p = r.parent_id ?? null
    if (!byParent.has(p)) byParent.set(p, [])
    byParent.get(p)!.push(r)
  }
  for (const arr of byParent.values()) arr.sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id))
  return byParent
})

const resourceTree = computed(() => {
  const byParent = resourceByParent.value
  const out: (IAMResource & { depth: number })[] = []
  function walk(parent: string | null, depth: number) {
    for (const r of byParent.get(parent) ?? []) {
      out.push({ ...r, depth })
      walk(r.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
})

const resourceParentOf = computed(() => {
  const m = new Map<string, string | null>()
  for (const r of resources.data.value ?? []) m.set(r.id, r.parent_id ?? null)
  return m
})

const permMenuCount = computed(() => {
  const set = new Set(checked.value)
  return resourceTree.value.filter((r) => r.kind === 'menu' && set.has(r.id)).length
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

async function selectRole(id: number) {
  selectedId.value = id
  detailTab.value = 'info'
  await loadDetail(id)
}

async function loadDetail(id: number) {
  detailLoading.value = true
  try {
    const [res, mem] = await Promise.all([admin.orgRoleResources(id), admin.orgRoleMembers(id)])
    checked.value = res.resource_ids ?? []
    members.value = mem.members ?? []
  } catch (e) {
    toast.error(e)
  } finally {
    detailLoading.value = false
  }
}

watch(
  () => roles.data.value,
  (list) => {
    if (!list?.length) {
      selectedId.value = null
      return
    }
    if (!selectedId.value || !list.some((r) => r.id === selectedId.value)) {
      selectRole(list[0].id)
    }
  },
)

function openNew() {
  editing.value = null
  Object.assign(form, { code: '', name: '', description: '' })
  showEdit.value = true
}
function openEdit() {
  if (!selected.value) return
  editing.value = selected.value
  Object.assign(form, {
    code: selected.value.code,
    name: selected.value.name,
    description: selected.value.description,
  })
  showEdit.value = true
}
async function saveRole() {
  const body = { code: form.code.trim(), name: form.name.trim(), description: form.description.trim() }
  const ok = editing.value
    ? await run(() => admin.updateOrgRole(editing.value!.id, body), '已保存')
    : await run(() => admin.createOrgRole(body), '已创建')
  if (ok) {
    showEdit.value = false
    await roles.load()
    if (!editing.value) {
      const created = (roles.data.value ?? []).find((r) => r.code === body.code)
      if (created) await selectRole(created.id)
    } else {
      await loadDetail(editing.value.id)
    }
  }
}

function askRemove() {
  if (!selected.value) return
  pendingDeleteRole.value = selected.value
  showDeleteRole.value = true
}
async function confirmRemove() {
  const role = pendingDeleteRole.value
  if (!role) return
  if (await run(() => admin.deleteOrgRole(role.id), '已删除')) {
    showDeleteRole.value = false
    pendingDeleteRole.value = null
    selectedId.value = null
    await roles.load()
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
  if (!selected.value) return
  if (await run(() => admin.setOrgRoleResources(selected.value!.id, checked.value), '菜单权限已保存')) {
    await roles.load()
  }
}

async function searchUsers() {
  pickHits.value = (await admin.orgUsers(memberQ.value.trim())) ?? []
}
async function addMember(u: IAMStaff) {
  if (members.value.some((m) => m.id === u.id)) {
    toast.info('已在成员列表中')
    return
  }
  members.value = [...members.value, u]
  pickHits.value = []
  memberQ.value = ''
}
function removeMember(id: number) {
  members.value = members.value.filter((m) => m.id !== id)
}
async function saveMembers() {
  if (!selected.value) return
  const user_ids = members.value.map((m) => m.id)
  if (await run(() => admin.setOrgRoleMembers(selected.value!.id, user_ids), '成员已保存')) {
    await roles.load()
    await loadDetail(selected.value.id)
  }
}

function openMenus() {
  showMenus.value = true
  resources.load()
}
function openResEdit(r: IAMResource) {
  editingRes.value = r
  Object.assign(resForm, { name: r.name, sort: String(r.sort), enabled: r.enabled, path: r.path, icon: r.icon })
  showResEdit.value = true
}
async function saveRes() {
  if (!editingRes.value) return
  const ok = await run(
    () =>
      admin.updateOrgResource(editingRes.value!.id, {
        name: resForm.name.trim(),
        sort: Number(resForm.sort) || 0,
        enabled: resForm.enabled,
        path: resForm.path.trim(),
        icon: resForm.icon.trim(),
      }),
    '菜单已保存',
  )
  if (ok) {
    showResEdit.value = false
    resources.load()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-org-roles"
    plain
    title="角色管理"
    description="左侧选角色，右侧看详情、权限与成员。改成员后需重新登录生效。"
  >
    <template #actions>
      <button type="button" class="btn btn-secondary" @click="openMenus">菜单管理</button>
      <button type="button" class="btn btn-primary" @click="openNew">新建角色</button>
    </template>

    <div class="org-split">
      <aside class="org-split__side">
        <div class="org-split__side-head">角色</div>
        <div v-if="roles.loading.value" class="org-split__empty">加载中…</div>
        <button
          v-for="r in roles.data.value ?? []"
          :key="r.id"
          type="button"
          class="org-split__item"
          :class="{ 'org-split__item--active': r.id === selectedId }"
          @click="selectRole(r.id)"
        >
          <span class="org-split__item-title">{{ r.name }}</span>
          <span class="org-split__item-meta">{{ r.code }} · {{ r.member_count ?? 0 }} 人</span>
        </button>
        <div v-if="!(roles.data.value ?? []).length && !roles.loading.value" class="org-split__empty">暂无角色</div>
      </aside>

      <section class="org-split__main">
        <div v-if="!selected" class="org-split__empty org-split__empty--main">请选择左侧角色</div>
        <template v-else>
          <div class="org-split__main-head">
            <div class="min-w-0">
              <h2 class="org-split__title">{{ selected.name }}</h2>
              <p class="org-split__desc">{{ selected.description || '暂无说明' }}</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="openEdit">编辑</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="askRemove">删除</button>
            </div>
          </div>

          <div class="org-stats">
            <div class="org-stat">
              <div class="org-stat__n">{{ selected.member_count ?? members.length }}</div>
              <div class="org-stat__l">成员</div>
            </div>
            <div class="org-stat">
              <div class="org-stat__n">{{ permMenuCount }}</div>
              <div class="org-stat__l">菜单权限</div>
            </div>
            <div class="org-stat">
              <div class="org-stat__n">{{ checked.length }}</div>
              <div class="org-stat__l">资源节点</div>
            </div>
          </div>

          <div class="org-tabs" role="tablist">
            <button type="button" class="org-tab" :class="{ 'org-tab--active': detailTab === 'info' }" @click="detailTab = 'info'">
              概览
            </button>
            <button type="button" class="org-tab" :class="{ 'org-tab--active': detailTab === 'perms' }" @click="detailTab = 'perms'">
              权限
            </button>
            <button
              type="button"
              class="org-tab"
              :class="{ 'org-tab--active': detailTab === 'members' }"
              @click="detailTab = 'members'"
            >
              成员
            </button>
          </div>

          <div v-if="detailLoading" class="org-split__empty">加载详情…</div>

          <div v-else-if="detailTab === 'info'" class="org-panel">
            <dl class="org-kv">
              <div><dt>编码</dt><dd><code>{{ selected.code }}</code></dd></div>
              <div><dt>成员数</dt><dd>{{ selected.member_count ?? members.length }}</dd></div>
              <div><dt>说明</dt><dd>{{ selected.description || '—' }}</dd></div>
            </dl>
            <p class="mt-4 text-sm text-gray-500">在「权限」「成员」页签中维护控制台菜单与人员；保存后成员需重新登录。</p>
          </div>

          <div v-else-if="detailTab === 'perms'" class="org-panel">
            <p class="mb-3 text-sm text-gray-500">勾选目录会自动勾选下级；勾选菜单会自动勾选上级。</p>
            <div class="max-h-[48vh] space-y-0.5 overflow-y-auto rounded-xl border border-black/5 p-2">
              <label
                v-for="r in resourceTree"
                :key="r.id"
                class="flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-black/[0.03]"
                :style="{ paddingLeft: `${8 + r.depth * 16}px` }"
              >
                <input type="checkbox" :checked="checked.includes(r.id)" :disabled="!auth.isAdmin" @change="toggleRes(r.id)" />
                <span :class="r.kind === 'dir' ? 'font-medium' : ''">{{ r.name }}</span>
                <span
                  class="lg-chip"
                  :class="r.kind === 'dir' ? 'is-off' : r.kind === 'menu' ? 'is-info' : 'is-warn'"
                >
                  {{ kindLabel[r.kind] || r.kind }}
                </span>
              </label>
            </div>
            <div class="mt-3 flex justify-end">
              <button
                type="button"
                class="btn btn-primary"
                :disabled="busy || !auth.isAdmin"
                :title="auth.isAdmin ? '' : '仅超管可改权限'"
                @click="savePerm"
              >
                保存权限
              </button>
            </div>
          </div>

          <div v-else class="org-panel">
            <div class="mb-3 flex flex-wrap gap-2">
              <input
                v-model="memberQ"
                class="input max-w-xs flex-1"
                placeholder="搜索邮箱或用户 ID"
                @keyup.enter="searchUsers"
              />
              <button type="button" class="btn btn-secondary" :disabled="busy || !auth.isAdmin" @click="searchUsers">
                搜索添加
              </button>
            </div>
            <ul v-if="pickHits.length" class="mb-4 max-h-36 overflow-y-auto rounded-xl border border-black/5 text-sm">
              <li
                v-for="u in pickHits"
                :key="u.id"
                class="flex items-center justify-between border-b border-black/5 px-3 py-2 last:border-0"
              >
                <span>#{{ u.id }} {{ u.email }}</span>
                <button type="button" class="btn-ghost btn-sm" :disabled="!auth.isAdmin" @click="addMember(u)">加入</button>
              </li>
            </ul>
            <div class="overflow-hidden rounded-xl border border-black/5">
              <table class="w-full text-left text-sm">
                <thead class="bg-black/[0.02] text-xs text-gray-500">
                  <tr>
                    <th class="px-3 py-2 font-semibold">ID</th>
                    <th class="px-3 py-2 font-semibold">邮箱</th>
                    <th class="px-3 py-2 font-semibold">状态</th>
                    <th class="px-3 py-2 font-semibold">操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="m in members" :key="m.id" class="border-t border-black/5">
                    <td class="px-3 py-2.5">{{ m.id }}</td>
                    <td class="px-3 py-2.5">{{ m.email }}</td>
                    <td class="px-3 py-2.5">
                      <span
                        class="lg-chip"
                        :class="m.status === 'active' ? 'is-ok' : m.status === 'frozen' ? 'is-danger' : 'is-off'"
                      >
                        {{ m.status === 'active' ? '正常' : m.status === 'frozen' ? '冻结' : m.status }}
                      </span>
                    </td>
                    <td class="px-3 py-2.5">
                      <button type="button" class="btn-ghost btn-sm" :disabled="!auth.isAdmin" @click="removeMember(m.id)">
                        移除
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!members.length">
                    <td colspan="4" class="px-3 py-8 text-center text-gray-400">暂无成员，搜索后加入</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="mt-3 flex justify-end">
              <button type="button" class="btn btn-primary" :disabled="busy || !auth.isAdmin" @click="saveMembers">
                保存成员
              </button>
            </div>
          </div>
        </template>
      </section>
    </div>
  </ConsoleListPage>

  <ModalDialog v-model:open="showDeleteRole" title="删除角色">
    <p class="text-sm text-gray-700">确认删除角色「{{ pendingDeleteRole?.name }}」？已绑定成员将失去该角色权限。</p>
    <template #footer>
      <button class="btn-secondary" @click="showDeleteRole = false">取消</button>
      <button class="btn-danger" :disabled="busy" @click="confirmRemove">删除</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showEdit" :title="editing ? '编辑角色' : '新建角色'">
    <div>
      <label class="label" for="rcode">编码</label>
      <input id="rcode" v-model="form.code" class="input" />
    </div>
    <div>
      <label class="label" for="rname">名称</label>
      <input id="rname" v-model="form.name" class="input" />
    </div>
    <div>
      <label class="label" for="rdesc">说明</label>
      <textarea id="rdesc" v-model="form.description" class="input" rows="2" />
    </div>
    <template #footer>
      <button class="btn-secondary" @click="showEdit = false">取消</button>
      <button class="btn-primary" :disabled="busy" @click="saveRole">保存</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showMenus" title="菜单管理" wide>
    <p class="mb-4 text-sm leading-relaxed text-gray-500">
      维护目录 / 菜单的名称、排序与启用。角色「权限」页签勾选的就是这些节点。
    </p>
    <div class="max-h-[60vh] space-y-1 overflow-y-auto pr-1">
      <div
        v-for="r in resourceTree"
        :key="r.id"
        class="group flex items-start gap-3 rounded-xl px-3 py-3 transition-colors hover:bg-black/[0.03]"
        :class="r.kind === 'dir' ? 'bg-black/[0.02]' : ''"
        :style="{ marginLeft: `${r.depth * 18}px` }"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-[15px] font-medium text-gray-900" :class="r.kind === 'dir' ? 'font-semibold' : ''">
              {{ r.name }}
            </span>
            <span
              class="lg-chip"
              :class="r.kind === 'dir' ? 'is-off' : r.kind === 'menu' ? 'is-info' : 'is-warn'"
            >
              {{ kindLabel[r.kind] || r.kind }}
            </span>
            <span class="lg-chip" :class="r.enabled ? 'is-ok' : 'is-off'">
              {{ r.enabled ? '启用' : '停用' }}
            </span>
          </div>
          <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
            <code class="rounded bg-black/[0.04] px-1.5 py-0.5 font-mono text-[11px] text-gray-600">{{ r.id }}</code>
            <span v-if="r.path" class="font-mono text-[11px] text-gray-400">{{ r.path }}</span>
          </div>
        </div>
        <button
          type="button"
          class="btn-ghost btn-sm shrink-0 opacity-70 group-hover:opacity-100"
          :disabled="!auth.isAdmin"
          @click="openResEdit(r)"
        >
          编辑
        </button>
      </div>
    </div>
    <template #footer>
      <button class="btn-primary" @click="showMenus = false">关闭</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showResEdit" title="编辑菜单资源">
    <p class="mb-2 text-xs text-gray-500">资源码：{{ editingRes?.id }}</p>
    <div>
      <label class="label" for="mname">名称</label>
      <input id="mname" v-model="resForm.name" class="input" />
    </div>
    <div>
      <label class="label" for="mpath">路由 path</label>
      <input id="mpath" v-model="resForm.path" class="input" placeholder="菜单用，如 /console/funds" />
    </div>
    <div>
      <label class="label" for="micon">图标</label>
      <input id="micon" v-model="resForm.icon" class="input" />
    </div>
    <div>
      <label class="label" for="msort">排序</label>
      <input id="msort" v-model="resForm.sort" class="input" inputmode="numeric" />
    </div>
    <label class="flex items-center gap-2 text-sm"><input v-model="resForm.enabled" type="checkbox" /> 启用</label>
    <template #footer>
      <button class="btn-secondary" @click="showResEdit = false">取消</button>
      <button class="btn-primary" :disabled="busy || !auth.isAdmin" @click="saveRes">保存</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.org-split {
  display: flex;
  gap: 12px;
  min-height: min(70vh, 720px);
  align-items: stretch;
}
.org-split__side {
  width: 260px;
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
  letter-spacing: 0.02em;
}
.org-split__item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  width: 100%;
  text-align: left;
  padding: 10px 12px;
  border-radius: 10px;
  border: none;
  background: transparent;
  cursor: pointer;
  transition: background 0.15s;
}
.org-split__item:hover {
  background: rgba(5, 85, 206, 0.06);
}
.org-split__item--active {
  background: rgba(5, 85, 206, 0.1);
}
.org-split__item-title {
  font-size: 14px;
  font-weight: 600;
  color: #262626;
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
.org-stat__l {
  margin-top: 2px;
  font-size: 12px;
  color: #868686;
}
.org-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid #e8e8e8;
  padding-bottom: 0;
}
.org-tab {
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 500;
  color: #868686;
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  cursor: pointer;
}
.org-tab--active {
  color: #0555ce;
  border-bottom-color: #0555ce;
}
.org-panel {
  flex: 1;
  min-height: 0;
}
.org-kv {
  display: grid;
  gap: 12px;
  margin: 0;
}
.org-kv > div {
  display: grid;
  grid-template-columns: 88px 1fr;
  gap: 8px;
  font-size: 14px;
}
.org-kv dt {
  margin: 0;
  color: #868686;
}
.org-kv dd {
  margin: 0;
  color: #262626;
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
