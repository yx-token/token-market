/**
 * API 文档页的数据与纯函数。
 *
 * 从 aux-system 的 React 版 ApiDocsPage.tsx 移植，去掉了所有框架相关内容（React 节点、
 * 路由、主题、GSAP 动画），只留下数据契约与可复用的纯函数，Vue 页面直接引用。
 *
 * 接口清单按 token-market 网关当前实际放行的路径维护，见 market/internal/edge/proxy.go
 * 的 allowedPath；余额字段见 market/internal/edge/usage.go 的 usageResponse。
 */

export type ApiDocEndpointGroup = 'OpenAI 兼容' | 'Anthropic 兼容' | 'Google 原生' | '账户'
export type ExampleLanguage = 'curl' | 'python' | 'go' | 'java'

export interface ApiDocParameter {
  name: string
  type: string
  required: boolean
  defaultValue: string
  description: string
}

export interface ApiDocEndpoint {
  id: string
  group: ApiDocEndpointGroup
  method: 'GET' | 'POST'
  path: string
  /** 该端点的示例模型；缺省时使用页面传入的模型。 */
  exampleModel?: string
  title: string
  description: string
  auth: string
  request?: string
  response: string
  requestParams: ApiDocParameter[]
  responseParams: ApiDocParameter[]
}

const DEFAULT_EXAMPLE_MODEL = 'gpt-5.5'

const API_KEY_AUTH = '需要 API Key（买家中心 → API Key 创建）'

export const API_DOC_ENDPOINT_GROUPS: ApiDocEndpointGroup[] = [
  'OpenAI 兼容',
  'Anthropic 兼容',
  'Google 原生',
  '账户',
]

export const ENDPOINT_GROUP_SLUGS: Record<ApiDocEndpointGroup, string> = {
  'OpenAI 兼容': 'openai',
  'Anthropic 兼容': 'anthropic',
  'Google 原生': 'google',
  '账户': 'account',
}

