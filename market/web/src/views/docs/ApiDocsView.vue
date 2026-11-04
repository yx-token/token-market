<script setup lang="ts">
/**
 * API 文档：参考 aux-system 的 ApiDocsPage，按闲猫 Edge 实际放行的接口重写（internal/edge/proxy.go allowedPath）。
 * 公开页面，不需要登录；基础地址默认取平台 Edge 地址，可用 ?api_base= 覆盖。
 */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import ApiCodeBlock from '@/components/docs/ApiCodeBlock.vue'
import ApiEndpointCard from '@/components/docs/ApiEndpointCard.vue'
import { useConfig } from '@/stores/config'
import { useAuth } from '@/stores/auth'
import { API_DOC_ENDPOINTS, API_DOC_ENDPOINT_GROUPS, ENDPOINT_GROUP_SLUGS, QUICKSTART_CURL } from '@/utils/apiDocs'

const route = useRoute()
const cfg = useConfig()
const auth = useAuth()

function trimSlash(v: string): string {
  let s = v.trim()
  while (s.endsWith('/')) s = s.slice(0, -1)
  return s
}

const DEFAULT_MODEL = 'claude-sonnet-4-5'
const queryBase = typeof route.query.api_base === 'string' ? trimSlash(route.query.api_base) : ''
const baseInput = ref(queryBase)
const model = ref(DEFAULT_MODEL)
const expanded = reactive(new Set<string>(['chat-completions']))
const activeSection = ref('quickstart')

const defaultBase = computed(() => trimSlash(cfg.config.edge_url || window.location.origin))
const baseURL = computed(() => trimSlash(baseInput.value) || defaultBase.value)
const quickstart = computed(() =>
  QUICKSTART_CURL.replace('$API_BASE', baseURL.value).replace('$EXAMPLE_MODEL', model.value.trim() || DEFAULT_MODEL),
)
const quickstartResponse = API_DOC_ENDPOINTS.find((e) => e.id === 'chat-completions')?.response.trim() ?? '{}'
const groups = API_DOC_ENDPOINT_GROUPS.map((g) => ({
  name: g,
  slug: ENDPOINT_GROUP_SLUGS[g],
  endpoints: API_DOC_ENDPOINTS.filter((e) => e.group === g),
})).filter((g) => g.endpoints.length)

const capabilities = [
  { id: 'models', method: 'GET', title: '发现模型', desc: '读取当前 Key 能调用的模型', path: '/v1/models' },
  { id: 'chat-completions', method: 'POST', title: 'OpenAI 对话', desc: 'Chat Completions，支持 SSE 流式', path: '/v1/chat/completions' },
  { id: 'messages', method: 'POST', title: 'Claude 消息', desc: 'Anthropic Messages，Claude Code 同款协议', path: '/v1/messages' },
  { id: 'usage', method: 'GET', title: '查询余额', desc: '用 Key 读取剩余额度，不计费', path: '/v1/usage' },
]

const errorCodes = [
  { code: '401', type: 'authentication_error', text: 'Key 缺失、无效或已过期' },
  { code: '403', type: 'permission_error', text: 'Key 已停用，或请求 IP 不在白名单' },
  { code: '404', type: 'not_found_error', text: '路径不在网关支持范围内' },
  { code: '413', type: 'invalid_request_error', text: '请求体过大' },
  { code: '429', type: 'rate_limit_error', text: '触发 Key 的消费限额，或无效 Key 尝试过多' },
  { code: '503', type: 'overloaded_error', text: '当前路线暂无可用卖家，稍后重试或在 Key 里加备用跳' },
]

const errorSample = `{
  "error": {
    "type": "authentication_error",
    "message": "invalid api key"
  }
}`

function toggle(id: string) {
  if (expanded.has(id)) expanded.delete(id)
  else expanded.add(id)
}

function jump(id: string) {
  if (id.startsWith('endpoint-')) expanded.add(id.slice('endpoint-'.length))
  requestAnimationFrame(() => {
    const el = document.getElementById(id)
    if (!el) return
    const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth', block: 'start' })
    activeSection.value = id
  })
}

