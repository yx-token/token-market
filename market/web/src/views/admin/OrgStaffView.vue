<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { IAMRole, IAMStaff, OrgUnit } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useListQuery } from '@/composables/useListQuery'
import { useAuth } from '@/stores/auth'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import '@/styles/console-liquid.css'

const auth = useAuth()
const toast = useToast()
const { queryParams, resetQuery, handleQuery } = useListQuery({ keyword: '' })
const staff = useLoader(() => admin.orgStaff(queryParams.keyword || undefined), [] as IAMStaff[] | null)
const roles = useLoader(() => admin.orgRoles(), [] as IAMRole[] | null)
const units = useLoader(() => admin.orgUnits(), [] as OrgUnit[] | null)
const { busy, run } = useAction()

const show = ref(false)
const showConvert = ref(false)
const showCreate = ref(false)
const editing = ref<IAMStaff | null>(null)
const form = reactive({ role_ids: [] as number[], org_unit_ids: [] as number[], primary_org_id: '' as string })
const createForm = reactive({
  email: '',
  password: '',
  role_ids: [] as number[],
  org_unit_ids: [] as number[],
  primary_org_id: '' as string,
})
const addQ = ref('')
const addHits = ref<IAMStaff[]>([])

onMounted(() => {
  staff.load()
  roles.load()
  units.load()
})

function search() {
  handleQuery()
  staff.load()
}
function onReset() {
  resetQuery()
  staff.load()
}

function openAssign(u: IAMStaff) {
  editing.value = u
  form.role_ids = (u.roles ?? []).map((r) => r.id)
  form.org_unit_ids = (u.org_units ?? []).map((o) => o.id)
  form.primary_org_id = u.org_units?.[0] ? String(u.org_units[0].id) : ''
  show.value = true
}
function toggleRole(id: number, target: 'form' | 'create' = 'form') {
  const list = target === 'form' ? form : createForm
  const set = new Set(list.role_ids)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  list.role_ids = [...set]
}
function toggleOrg(id: number, target: 'form' | 'create' = 'form') {
  const list = target === 'form' ? form : createForm
  const set = new Set(list.org_unit_ids)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  list.org_unit_ids = [...set]
}
async function save() {
  if (!editing.value) return
  const body = {
    role_ids: form.role_ids,
    org_unit_ids: form.org_unit_ids,
    primary_org_id: form.primary_org_id ? Number(form.primary_org_id) : undefined,
  }
  if (await run(() => admin.assignOrgStaff(editing.value!.id, body), '已保存')) {
    show.value = false
    staff.load()
  }
}

async function searchConvert() {
  addHits.value = (await admin.orgUsers(addQ.value.trim())) ?? []
}
async function pickConvert(u: IAMStaff) {
  editing.value = u
  form.role_ids = (u.roles ?? []).map((r) => r.id)
  form.org_unit_ids = (u.org_units ?? []).map((o) => o.id)
  form.primary_org_id = u.org_units?.[0] ? String(u.org_units[0].id) : ''
  showConvert.value = false
  show.value = true
}