export const API_DOC_ENDPOINTS: ApiDocEndpoint[] = [
  {
    id: 'models',
    group: 'OpenAI 兼容',
    method: 'GET',
    path: '/v1/models',
    title: '列出可用模型',
    description: '返回当前 API Key 可以使用的模型列表，可用于启动时探测模型能力。本接口不产生费用。',
    auth: API_KEY_AUTH,
    requestParams: [],
    response: `{
  "object": "list",
  "data": [{
    "id": "gpt-5.5",
    "object": "model",
    "owned_by": "provider"
  }]
}`,
    responseParams: [
      { name: 'object', type: 'string', required: true, defaultValue: '"list"', description: '响应对象类型，固定为 list。' },
      { name: 'data', type: 'array', required: true, defaultValue: '[]', description: '模型对象数组。' },
      { name: 'data[].id', type: 'string', required: true, defaultValue: '-', description: '可用于其他接口 model 字段的模型标识。' },
      { name: 'data[].object', type: 'string', required: true, defaultValue: '"model"', description: '模型对象类型，固定为 model。' },
      { name: 'data[].owned_by', type: 'string', required: false, defaultValue: '"provider"', description: '模型归属或上游提供方。' },
    ],
  },
  {
    id: 'chat-completions',
    group: 'OpenAI 兼容',
    method: 'POST',
    path: '/v1/chat/completions',
    exampleModel: 'gpt-5.5',
    title: 'Chat Completions',
    description: 'OpenAI 兼容的对话接口，支持普通响应和 `stream: true` 的 SSE 流式响应。',
    auth: API_KEY_AUTH,
    request: `{
  "model": "gpt-5.5",
  "messages": [
    { "role": "user", "content": "用一句话介绍这个接口" }
  ],
  "temperature": 0.7,
  "stream": false
}`,
    response: `{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "choices": [{
    "message": { "role": "assistant", "content": "..." },
    "finish_reason": "stop"
  }]
}`,
    requestParams: [
      { name: 'model', type: 'string', required: true, defaultValue: '-', description: '要调用的模型 ID，来自 /v1/models。' },
      { name: 'messages', type: 'array', required: true, defaultValue: '[]', description: '按时间顺序排列的对话消息。' },
      { name: 'messages[].role', type: 'string', required: true, defaultValue: '-', description: '消息角色，通常为 system、user 或 assistant。' },
      { name: 'messages[].content', type: 'string | array', required: true, defaultValue: '-', description: '文本内容，也可传入内容块数组。' },
      { name: 'temperature', type: 'number', required: false, defaultValue: '服务端默认', description: '采样随机性，范围通常为 0 到 2。' },
      { name: 'stream', type: 'boolean', required: false, defaultValue: 'false', description: '是否以 SSE 流式返回增量内容。' },
    ],
    responseParams: [
      { name: 'id', type: 'string', required: true, defaultValue: '-', description: '本次生成请求的唯一 ID。' },
      { name: 'object', type: 'string', required: true, defaultValue: '"chat.completion"', description: '响应对象类型。' },
      { name: 'choices', type: 'array', required: true, defaultValue: '[]', description: '候选回复数组。' },
      { name: 'choices[].message', type: 'object', required: true, defaultValue: '-', description: '模型生成的消息对象。' },
      { name: 'choices[].finish_reason', type: 'string | null', required: false, defaultValue: 'null', description: '结束原因，例如 stop、length 或 content_filter。' },
      { name: 'usage', type: 'object', required: false, defaultValue: 'null', description: 'token 用量，服务端可能不返回。' },
    ],
  },
  {
    id: 'responses',
    group: 'OpenAI 兼容',
    method: 'POST',
    path: '/v1/responses',
    exampleModel: 'gpt-5.5',
    title: 'Responses',
    description: '面向新客户端的统一响应接口，适合文本、工具调用和多轮响应工作流。',
    auth: API_KEY_AUTH,
    request: `{
  "model": "gpt-5.5",
  "input": "Explain this API in one sentence",
  "max_output_tokens": 128
}`,
    response: `{
  "id": "resp_...",
  "object": "response",
  "status": "completed",
  "output": [{ "type": "message", "role": "assistant" }]
}`,
    requestParams: [
      { name: 'model', type: 'string', required: true, defaultValue: '-', description: '要调用的模型 ID。' },
      { name: 'input', type: 'string | array', required: true, defaultValue: '-', description: '用户输入，可以是文本或结构化输入项数组。' },
      { name: 'max_output_tokens', type: 'integer', required: false, defaultValue: '服务端默认', description: '限制模型最多生成的 token 数。' },
      { name: 'stream', type: 'boolean', required: false, defaultValue: 'false', description: '是否以事件流返回增量事件。' },
      { name: 'tools', type: 'array', required: false, defaultValue: '[]', description: '可供模型选择的工具定义。' },
    ],
    responseParams: [
      { name: 'id', type: 'string', required: true, defaultValue: '-', description: '响应对象唯一 ID。' },
      { name: 'object', type: 'string', required: true, defaultValue: '"response"', description: '响应对象类型。' },
      { name: 'status', type: 'string', required: true, defaultValue: '"completed"', description: '响应状态。' },
      { name: 'output', type: 'array', required: true, defaultValue: '[]', description: '模型输出项，通常包含 message、tool_call 等类型。' },
      { name: 'usage', type: 'object', required: false, defaultValue: 'null', description: 'token 用量信息。' },
    ],
  },
  {
    id: 'messages',
    group: 'Anthropic 兼容',
    method: 'POST',
    path: '/v1/messages',
    exampleModel: 'claude-sonnet-4-5',
    title: 'Messages',
    description: 'Anthropic Messages 兼容接口。把现有 Anthropic SDK 的 base URL 指向 API 服务即可复用。',
    auth: API_KEY_AUTH,
    request: `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 512,
  "messages": [{ "role": "user", "content": "Hello" }]
}`,
    response: `{
  "id": "msg_...",
  "type": "message",
  "role": "assistant",
  "content": [{ "type": "text", "text": "Hello!" }],
  "stop_reason": "end_turn"
}`,
    requestParams: [
      { name: 'model', type: 'string', required: true, defaultValue: '-', description: 'Claude 模型 ID。' },
      { name: 'max_tokens', type: 'integer', required: true, defaultValue: '-', description: '本次请求允许生成的最大 token 数。' },
      { name: 'messages', type: 'array', required: true, defaultValue: '[]', description: 'Anthropic 消息数组。' },
      { name: 'system', type: 'string | array', required: false, defaultValue: 'null', description: '系统提示词。' },
      { name: 'temperature', type: 'number', required: false, defaultValue: '服务端默认', description: '采样随机性。' },
      { name: 'stream', type: 'boolean', required: false, defaultValue: 'false', description: '是否返回 Anthropic SSE 事件流。' },
    ],
    responseParams: [
      { name: 'id', type: 'string', required: true, defaultValue: '-', description: '消息响应唯一 ID。' },
      { name: 'type', type: 'string', required: true, defaultValue: '"message"', description: '响应类型。' },
      { name: 'role', type: 'string', required: true, defaultValue: '"assistant"', description: '响应角色。' },
      { name: 'content', type: 'array', required: true, defaultValue: '[]', description: '模型生成的内容块数组。' },
      { name: 'stop_reason', type: 'string | null', required: false, defaultValue: 'null', description: '生成结束原因。' },
      { name: 'usage', type: 'object', required: false, defaultValue: 'null', description: '输入与输出 token 用量。' },
    ],
  },
  {
    id: 'messages-count-tokens',
    group: 'Anthropic 兼容',
    method: 'POST',
    path: '/v1/messages/count_tokens',
    exampleModel: 'claude-sonnet-4-5',
    title: '统计输入 Token',
    description: 'Anthropic 兼容的 token 计数接口。请求体与 `/v1/messages` 相同，但不需要 `max_tokens`；只返回输入 token 数，不产生费用。',
    auth: API_KEY_AUTH,
    request: `{
  "model": "claude-sonnet-4-5",
  "messages": [{ "role": "user", "content": "Hello" }]
}`,
    response: `{
  "input_tokens": 123
}`,
    requestParams: [
      { name: 'model', type: 'string', required: true, defaultValue: '-', description: 'Claude 模型 ID。' },
      { name: 'messages', type: 'array', required: true, defaultValue: '[]', description: '与 /v1/messages 相同格式的消息数组。' },
      { name: 'messages[].role', type: 'string', required: true, defaultValue: '-', description: '消息角色，通常为 user 或 assistant。' },
      { name: 'messages[].content', type: 'string | array', required: true, defaultValue: '-', description: '消息内容，文本或内容块数组。' },
      { name: 'system', type: 'string | array', required: false, defaultValue: 'null', description: '系统提示词，会计入输入 token。' },
      { name: 'tools', type: 'array', required: false, defaultValue: '[]', description: '可选工具定义，会计入输入 token。' },
    ],
    responseParams: [
      { name: 'input_tokens', type: 'integer', required: true, defaultValue: '-', description: '本次请求内容的输入 token 总数。' },
    ],
  },
  {
    id: 'gemini-generate-content',
    group: 'Google 原生',
    method: 'POST',
    path: '/v1beta/models/{model}:generateContent',
    exampleModel: 'gemini-2.5-pro',
    title: 'Gemini Generate Content',
    description: '保留 Gemini 原生请求格式。流式请求将动作名替换为 `streamGenerateContent`，并追加 `?alt=sse`。',
    auth: 'x-goog-api-key（Key 在买家中心 → API Key 创建）',
    request: `{
  "contents": [{
    "role": "user",
    "parts": [{ "text": "Hello from the API" }]
  }]
}`,
    response: `{
  "candidates": [{
    "content": { "role": "model", "parts": [{ "text": "Hello!" }] },
    "finishReason": "STOP"
  }]
}`,
    requestParams: [
      { name: 'model', type: 'path string', required: true, defaultValue: '-', description: 'Gemini 模型名称，替换路径中的 {model}。' },
      { name: 'contents', type: 'array', required: true, defaultValue: '[]', description: 'Gemini 原生内容数组，包含 role 和 parts。' },
      { name: 'contents[].parts', type: 'array', required: true, defaultValue: '[]', description: '文本等内容块数组。' },
      { name: 'generationConfig', type: 'object', required: false, defaultValue: '{}', description: '温度、最大输出 token 等生成参数。' },
      { name: 'safetySettings', type: 'array', required: false, defaultValue: '[]', description: '安全阈值配置。' },
    ],
    responseParams: [
      { name: 'candidates', type: 'array', required: true, defaultValue: '[]', description: '模型候选回复数组。' },
      { name: 'candidates[].content', type: 'object', required: true, defaultValue: '-', description: '包含 model 角色和 parts 的内容对象。' },
      { name: 'candidates[].finishReason', type: 'string', required: false, defaultValue: 'null', description: '生成结束原因，例如 STOP。' },
      { name: 'usageMetadata', type: 'object', required: false, defaultValue: 'null', description: '提示词和候选内容的 token 统计。' },
    ],
  },
  {
    id: 'usage',
    group: '账户',
    method: 'GET',
    path: '/v1/usage',
    title: '查询余额与额度',
    description: '用当前 API Key 查询钱包余额、额度包余额与各限流窗口的已用金额。这是本地账本快照，不转发上游，也不产生费用。',
    auth: API_KEY_AUTH,
    response: `{
  "remaining": 86.42,
  "balance": 86.42,
  "quota_remaining": 86.42,
  "wallet_remaining": 86.42,
  "quota_pack_remaining": 0,
  "store_credit_remaining_balance_units": 0,
  "store_credit_remaining_usd": 0,
  "store_credit_prepaid_remaining_cny": 0,
  "used": 13.58,
  "limit": 100,
  "quota": { "limit": 100, "used": 13.58, "remaining": 86.42, "unit": "CNY" },
  "rate_limits": [
    { "window": "5h", "limit": 20, "used": 3.2, "remaining": 16.8, "unit": "CNY" }
  ],
  "unit": "CNY",
  "currency": "CNY",
  "is_active": true,
  "isValid": true,
  "mode": "quota_limited",
  "status": "active",
  "source": "market_ledger",
  "scope": "key_routes",
  "amount_kind": "consumption_credit",
  "as_of": "2026-11-05T08:30:00Z"
}`,
    requestParams: [],
    responseParams: [
      { name: 'remaining', type: 'number', required: true, defaultValue: '-', description: '综合可用余额，取钱包、额度包与限流窗口中剩余值的最小值，单位为 CNY。' },
      { name: 'balance', type: 'number', required: true, defaultValue: '-', description: '与 remaining 相同的兼容字段。' },
      { name: 'quota_remaining', type: 'number', required: true, defaultValue: '-', description: '与 remaining 相同的兼容字段。' },
      { name: 'wallet_remaining', type: 'number', required: true, defaultValue: '-', description: '钱包现金余额，单位为 CNY。' },
      { name: 'quota_pack_remaining', type: 'number', required: true, defaultValue: '0', description: '额度包剩余金额；按额度包计费的 Key 只有这一项有值。' },
      { name: 'store_credit_remaining_balance_units', type: 'number', required: true, defaultValue: '0', description: '店铺券余额，只能在受限店铺与模型范围内消费。' },
      { name: 'store_credit_remaining_usd', type: 'number', required: true, defaultValue: '0', description: '店铺券余额的美元折算值。' },
      { name: 'store_credit_prepaid_remaining_cny', type: 'number', required: true, defaultValue: '0', description: '店铺券预付款中尚未使用的 CNY 金额。' },
      { name: 'used', type: 'number', required: true, defaultValue: '0', description: '该 Key 累计已消费金额，单位为 CNY。' },
      { name: 'limit', type: 'number', required: false, defaultValue: 'null', description: 'Key 的总额度上限；未设置时不返回。' },
      { name: 'quota', type: 'object', required: false, defaultValue: 'null', description: '总额度明细；仅在设置了额度上限时返回。' },
      { name: 'quota.limit', type: 'number', required: true, defaultValue: '-', description: '额度上限，单位为 CNY。' },
      { name: 'quota.used', type: 'number', required: true, defaultValue: '-', description: '已使用金额，单位为 CNY。' },
      { name: 'quota.remaining', type: 'number', required: true, defaultValue: '-', description: '额度剩余金额，单位为 CNY。' },
      { name: 'quota.unit', type: 'string', required: true, defaultValue: '"CNY"', description: '额度单位。' },
      { name: 'rate_limits', type: 'array', required: false, defaultValue: '[]', description: '限流窗口列表；只有配置了限流上限的窗口才会出现。' },
      { name: 'rate_limits[].window', type: 'string', required: true, defaultValue: '-', description: '窗口长度：5h、1d 或 7d。' },
      { name: 'rate_limits[].limit', type: 'number', required: true, defaultValue: '-', description: '该窗口的消费上限，单位为 CNY。' },
      { name: 'rate_limits[].used', type: 'number', required: true, defaultValue: '-', description: '该窗口内已消费金额，单位为 CNY。' },
      { name: 'rate_limits[].remaining', type: 'number', required: true, defaultValue: '-', description: '该窗口内剩余可用金额，单位为 CNY。' },
      { name: 'rate_limits[].unit', type: 'string', required: true, defaultValue: '"CNY"', description: '金额单位。' },
      { name: 'unit', type: 'string', required: true, defaultValue: '"CNY"', description: '金额单位。' },
      { name: 'currency', type: 'string', required: true, defaultValue: '"CNY"', description: '币种。' },
      { name: 'is_active', type: 'boolean', required: true, defaultValue: 'true', description: 'Key 当前是否仍可发起请求（余额是否大于 0）。' },
      { name: 'isValid', type: 'boolean', required: true, defaultValue: 'true', description: '与 is_active 相同的兼容字段，供客户端统计插件读取。' },
      { name: 'mode', type: 'string', required: true, defaultValue: '"unrestricted"', description: 'unrestricted 表示只受余额限制，quota_limited 表示还受额度或限流窗口限制。' },
      { name: 'status', type: 'string', required: true, defaultValue: '"active"', description: 'active 或 balance_exhausted。' },
      { name: 'source', type: 'string', required: true, defaultValue: '"market_ledger"', description: '数据来源，固定为市场账本。' },
      { name: 'scope', type: 'string', required: true, defaultValue: '"key_routes"', description: '统计范围，固定为当前 Key 绑定的路由。' },
      { name: 'amount_kind', type: 'string', required: true, defaultValue: '"consumption_credit"', description: '余额性质，固定为消费额度。' },
      { name: 'as_of', type: 'string', required: true, defaultValue: '-', description: '快照时间，RFC 3339 格式。' },
    ],
  },
]

