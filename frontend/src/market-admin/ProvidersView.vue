<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface DetectionProvider {
  name: string
  label: string
  description: string
  docs_url: string
  default_base_url: string
  options: Record<string, string>
  runs_per_hour_limit: number
  requests_per_run: number
  enabled: boolean
  base_url: string
  has_api_key: boolean
  settings: Record<string, unknown>
  max_runs_per_hour: number
  timeout_minutes: number
  runs_last_hour: number
}

interface ThirdPartyCheck {
  id: number
  upstream_id: number
  provider: string
  model: string
  status: string
  outcome?: string
  verdict?: string
  error?: string
  created_at?: string
}

type FormState = {
  enabled: boolean
  baseURL: string
  rate: number
  timeout: number
  apiKey: string
  settings: Record<string, unknown>
}

const BOOL_OPTIONS = new Set(['run_context_check', 'bias_fingerprint', 'effort_verification'])

const tab = ref<'checks' | 'config'>('config')
const providers = ref<DetectionProvider[]>([])
const checks = ref<ThirdPartyCheck[]>([])
const forms = reactive<Record<string, FormState>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterProvider = ref('')
const filterStatus = ref('')

const filteredChecks = computed(() => {
  let rows = checks.value
  if (filterProvider.value) rows = rows.filter((c) => c.provider === filterProvider.value)
  if (filterStatus.value) rows = rows.filter((c) => c.status === filterStatus.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((c) =>
      [c.id, c.upstream_id, c.provider, c.model, c.outcome, c.verdict]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return rows
})

function triState(f: Record<string, unknown>, key: string): string {
  return f[key] === true ? 'on' : f[key] === false ? 'off' : ''
}

function setTriState(f: Record<string, unknown>, key: string, v: string) {
  if (v === '') delete f[key]
  else f[key] = v === 'on'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [ps, cs] = await Promise.all([
      marketGet<DetectionProvider[] | null>(`${MARKET_API_PREFIX}/detection-providers`),
      marketGet<ThirdPartyCheck[] | null>(`${MARKET_API_PREFIX}/third-party-checks?limit=100`).catch(() => []),
    ])
    providers.value = ps ?? []
    for (const p of providers.value) {
      forms[p.name] = {
        enabled: p.enabled,
        baseURL: p.base_url,
        rate: p.max_runs_per_hour,
        timeout: p.timeout_minutes,
        apiKey: '',
        settings: { ...(p.settings || {}) },
      }
    }
    checks.value = cs ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function save(p: DetectionProvider) {
  const f = forms[p.name]
  if (!f) return
  const settings: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(f.settings)) {
    if (v === '' || v === null || v === undefined) continue
    settings[k] = v
  }
  const body: Record<string, unknown> = {
    enabled: f.enabled,
    base_url: f.baseURL,
    max_runs_per_hour: Number(f.rate),
    timeout_minutes: Number(f.timeout),
    settings,
  }
  if (f.apiKey) body.api_key = f.apiKey
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/detection-providers/${p.name}`, body)
    msg.value = `${p.label} 已保存`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-providers-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">{{ tab === 'config' ? '检测站配置' : '最近检测' }}</h2>
        <p class="y-page-desc">
          {{
            tab === 'config'
              ? '启用第三方检测站、配置接口与提交频率'
              : '第三方检测站最近提交的模型身份检测记录'
          }}
        </p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'config' }" @click="tab = 'config'">检测站配置</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'checks' }" @click="tab = 'checks'">最近检测</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'config'">
        <div v-for="p in providers" :key="p.name" class="provider-card">
          <div class="provider-head">
            <h3>{{ p.label }}</h3>
            <span :class="p.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ p.enabled ? '已启用' : '未启用' }}</span>
            <a v-if="p.docs_url" :href="p.docs_url" target="_blank" rel="noopener noreferrer" class="y-link">文档</a>
            <span class="provider-meta">最近 1 小时提交 {{ p.runs_last_hour }} 次</span>
          </div>
          <p class="provider-desc">{{ p.description }}</p>
          <p class="provider-hint">
            每次约 {{ p.requests_per_run }} 个请求；检测站限制同出口 IP 每小时 {{ p.runs_per_hour_limit }} 次。
          </p>
          <div v-if="forms[p.name]" class="provider-form">
            <label class="y-check"><input v-model="forms[p.name].enabled" type="checkbox" /> 启用</label>
            <label class="y-field y-field--wide">
              <span>接口地址</span>
              <input v-model="forms[p.name].baseURL" class="input w-full" :placeholder="p.default_base_url" />
            </label>
            <label class="y-field">
              <span>每小时最多提交</span>
              <input v-model.number="forms[p.name].rate" class="input w-full" type="number" min="1" />
            </label>
            <label class="y-field">
              <span>等待结果上限（分钟）</span>
              <input v-model.number="forms[p.name].timeout" class="input w-full" type="number" min="5" />
            </label>
            <label v-for="(desc, key) in p.options" :key="key" class="y-field">
              <span>{{ desc }}</span>
              <select
                v-if="BOOL_OPTIONS.has(String(key))"
                class="input w-full"
                :value="triState(forms[p.name].settings, String(key))"
                @change="setTriState(forms[p.name].settings, String(key), ($event.target as HTMLSelectElement).value)"
              >
                <option value="">检测站默认</option>
                <option value="on">开启</option>
                <option value="off">关闭</option>
              </select>
              <input v-else v-model="forms[p.name].settings[key]" class="input w-full" />
            </label>
            <label class="y-field">
              <span>API Key（可选）</span>
              <input
                v-model="forms[p.name].apiKey"
                class="input w-full"
                type="password"
                :placeholder="p.has_api_key ? '已设置，留空不修改' : '无需鉴权时留空'"
              />
            </label>
            <label class="y-check y-field--wide">
              <input v-model="forms[p.name].settings.allow_insecure_target" type="checkbox" />
              允许把 http / 内网地址交给检测站（仅开发）
            </label>
            <div class="provider-actions">
              <button type="button" class="y-btn" :disabled="busy" @click="save(p)">保存</button>
            </div>
          </div>
        </div>
        <p v-if="!providers.length" class="y-msg">暂无检测站</p>
      </template>

      <template v-else>
        <div class="y-search">
          <input v-model="keyword" type="search" placeholder="上游 / 模型 / 判定" />
          <select v-model="filterProvider" class="input">
            <option value="">检测站：全部</option>
            <option v-for="p in providers" :key="p.name" :value="p.name">{{ p.label }}</option>
          </select>
          <select v-model="filterStatus" class="input">
            <option value="">状态：全部</option>
            <option value="pending">pending</option>
            <option value="running">running</option>
            <option value="done">done</option>
            <option value="failed">failed</option>
          </select>
        </div>
        <p v-if="!filteredChecks.length" class="y-msg">暂无检测记录</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>上游</th>
              <th>检测站</th>
              <th>模型</th>
              <th>状态</th>
              <th>判定</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in filteredChecks" :key="c.id">
              <td>{{ c.id }}</td>
              <td>#{{ c.upstream_id }}</td>
              <td>{{ c.provider }}</td>
              <td>{{ c.model || '—' }}</td>
              <td><span class="y-chip">{{ c.status }}</span></td>
              <td>{{ c.verdict || c.outcome || c.error || '—' }}</td>
              <td>{{ c.created_at || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ filteredChecks.length }} 条</footer>
      </template>
    </div>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  gap: 8px;
}
.provider-card {
  margin-bottom: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid rgb(0 0 0 / 6%);
}
.provider-card:last-child {
  border-bottom: 0;
}
.provider-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.provider-head h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.provider-meta {
  margin-left: auto;
  font-size: 12px;
  opacity: 0.65;
}
.provider-desc,
.provider-hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--color-text-secondary, #868686);
}
.provider-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 12px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.y-field--wide {
  grid-column: 1 / -1;
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.provider-actions {
  grid-column: 1 / -1;
  display: flex;
  justify-content: flex-end;
}
</style>