function openCreate() {
  Object.assign(createForm, {
    email: '',
    password: '',
    role_ids: [] as number[],
    org_unit_ids: [] as number[],
    primary_org_id: '',
  })
  showCreate.value = true
}
async function saveCreate() {
  if (!createForm.role_ids.length) {
    toast.error('请至少选择一个组织角色')
    return
  }
  const body = {
    email: createForm.email.trim(),
    password: createForm.password,
    role_ids: createForm.role_ids,
    org_unit_ids: createForm.org_unit_ids,
    primary_org_id: createForm.primary_org_id ? Number(createForm.primary_org_id) : undefined,
  }
  if (await run(() => admin.createOrgStaff(body), '员工已创建')) {
    showCreate.value = false
    staff.load()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-org-staff"
    title="员工授权"
    description="管理控制台员工：超管可新建账号并挂组织角色，也可把已有 C 端用户转为员工。前台注册不会产生员工。"
  >
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="!auth.isAdmin" @click="showConvert = true">
        转为员工
      </button>
      <button type="button" class="btn btn-primary" :disabled="!auth.isAdmin" @click="openCreate">新建员工</button>
    </template>
    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="邮箱 / 用户 ID" @keyup.enter="search" />
      </div>
      <div class="filter-actions lg:col-span-3">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      :columns="[
        { key: 'id', label: 'ID' },
        { key: 'email', label: '账号' },
        { key: 'roles', label: '组织角色' },
        { key: 'orgs', label: '部门' },
        { key: 'is_admin', label: '超管' },
        { key: 'status', label: '状态' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="staff.data.value ?? []"
      :loading="staff.loading.value"
      row-key="id"
      empty="暂无员工。可「新建员工」，或将已有 C 端用户「转为员工」。"
    >
      <template #cell-roles="{ row }">
        <span v-if="!(row.roles ?? []).length" class="text-gray-400">—</span>
        <span v-for="r in row.roles ?? []" :key="r.id" class="lg-chip is-info mr-1">{{ r.name }}</span>
      </template>
      <template #cell-orgs="{ row }">
        <span v-if="!(row.org_units ?? []).length" class="text-gray-400">—</span>
        <span v-for="o in row.org_units ?? []" :key="o.id" class="lg-chip is-violet mr-1">{{ o.name }}</span>
      </template>
      <template #cell-is_admin="{ row }">
        <span v-if="row.is_admin" class="lg-chip is-warn">超管</span>
        <span v-else class="text-gray-400">—</span>
      </template>
      <template #cell-status="{ row }">
        <span
          class="lg-chip"
          :class="row.status === 'active' ? 'is-ok' : row.status === 'frozen' ? 'is-danger' : 'is-off'"
        >
          {{ row.status === 'active' ? '正常' : row.status === 'frozen' ? '冻结' : row.status }}
        </span>
      </template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" :disabled="!auth.isAdmin" @click="openAssign(row)">设置角色 / 部门</button>
      </template>
    </DataTable>
  </ConsoleListPage>

  <ModalDialog v-model:open="show" :title="`员工权限 · ${editing?.email ?? ''}`" wide>
    <p class="mb-3 text-sm text-gray-500">清空全部组织角色即收回控制台权限；不影响其 C 端买卖能力。</p>
    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <p class="label mb-2">组织角色</p>
        <div class="max-h-56 space-y-1 overflow-y-auto text-sm">
          <label v-for="r in roles.data.value ?? []" :key="r.id" class="flex items-center gap-2">
            <input type="checkbox" :checked="form.role_ids.includes(r.id)" @change="toggleRole(r.id)" />
            {{ r.name }} <span class="text-xs text-gray-400">{{ r.code }}</span>
          </label>
        </div>
      </div>
      <div>
        <p class="label mb-2">部门</p>
        <div class="max-h-56 space-y-1 overflow-y-auto text-sm">
          <label v-for="o in units.data.value ?? []" :key="o.id" class="flex items-center gap-2">
            <input type="checkbox" :checked="form.org_unit_ids.includes(o.id)" @change="toggleOrg(o.id)" />
            {{ o.name }}
          </label>
        </div>
        <div class="mt-3">
          <label class="label" for="primary">主部门</label>
          <select id="primary" v-model="form.primary_org_id" class="input">
            <option value="">未指定</option>
            <option v-for="id in form.org_unit_ids" :key="id" :value="String(id)">
              {{ units.data.value?.find((o) => o.id === id)?.name || id }}
            </option>
          </select>
        </div>
      </div>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="show = false">取消</button>
      <button class="btn-primary" :disabled="busy || !auth.isAdmin" @click="save">保存</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showCreate" title="新建员工" wide>
    <p class="mb-3 text-sm text-gray-500">由超管开户并挂组织角色。不会设为超管；前台注册通道不会走这里。</p>
    <div class="grid gap-3 md:grid-cols-2">
      <div>
        <label class="label" for="staff-email">邮箱</label>
        <input id="staff-email" v-model="createForm.email" class="input" autocomplete="off" />
      </div>
      <div>
        <label class="label" for="staff-pass">初始密码</label>
        <input id="staff-pass" v-model="createForm.password" class="input" type="password" autocomplete="new-password" />
      </div>
    </div>
    <div class="mt-4 grid gap-4 md:grid-cols-2">
      <div>
        <p class="label mb-2">组织角色（必选）</p>
        <div class="max-h-48 space-y-1 overflow-y-auto text-sm">
          <label v-for="r in roles.data.value ?? []" :key="r.id" class="flex items-center gap-2">
            <input type="checkbox" :checked="createForm.role_ids.includes(r.id)" @change="toggleRole(r.id, 'create')" />
            {{ r.name }}
          </label>
        </div>
      </div>
      <div>
        <p class="label mb-2">部门（可选）</p>
        <div class="max-h-48 space-y-1 overflow-y-auto text-sm">
          <label v-for="o in units.data.value ?? []" :key="o.id" class="flex items-center gap-2">
            <input type="checkbox" :checked="createForm.org_unit_ids.includes(o.id)" @change="toggleOrg(o.id, 'create')" />
            {{ o.name }}
          </label>
        </div>
        <div class="mt-3">
          <label class="label" for="create-primary">主部门</label>
          <select id="create-primary" v-model="createForm.primary_org_id" class="input">
            <option value="">未指定</option>
            <option v-for="id in createForm.org_unit_ids" :key="id" :value="String(id)">
              {{ units.data.value?.find((o) => o.id === id)?.name || id }}
            </option>
          </select>
        </div>
      </div>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="showCreate = false">取消</button>
      <button class="btn-primary" :disabled="busy || !auth.isAdmin" @click="saveCreate">创建</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showConvert" title="将 C 端用户转为员工">
    <p class="mb-3 text-sm text-gray-500">搜索已有平台账号，挂上组织角色后即可进控制台（同一账号，不另开户）。</p>
    <div class="flex gap-2">
      <input v-model="addQ" class="input flex-1" placeholder="邮箱或 ID" @keyup.enter="searchConvert" />
      <button type="button" class="btn btn-secondary" @click="searchConvert">搜索</button>
    </div>
    <ul class="mt-3 max-h-64 overflow-y-auto text-sm">
      <li v-for="u in addHits" :key="u.id" class="flex items-center justify-between border-b border-black/5 py-2">
        <span>#{{ u.id }} {{ u.email }}</span>
        <button type="button" class="btn-ghost btn-sm" @click="pickConvert(u)">选择</button>
      </li>
    </ul>
    <template #footer>
      <button class="btn-secondary" @click="showConvert = false">关闭</button>
    </template>
  </ModalDialog>
</template>