export const EXAMPLE_LANGUAGES: Array<{ id: ExampleLanguage; label: string }> = [
  { id: 'curl', label: 'cURL' },
  { id: 'python', label: 'Python' },
  { id: 'go', label: 'Go' },
  { id: 'java', label: 'Java' },
]

export const QUICKSTART_CURL = `curl "$API_BASE/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "$EXAMPLE_MODEL",
    "messages": [{"role":"user","content":"Hello"}]
  }'`

function resolveExampleModel(endpoint: ApiDocEndpoint, model: string): string {
  return (endpoint.exampleModel ?? model.trim()) || DEFAULT_EXAMPLE_MODEL
}

function exampleRequestBody(endpoint: ApiDocEndpoint, model: string): string | undefined {
  return endpoint.request?.trim().replace(/("model"\s*:\s*)"[^"]+"/, `$1"${model}"`)
}

export function buildExampleCode(
  endpoint: ApiDocEndpoint,
  baseURL: string,
  model: string,
  language: ExampleLanguage,
): string {
  const model_ = resolveExampleModel(endpoint, model)
  const examplePath = endpoint.path.replace('{model}', model_)
  const url = `${baseURL || '$API_BASE'}${examplePath}`
  const requestBody = exampleRequestBody(endpoint, model_)
  const isGoogle = endpoint.group === 'Google 原生'
  const headerName = isGoogle ? 'x-goog-api-key' : 'Authorization'
  const isPost = endpoint.method === 'POST'

  if (language === 'curl') {
    const slash = String.fromCharCode(92)
    const lines = [`curl "${url}" \\\n  -H "${headerName}: ${isGoogle ? '$API_KEY' : 'Bearer $API_KEY'}"`]
    if (isPost) lines[0] += ` ${slash}`
    if (isPost) {
      lines.push(`  -H "Content-Type: application/json"${requestBody ? ` ${slash}` : ''}`)
      if (requestBody) lines.push(`  -d '${requestBody.replace(/'/g, "'\\''")}'`)
    }
    return lines.join('\n')
  }

  if (language === 'python') {
    const authLine = isGoogle
      ? '    "x-goog-api-key": os.environ["API_KEY"],'
      : '    "Authorization": f"Bearer {os.environ[\'API_KEY\']}",'
    const lines = [
      'import os',
      'import requests',
      '',
      `url = ${JSON.stringify(url)}`,
      'headers = {',
      authLine,
      ...(isPost ? ['    "Content-Type": "application/json",'] : []),
      '}',
    ]
    if (requestBody) {
      lines.push('', `payload = r'''${requestBody}'''`, 'response = requests.post(url, headers=headers, data=payload)')
    } else {
      lines.push('', 'response = requests.get(url, headers=headers)')
    }
    lines.push('response.raise_for_status()', 'print(response.json())')
    return lines.join('\n')
  }

  if (language === 'go') {
    const imports = ['"fmt"', '"io"', '"net/http"', '"os"']
    if (requestBody) imports.push('"strings"')
    const lines = [
      'package main',
      '',
      'import (',
      ...imports.map((item) => `\t${item}`),
      ')',
      '',
      'func main() {',
      '    var body io.Reader',
      ...(requestBody ? [`    body = strings.NewReader(\`${requestBody}\`)`] : []),
      `    req, err := http.NewRequest(http.Method${isPost ? 'Post' : 'Get'}, ${JSON.stringify(url)}, body)`,
      '    if err != nil { panic(err) }',
      `    req.Header.Set("${headerName}", ${isGoogle ? 'os.Getenv("API_KEY")' : '"Bearer " + os.Getenv("API_KEY")'})`,
      ...(isPost ? ['    req.Header.Set("Content-Type", "application/json")'] : []),
      '    resp, err := http.DefaultClient.Do(req)',
      '    if err != nil { panic(err) }',
      '    defer resp.Body.Close()',
      '    fmt.Println(resp.Status)',
      '    _, _ = io.Copy(os.Stdout, resp.Body)',
      '}',
    ]
    return lines.join('\n')
  }

  const javaAuth = isGoogle
    ? '.header("x-goog-api-key", apiKey)'
    : '.header("Authorization", "Bearer " + apiKey)'
  const javaRequest = requestBody
    ? `.method("POST", HttpRequest.BodyPublishers.ofString("""\n${requestBody}\n"""))`
    : '.GET()'
  return [
    'import java.net.URI;',
    'import java.net.http.HttpClient;',
    'import java.net.http.HttpRequest;',
    'import java.net.http.HttpResponse;',
    '',
    'public class Main {',
    '  public static void main(String[] args) throws Exception {',
    '    String apiKey = System.getenv("API_KEY");',
    '    HttpRequest.Builder builder = HttpRequest.newBuilder()',
    `        .uri(URI.create(${JSON.stringify(url)}))`,
    `        ${javaAuth}`,
    ...(isPost ? ['        .header("Content-Type", "application/json");'] : ['        ;']),
    `    HttpRequest request = builder${javaRequest}.build();`,
    '    HttpResponse<String> response = HttpClient.newHttpClient().send(request, HttpResponse.BodyHandlers.ofString());',
    '    System.out.println(response.statusCode());',
    '    System.out.println(response.body());',
    '  }',
    '}',
  ].join('\n')
}