let observer: IntersectionObserver | null = null
const prevTitle = document.title

onMounted(async () => {
  document.title = 'API 文档 · 闲猫'
  await cfg.ensure()
  if (cfg.config.price_models?.length) {
    model.value = cfg.config.price_models.find((m) => m.startsWith('claude-sonnet')) ?? cfg.config.price_models[0]!
  }
  if (typeof IntersectionObserver === 'undefined') return
  const ids = ['quickstart', 'authentication', ...API_DOC_ENDPOINTS.map((e) => `endpoint-${e.id}`), 'errors']
  observer = new IntersectionObserver(
    (entries) => {
      const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)
      if (visible[0]) activeSection.value = visible[0].target.id
    },
    { rootMargin: '-80px 0px -62% 0px', threshold: [0, 0.2, 0.5] },
  )
  for (const id of ids) {
    const el = document.getElementById(id)
    if (el) observer.observe(el)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  document.title = prevTitle
})
</script>

<template>
  <div class="apidocs">
    <section class="apidocs-hero" aria-labelledby="apidocs-title">
      <div class="apidocs-hero__copy">
        <p class="apidocs-eyebrow"><span class="apidocs-dot" aria-hidden="true" />闲猫 API 参考</p>
        <h1 id="apidocs-title">
          <span>一个 Key，</span>
          <span class="apidocs-hero__accent">接上所有闲置算力。</span>
        </h1>
        <p class="apidocs-lede">
          闲猫 Edge 同时兼容 OpenAI、Anthropic 与 Gemini 协议。把 Base URL 换成闲猫地址、API Key 换成闲猫 Key，现有 SDK 和客户端就能直接用，按实际用量从钱包扣费。
        </p>
        <div class="apidocs-hero__actions">
          <button type="button" class="xm-btn xm-btn-primary" @click="jump('quickstart')">开始接入</button>
          <RouterLink class="xm-btn xm-btn-ghost" :to="auth.loggedIn ? '/buyer/client-import' : '/buyer/keys'">导入到客户端</RouterLink>
        </div>
      </div>
      <div class="apidocs-hero__panel" aria-label="请求预览">
        <div class="apidocs-panel__top">
          <span class="apidocs-panel__lights" aria-hidden="true"><i /><i /><i /></span>
          <small>request preview</small>
        </div>
        <div class="apidocs-panel__body">
          <p><span class="api-method api-method--post">POST</span> <code>/v1/messages</code></p>
          <p class="is-muted">x-api-key: <b>tm-••••••••</b></p>
          <p class="is-muted">anthropic-version: 2023-06-01</p>
          <p class="is-gap">{ <span>"model"</span>: <strong>"{{ model }}"</strong> }</p>
          <p class="apidocs-panel__ok"><span>200</span> 已路由到卖家通道 <b aria-hidden="true">●</b></p>
        </div>
      </div>
    </section>

    <section class="apidocs-index" aria-label="常用接口">
      <button v-for="c in capabilities" :key="c.id" type="button" class="apidocs-cap" @click="jump(`endpoint-${c.id}`)">
        <span class="api-method" :class="`api-method--${c.method.toLowerCase()}`">{{ c.method }}</span>
        <span class="apidocs-cap__copy">
          <strong>{{ c.title }}</strong>
          <small>{{ c.desc }}</small>
        </span>
        <code>{{ c.path }}</code>
      </button>
    </section>

    <div class="apidocs-layout">
      <aside class="apidocs-side" aria-label="文档目录">
        <div class="apidocs-side__label">本页目录</div>
        <a href="#quickstart" :class="{ on: activeSection === 'quickstart' }" @click.prevent="jump('quickstart')">快速开始</a>
        <a href="#authentication" :class="{ on: activeSection === 'authentication' }" @click.prevent="jump('authentication')">认证方式</a>
        <div v-for="g in groups" :key="g.slug" class="apidocs-side__group">
          <span>{{ g.name }}</span>
          <a
            v-for="e in g.endpoints"
            :key="e.id"
            :href="`#endpoint-${e.id}`"
            class="apidocs-side__ep"
            :class="{ on: activeSection === `endpoint-${e.id}` }"
            :aria-label="`${e.method} ${e.path}`"
            @click.prevent="jump(`endpoint-${e.id}`)"
          >
            <i :class="`is-${e.method.toLowerCase()}`">{{ e.method }}</i>
            <code :title="e.path">{{ e.path }}</code>
          </a>
        </div>
        <a href="#errors" :class="{ on: activeSection === 'errors' }" @click.prevent="jump('errors')">错误处理</a>
      </aside>

      <div class="apidocs-content">
        <section id="quickstart" class="apidocs-section">
          <h2>五分钟完成第一次请求</h2>
          <p class="apidocs-section__lead">准备一个闲猫 API Key（买家中心 → API Key 创建），把下面的命令粘到终端，换上你的 Key 即可。</p>
          <div class="apidocs-fields">
            <label class="apidocs-field">
              <span class="xm-field-label">API 基础地址</span>
              <input v-model="baseInput" class="xm-input" :placeholder="defaultBase" spellcheck="false" autocomplete="off" />
              <small class="xm-hint">默认是平台 Edge 地址，示例里的请求都会用它。</small>
            </label>
            <label class="apidocs-field">
              <span class="xm-field-label">示例模型</span>
              <input v-model="model" class="xm-input" list="apidocs-models" spellcheck="false" autocomplete="off" />
              <datalist id="apidocs-models">
                <option v-for="m in cfg.config.price_models" :key="m" :value="m" />
              </datalist>
              <small class="xm-hint">实际可用模型以你 Key 路线上卖家开放的为准，可先调 /v1/models。</small>
            </label>
          </div>
          <ApiCodeBlock :code="quickstart" language="shell" copied-text="快速开始命令已复制" aria-label="快速开始 curl 命令" />
          <div class="apidocs-sub">
            <div class="apidocs-sub__label">响应示例</div>
            <ApiCodeBlock :code="quickstartResponse" language="json" copied-text="响应示例已复制" aria-label="快速开始响应示例" />
          </div>
        </section>

        <section id="authentication" class="apidocs-section">
          <h2>用 API Key 保护每一次请求</h2>
          <p class="apidocs-section__lead">闲猫 Key 以 <code class="apidocs-inline">tm-</code> 开头，放在下面任一位置都可以。Key 只在服务端使用，不要写进前端代码或公开仓库。</p>
          <div class="apidocs-auth">
            <div>
              <span>OpenAI / 通用</span>
              <code>Authorization: Bearer $API_KEY</code>
            </div>
            <div>
              <span>Anthropic</span>
              <code>x-api-key: $API_KEY</code>
            </div>
            <div>
              <span>Gemini</span>
              <code>x-goog-api-key: $API_KEY</code>
            </div>
          </div>
          <ul class="apidocs-notes">
            <li>Gemini 原生路径也接受查询参数 <code class="apidocs-inline">?key=$API_KEY</code>，生产环境仍建议放 Header。</li>
            <li>Key 可在买家中心设置额度、IP 白名单和到期时间，超出后网关直接拒绝，不会扣费。</li>
            <li>只放行对话相关路径（模型列表、Chat Completions、Responses、Messages、Gemini、余额查询），其它路径返回 404。</li>
          </ul>
        </section>

        <section v-for="g in groups" :id="`group-${g.slug}`" :key="g.slug" class="apidocs-section apidocs-group">
          <div class="apidocs-group__head">
            <h2>{{ g.name }}</h2>
            <span>{{ g.endpoints.length }} 个接口</span>
          </div>
          <div class="apidocs-group__list">
            <ApiEndpointCard
              v-for="e in g.endpoints"
              :key="e.id"
              :endpoint="e"
              :base-url="baseURL"
              :model="model"
              :expanded="expanded.has(e.id)"
              @toggle="toggle(e.id)"
            />
          </div>
        </section>

        <section id="errors" class="apidocs-section">
          <h2>错误处理</h2>
          <p class="apidocs-section__lead">网关自身的错误统一返回下面的 JSON 结构；请求转给卖家上游后，错误按对应协议原样返回。先看 HTTP 状态码，再看 <code class="apidocs-inline">error.type</code>。</p>
          <ApiCodeBlock :code="errorSample" language="json" copied-text="错误示例已复制" aria-label="错误响应示例" />
          <div class="apidocs-errors">
            <div v-for="e in errorCodes" :key="e.code" class="apidocs-error">
              <b>{{ e.code }}</b>
              <div>
                <code>{{ e.type }}</code>
                <span>{{ e.text }}</span>
              </div>
            </div>
          </div>
        </section>

        <section class="apidocs-cta">
          <div>
            <strong>不想手动改配置？</strong>
            <p>在「导入客户端」里选一个 Key，一键导入 CC Switch、Cherry Studio、Chatbox，或复制命令写入 Claude Code、Codex 等本地配置。</p>
          </div>
          <RouterLink class="xm-btn xm-btn-dark" :to="auth.loggedIn ? '/buyer/client-import' : '/buyer/keys'">去导入客户端</RouterLink>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.apidocs {
  --ad-ink: #1f1f1f;
  --ad-muted: #6f6f6f;
  --ad-line: rgba(0, 0, 0, 0.08);
  --ad-yellow: #ffd400;
  --ad-yellow-soft: #fff3a8;
  --ad-cream: #fffced;
  --ad-mono: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  width: 100%;
  max-width: 1280px;
  margin: 0 auto;
  padding: 24px clamp(14px, 3vw, 40px) 64px;
  color: var(--ad-ink);
}
/* 全局 base 给所有 code 加了米色底，这里统一清掉，按各处需要再设 */
:where(.apidocs) code {
  border-radius: 0;
  background: transparent;
  padding: 0;
  font-size: inherit;
  color: inherit;
}

