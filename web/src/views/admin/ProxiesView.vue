<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Proxy } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime } from '@/utils/format'

const proxies = useLoader(() => admin.proxies(), [] as Proxy[] | null)
const { busy, run } = useAction()
const show = ref(false)
const form = reactive({ name: '', protocol: 'socks5', host: '', port: '', username: '', password: '', region: '' })
onMounted(proxies.load)

async function create() {
  const body = { ...form, port: Number(form.port) }
  if (await run(() => admin.createProxy(body), '代理已添加')) {
    show.value = false
    Object.assign(form, { name: '', host: '', port: '', username: '', password: '', region: '' })
    proxies.load()
  }
}
async function act(fn: () => Promise<unknown>, msg: string) {
  if (await run(fn, msg)) proxies.load()
}
</script>

<template>
  <div>
    <PageHeader title="代理" description="每个上游独占一个代理（固定出口 IP），创建上游时自动分配空闲代理，删除上游后释放。">
      <button class="btn-primary" @click="show = true">添加代理</button>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: 'ID' },
          { key: 'name', label: '名称' },
          { key: 'addr', label: '地址' },
          { key: 'region', label: '地区' },
          { key: 'status', label: '状态' },
          { key: 'assigned_upstream_id', label: '分配给' },
          { key: 'sync', label: 'sub2api' },
          { key: 'created_at', label: '添加时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="proxies.data.value"
        :loading="proxies.loading.value"
        row-key="id"
      >
        <template #cell-addr="{ row }">{{ row.protocol }}://{{ row.host }}:{{ row.port }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="proxyStatus" :value="row.status" /></template>
        <template #cell-assigned_upstream_id="{ row }">{{ row.assigned_upstream_id ? `上游 #${row.assigned_upstream_id}` : '—' }}</template>
        <template #cell-sync="{ row }">{{ row.sub2api_proxy_id ? `#${row.sub2api_proxy_id}` : '未同步' }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="!row.sub2api_proxy_id" class="btn-ghost btn-sm" :disabled="busy" @click="act(() => admin.syncProxy(row.id), '已同步')">同步</button>
          <button v-if="row.status !== 'disabled'" class="btn-ghost btn-sm" :disabled="busy" @click="act(() => admin.setProxyDisabled(row.id, true), '已停用')">停用</button>
          <button v-else class="btn-ghost btn-sm" :disabled="busy" @click="act(() => admin.setProxyDisabled(row.id, false), '已启用')">启用</button>
        </template>
      </DataTable>
    </div>
    <ModalDialog v-model:open="show" title="添加代理">
      <div class="grid grid-cols-2 gap-3">
        <div class="col-span-2">
          <label class="label" for="pn">名称</label>
          <input id="pn" v-model="form.name" class="input" />
        </div>
        <div>
          <label class="label" for="pp">协议</label>
          <select id="pp" v-model="form.protocol" class="input">
            <option v-for="p in ['socks5', 'socks5h', 'http', 'https']" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
        <div>
          <label class="label" for="pr">地区</label>
          <input id="pr" v-model="form.region" class="input" placeholder="us / jp …" />
        </div>
        <div>
          <label class="label" for="ph">主机</label>
          <input id="ph" v-model="form.host" class="input" />
        </div>
        <div>
          <label class="label" for="pport">端口</label>
          <input id="pport" v-model="form.port" class="input" type="number" />
        </div>
        <div>
          <label class="label" for="pu">用户名</label>
          <input id="pu" v-model="form.username" class="input" autocomplete="off" />
        </div>
        <div>
          <label class="label" for="ppw">密码</label>
          <input id="ppw" v-model="form.password" class="input" type="password" autocomplete="new-password" />
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="show = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="create">添加</button>
      </template>
    </ModalDialog>
  </div>
</template>