function markdownTableCell(value: string): string {
  return value.replace(/\|/g, '\\|').replace(/[\r\n]+/g, ' ')
}

function markdownParameterTable(parameters: ApiDocParameter[]): string {
  if (parameters.length === 0) return '无参数。'
  return [
    '| 参数名称 | 类型 | 是否可选 | 默认值 | 说明 |',
    '| --- | --- | --- | --- | --- |',
    ...parameters.map((parameter) => `| \`${markdownTableCell(parameter.name)}\` | \`${markdownTableCell(parameter.type)}\` | ${parameter.required ? '否' : '是'} | \`${markdownTableCell(parameter.defaultValue)}\` | ${markdownTableCell(parameter.description)} |`),
  ].join('\n')
}

export function buildMarkdownDocument(endpoint: ApiDocEndpoint, baseURL: string, model: string): string {
  const requestBody = exampleRequestBody(endpoint, resolveExampleModel(endpoint, model))
  const examples = EXAMPLE_LANGUAGES.map((item) => `### ${item.label}\n\n\`\`\`${item.id}\n${buildExampleCode(endpoint, baseURL, model, item.id)}\n\`\`\``)
  return [
    `# ${endpoint.title}`,
    '',
    `> ${endpoint.group} · \`${endpoint.method}\` \`${endpoint.path}\``,
    '',
    endpoint.description,
    '',
    '## 认证方式',
    '',
    endpoint.auth,
    '',
    '## 请求参数',
    '',
    markdownParameterTable(endpoint.requestParams),
    '',
    requestBody ? '## 请求体示例' : '',
    requestBody ? '' : '',
    requestBody ? `\`\`\`json\n${requestBody}\n\`\`\`` : '',
    requestBody ? '' : '',
    '## 响应参数',
    '',
    markdownParameterTable(endpoint.responseParams),
    '',
    '## 响应示例',
    '',
    `\`\`\`json\n${endpoint.response.trim()}\n\`\`\``,
    '',
    '## 调用示例',
    '',
    ...examples.flatMap((example) => [example, '']),
  ].filter((line, index, lines) => !(line === '' && (lines[index - 1] ?? '') === '')).join('\n').trimEnd() + '\n'
}