/* ---- Hero ---- */
.apidocs-hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr);
  gap: clamp(20px, 4vw, 56px);
  align-items: center;
  overflow: hidden;
  border: 1px solid rgba(240, 194, 0, 0.35);
  border-radius: 24px;
  padding: clamp(24px, 4vw, 52px);
  background:
    radial-gradient(120% 90% at 100% 0%, rgba(255, 230, 15, 0.35) 0%, rgba(255, 230, 15, 0) 55%),
    linear-gradient(180deg, #fffdf2 0%, var(--ad-cream) 100%);
}
.apidocs-hero::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background-image: radial-gradient(rgba(0, 0, 0, 0.06) 1px, transparent 1px);
  background-size: 18px 18px;
  mask-image: linear-gradient(90deg, transparent 0%, #000 70%);
  -webkit-mask-image: linear-gradient(90deg, transparent 0%, #000 70%);
}
.apidocs-hero__copy,
.apidocs-hero__panel { position: relative; z-index: 1; }
.apidocs-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 14px;
  border-radius: 999px;
  background: #fff;
  border: 1px solid var(--ad-line);
  padding: 5px 12px 5px 10px;
  font-size: 12px;
  font-weight: 800;
  color: #5b4a00;
}
.apidocs-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #34b36a;
  box-shadow: 0 0 0 3px rgba(52, 179, 106, 0.18);
}
.apidocs-hero h1 {
  margin: 0;
  font-family: var(--display, inherit);
  font-size: clamp(30px, 4.4vw, 50px);
  font-weight: 800;
  line-height: 1.12;
  letter-spacing: -0.03em;
}
.apidocs-hero h1 span { display: block; }
.apidocs-hero__accent {
  width: fit-content;
  background: linear-gradient(transparent 62%, var(--ad-yellow) 62%, var(--ad-yellow) 92%, transparent 92%);
}
.apidocs-lede {
  max-width: 560px;
  margin: 16px 0 0;
  font-size: 15px;
  line-height: 1.75;
  color: #4a4a4a;
}
.apidocs-hero__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 24px;
}
.apidocs-hero__panel {
  border-radius: 18px;
  background: #1c1b18;
  box-shadow: 0 24px 48px rgba(60, 45, 0, 0.18);
  overflow: hidden;
  transform: rotate(0.6deg);
}
.apidocs-panel__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.07);
  font-family: var(--ad-mono);
  font-size: 11px;
  color: #9c968a;
}
.apidocs-panel__lights { display: inline-flex; gap: 6px; }
.apidocs-panel__lights i {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #4a463e;
}
.apidocs-panel__lights i:first-child { background: var(--ad-yellow); }
.apidocs-panel__body {
  padding: 16px 18px 18px;
  font-family: var(--ad-mono);
  font-size: 13px;
  line-height: 1.9;
  color: #ece7db;
}
.apidocs-panel__body p { margin: 0; overflow-wrap: anywhere; }
.apidocs-panel__body .is-muted { color: #a39d90; }
.apidocs-panel__body .is-muted b { color: #ffd84d; font-weight: 600; }
.apidocs-panel__body .is-gap { margin-top: 10px; }
.apidocs-panel__body .is-gap span { color: #a9c7ff; }
.apidocs-panel__body strong { color: #ffd84d; font-weight: 600; }
.apidocs-panel__body code { color: #fff; }
.apidocs-panel__ok {
  margin-top: 12px !important;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #b9e8cc;
}
.apidocs-panel__ok span {
  border-radius: 6px;
  background: rgba(52, 179, 106, 0.2);
  padding: 0 7px;
  font-weight: 700;
  color: #7fe0a8;
}
.apidocs-panel__ok b { color: #34b36a; font-size: 10px; }

/* ---- 能力索引 ---- */
.apidocs-index {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}
.apidocs-cap {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 8px 10px;
  align-items: center;
  border: 1px solid var(--ad-line);
  border-radius: 16px;
  background: #fff;
  padding: 14px;
  font: inherit;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s, transform 0.15s, box-shadow 0.15s;
}
.apidocs-cap:hover {
  border-color: var(--ad-ink);
  transform: translateY(-1px);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.06);
}
.apidocs-cap:focus-visible { outline: 2px solid var(--ad-ink); outline-offset: 2px; }
.apidocs-cap__copy strong { display: block; font-size: 14px; font-weight: 800; }
.apidocs-cap__copy small { display: block; margin-top: 2px; font-size: 12px; color: var(--ad-muted); line-height: 1.45; }
.apidocs-cap code {
  grid-column: 1 / -1;
  font-family: var(--ad-mono);
  font-size: 12px;
  color: #8a6a00;
}

/* ---- 两栏布局 ---- */
.apidocs-layout {
  display: grid;
  grid-template-columns: 228px minmax(0, 1fr);
  gap: 28px;
  margin-top: 28px;
  align-items: start;
}
.apidocs-side {
  position: sticky;
  top: calc(var(--mh-bar-h, 53px) + 16px);
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: calc(100vh - var(--mh-bar-h, 53px) - 32px);
  overflow-y: auto;
  border: 1px solid var(--ad-line);
  border-radius: 18px;
  background: #fff;
  padding: 14px 10px;
}
.apidocs-side__label {
  padding: 0 10px 8px;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
  color: #999;
}
.apidocs-side a {
  display: flex;
  align-items: center;
  gap: 8px;
  border-radius: 10px;
  padding: 8px 10px;
  font-size: 13px;
  font-weight: 700;
  color: #555;
  text-decoration: none;
  transition: background 0.12s, color 0.12s;
}
.apidocs-side a:hover { background: rgba(0, 0, 0, 0.04); color: var(--ad-ink); }
.apidocs-side a.on { background: var(--ad-yellow-soft); color: var(--ad-ink); }
.apidocs-side a:focus-visible { outline: 2px solid var(--ad-ink); outline-offset: -2px; }
.apidocs-side__group {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 8px 0 4px;
}
.apidocs-side__group > span {
  padding: 4px 10px;
  font-size: 11px;
  font-weight: 800;
  color: #a08300;
}
.apidocs-side__ep i {
  flex-shrink: 0;
  width: 34px;
  font-family: var(--ad-mono);
  font-size: 10px;
  font-style: normal;
  font-weight: 700;
}
.apidocs-side__ep i.is-get { color: #1a7a45; }
.apidocs-side__ep i.is-post { color: #8a6a00; }
.apidocs-side__ep code {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--ad-mono);
  font-size: 12px;
  font-weight: 500;
}
.apidocs-content {
  display: flex;
  flex-direction: column;
  gap: 18px;
  min-width: 0;
}
.apidocs-section {
  scroll-margin-top: calc(var(--mh-bar-h, 53px) + 16px);
  border: 1px solid var(--ad-line);
  border-radius: 20px;
  background: #fff;
  padding: clamp(18px, 2.4vw, 28px);
}
.apidocs-section h2 {
  margin: 0;
  font-size: 21px;
  font-weight: 800;
  letter-spacing: -0.02em;
}
.apidocs-section__lead {
  margin: 8px 0 18px;
  max-width: 760px;
  font-size: 14px;
  line-height: 1.7;
  color: var(--ad-muted);
}
.apidocs-inline {
  border-radius: 5px;
  background: var(--ad-yellow-soft);
  padding: 1px 5px;
  font-family: var(--ad-mono);
  font-size: 0.88em;
  color: #3a2f00;
}
.apidocs-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}
.apidocs-field { display: block; }
.apidocs-sub { margin-top: 16px; }
.apidocs-sub__label {
  margin-bottom: 8px;
  font-size: 12px;
  font-weight: 700;
  color: #888;
}
.apidocs-auth {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.apidocs-auth > div {
  border-radius: 14px;
  background: #fafafa;
  border: 1px solid var(--ad-line);
  padding: 12px 14px;
}
.apidocs-auth span {
  display: block;
  margin-bottom: 6px;
  font-size: 11px;
  font-weight: 800;
  color: #999;
}
.apidocs-auth code {
  font-family: var(--ad-mono);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ad-ink);
  overflow-wrap: anywhere;
}
.apidocs-notes {
  margin: 16px 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 8px;
}
.apidocs-notes li {
  position: relative;
  padding-left: 18px;
  font-size: 13px;
  line-height: 1.65;
  color: #555;
}
.apidocs-notes li::before {
  content: '';
  position: absolute;
  left: 4px;
  top: 0.62em;
  width: 6px;
  height: 6px;
  border-radius: 2px;
  background: var(--ad-yellow);
}
.apidocs-group { background: #fcfcfb; }
.apidocs-group__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.apidocs-group__head span {
  font-size: 12px;
  font-weight: 700;
  color: #999;
}
.apidocs-group__list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.apidocs-errors {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 16px;
}
.apidocs-error {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  border-radius: 14px;
  border: 1px solid var(--ad-line);
  padding: 12px 14px;
}
.apidocs-error b {
  flex-shrink: 0;
  border-radius: 8px;
  background: var(--ad-ink);
  padding: 2px 8px;
  font-family: var(--ad-mono);
  font-size: 13px;
  color: var(--ad-yellow);
}
.apidocs-error code {
  display: block;
  font-family: var(--ad-mono);
  font-size: 12px;
  color: #8a6a00;
}
.apidocs-error span {
  display: block;
  margin-top: 2px;
  font-size: 13px;
  color: #555;
  line-height: 1.5;
}
.apidocs-cta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-radius: 20px;
  background: var(--ad-yellow);
  padding: 20px 24px;
}
.apidocs-cta strong { font-size: 17px; font-weight: 800; }
.apidocs-cta p {
  margin: 4px 0 0;
  max-width: 620px;
  font-size: 13px;
  line-height: 1.6;
  color: #3a3000;
}

@media (max-width: 1080px) {
  .apidocs-index { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 1024px) {
  .apidocs-layout { grid-template-columns: minmax(0, 1fr); }
  .apidocs-side { display: none; }
}
@media (max-width: 760px) {
  .apidocs { padding-top: 14px; }
  .apidocs-hero { grid-template-columns: minmax(0, 1fr); border-radius: 18px; }
  .apidocs-hero__panel { transform: none; }
  .apidocs-fields,
  .apidocs-auth,
  .apidocs-errors { grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 520px) {
  .apidocs-index { grid-template-columns: minmax(0, 1fr); }
}
@media (prefers-reduced-motion: reduce) {
  .apidocs-cap { transition: none; }
  .apidocs-cap:hover { transform: none; }
}
</style>