const codeTokenPattern = /(#.*$|\/\/.*$|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\b(?:true|false|null|undefined)\b|\b\d+(?:\.\d+)?\b|--?[A-Za-z][\w-]*|[A-Za-z_$][\w$.-]*|[{}()[\],.;:=])/gm

const codeKeywords: Record<string, Set<string>> = {
  shell: new Set(['curl']),
  curl: new Set(['curl']),
  python: new Set(['import', 'from', 'as', 'def', 'if', 'for', 'in', 'return', 'True', 'False', 'None']),
  go: new Set(['package', 'import', 'func', 'var', 'if', 'range', 'defer', 'return', 'panic']),
  java: new Set(['import', 'public', 'class', 'static', 'void', 'new', 'throws', 'String']),
  json: new Set(['true', 'false', 'null']),
}

const CODE_TOKEN_CLASS = 'api-doc-code-token'

/** 返回 token 的高亮类别后缀；null 表示不需要高亮。 */
function codeTokenClass(token: string, language: string): string | null {
  if (token.startsWith('#') || token.startsWith('//')) return 'comment'
  if (/^["'`]/.test(token)) return 'string'
  if (/^\d/.test(token)) return 'number'
  if (/^--?/.test(token)) return 'flag'
  if (codeKeywords[language]?.has(token)) return 'keyword'
  if (language === 'json' && /^[A-Za-z_$]/.test(token)) return 'property'
  if (/^[{}()[\],.;:=]$/.test(token)) return 'punctuation'
  return null
}

export interface ApiDocCodeToken {
  text: string
  /** 可直接用于 :class 的完整类名；null 表示纯文本片段。 */
  cls: string | null
}

/**
 * 把代码切成高亮片段，替代源实现里返回 React 节点的 highlightCode。
 * 所有片段的 text 拼接后与原 code 完全一致。
 */
export function tokenizeCode(code: string, language: string): ApiDocCodeToken[] {
  const tokens: ApiDocCodeToken[] = []
  let lastIndex = 0
  for (const match of code.matchAll(codeTokenPattern)) {
    const token = match[0]
    const index = match.index ?? 0
    if (index > lastIndex) tokens.push({ text: code.slice(lastIndex, index), cls: null })
    const tokenClass = codeTokenClass(token, language)
    tokens.push({ text: token, cls: tokenClass ? `${CODE_TOKEN_CLASS} ${CODE_TOKEN_CLASS}--${tokenClass}` : null })
    lastIndex = index + token.length
  }
  if (lastIndex < code.length) tokens.push({ text: code.slice(lastIndex), cls: null })
  return tokens
}

export function codeLanguageLabel(language: string): string {
  return language === 'shell' ? 'shell' : language === 'curl' ? 'cURL' : language
}
