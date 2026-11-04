import { describe, expect, it } from 'vitest'
import {
  API_DOC_ENDPOINTS,
  API_DOC_ENDPOINT_GROUPS,
  ENDPOINT_GROUP_SLUGS,
  EXAMPLE_LANGUAGES,
  QUICKSTART_CURL,
  buildExampleCode,
  buildMarkdownDocument,
  codeLanguageLabel,
  tokenizeCode,
  type ApiDocEndpoint,
  type ApiDocEndpointGroup,
  type ExampleLanguage,
} from '../apiDocs'

const BASE_URL = 'https://api.example.com'
const LANGUAGES: ExampleLanguage[] = ['curl', 'python', 'go', 'java']

/** 与 market/internal/edge/proxy.go 的 allowedPath 保持一致（只列文档承诺放行的路径）。 */
function allowedPath(method: string, path: string): boolean {
  if (path === '/v1/usage') return method === 'GET'
  if (path === '/v1/messages' || path === '/v1/messages/count_tokens' || path === '/v1/chat/completions') {
    return method === 'POST'
  }
  if (path === '/v1/responses') return method === 'POST' || method === 'GET'
  if (path === '/v1/models' || path.startsWith('/v1/models/')) return method === 'GET'
  if (path === '/v1beta/models' || path.startsWith('/v1beta/models')) return true
  return false
}

function resolvedPath(endpoint: ApiDocEndpoint): string {
  return endpoint.path.replace('{model}', endpoint.exampleModel ?? '')
}

describe('API_DOC_ENDPOINTS', () => {
  it('ids are unique', () => {
    const ids = API_DOC_ENDPOINTS.map((endpoint) => endpoint.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('every path is allowed by the edge gateway', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      expect(allowedPath(endpoint.method, endpoint.path), `${endpoint.method} ${endpoint.path}`).toBe(true)
    }
  })

  it('only documents the chat, messages, gemini, models and usage endpoints', () => {
    expect(API_DOC_ENDPOINTS.map((endpoint) => `${endpoint.method} ${endpoint.path}`)).toEqual([
      'GET /v1/models',
      'POST /v1/chat/completions',
      'POST /v1/responses',
      'POST /v1/messages',
      'POST /v1/messages/count_tokens',
      'POST /v1beta/models/{model}:generateContent',
      'GET /v1/usage',
    ])
  })

  it('groups stay inside the declared union and all have slugs', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      expect(API_DOC_ENDPOINT_GROUPS).toContain(endpoint.group)
      expect(typeof ENDPOINT_GROUP_SLUGS[endpoint.group]).toBe('string')
      expect(ENDPOINT_GROUP_SLUGS[endpoint.group]).not.toBe('')
    }
    expect(Object.keys(ENDPOINT_GROUP_SLUGS).sort()).toEqual([...API_DOC_ENDPOINT_GROUPS].sort())
    expect(API_DOC_ENDPOINT_GROUPS).toEqual(['OpenAI 兼容', 'Anthropic 兼容', 'Google 原生', '账户'])
    expect(ENDPOINT_GROUP_SLUGS['OpenAI 兼容']).toBe('openai')
    expect(ENDPOINT_GROUP_SLUGS['Anthropic 兼容']).toBe('anthropic')
    expect(ENDPOINT_GROUP_SLUGS['Google 原生']).toBe('google')
    expect(ENDPOINT_GROUP_SLUGS['账户']).toBe('account')
  })

  it('每个分组都有端点，且没有遗留多模态条目', () => {
    const groups = new Set(API_DOC_ENDPOINTS.map((endpoint) => endpoint.group))
    for (const group of API_DOC_ENDPOINT_GROUPS) expect(groups.has(group as ApiDocEndpointGroup)).toBe(true)
    for (const endpoint of API_DOC_ENDPOINTS) {
      expect(endpoint.path).not.toMatch(/\/(?:embeddings|images|videos)\b/)
      expect(endpoint.group as string).not.toBe('多模态')
    }
  })

  it('示例模型使用当前模型，而不是旧的 gpt-4o-mini', () => {
    const byId = new Map(API_DOC_ENDPOINTS.map((endpoint) => [endpoint.id, endpoint]))
    expect(byId.get('chat-completions')?.exampleModel).toBe('gpt-5.5')
    expect(byId.get('responses')?.exampleModel).toBe('gpt-5.5')
    expect(byId.get('messages')?.exampleModel).toBe('claude-sonnet-4-5')
    expect(byId.get('messages-count-tokens')?.exampleModel).toBe('claude-sonnet-4-5')
    expect(byId.get('gemini-generate-content')?.exampleModel).toBe('gemini-2.5-pro')
    for (const endpoint of API_DOC_ENDPOINTS) {
      expect(endpoint.request ?? '').not.toContain('gpt-4o-mini')
      expect(endpoint.response).not.toContain('gpt-4o-mini')
    }
  })

  it('认证说明指向买家中心创建 API Key', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      expect(endpoint.auth).not.toContain('管理控制台')
      expect(endpoint.auth).toContain('买家中心')
      expect(endpoint.auth).toContain('API Key')
    }
  })

  it('usage 端点的响应字段与 edge 的 usageResponse 结构一致', () => {
    const usage = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'usage')
    expect(usage?.method).toBe('GET')
    const body = JSON.parse(usage?.response ?? '{}') as Record<string, unknown>
    for (const field of [
      'remaining',
      'balance',
      'quota_remaining',
      'wallet_remaining',
      'quota_pack_remaining',
      'store_credit_remaining_balance_units',
      'store_credit_remaining_usd',
      'store_credit_prepaid_remaining_cny',
      'used',
      'limit',
      'quota',
      'rate_limits',
      'unit',
      'currency',
      'is_active',
      'isValid',
      'mode',
      'status',
      'source',
      'scope',
      'amount_kind',
      'as_of',
    ]) {
      expect(body, field).toHaveProperty(field)
    }
    expect(body.unit).toBe('CNY')
    expect(body.currency).toBe('CNY')
    // 示例值要自洽：limit - used = remaining。
    expect(body.remaining).toBeCloseTo((body.limit as number) - (body.used as number), 2)
    expect((body.quota as Record<string, number>).remaining).toBeCloseTo(body.remaining as number, 2)
    expect((body.rate_limits as Array<Record<string, number>>)[0].remaining).toBeCloseTo(16.8, 2)
  })

  it('count_tokens 只需输入 token 响应', () => {
    const endpoint = API_DOC_ENDPOINTS.find((item) => item.id === 'messages-count-tokens')
    expect(endpoint?.path).toBe('/v1/messages/count_tokens')
    expect(JSON.parse(endpoint?.response ?? '{}')).toEqual({ input_tokens: 123 })
    expect(endpoint?.request ?? '').not.toContain('max_tokens')
  })
})

describe('buildExampleCode', () => {
  it('所有端点、四种语言都包含 baseURL 与端点路径', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      for (const language of LANGUAGES) {
        const code = buildExampleCode(endpoint, BASE_URL, '', language)
        expect(code, `${endpoint.id}/${language}`).toContain(BASE_URL)
        expect(code, `${endpoint.id}/${language}`).toContain(resolvedPath(endpoint))
      }
    }
  })

  it('GET 端点没有请求体也能生成可执行示例', () => {
    for (const endpoint of API_DOC_ENDPOINTS.filter((item) => item.method === 'GET')) {
      for (const language of LANGUAGES) {
        const code = buildExampleCode(endpoint, BASE_URL, '', language)
        expect(code.trim().length).toBeGreaterThan(0)
        expect(code).toContain(`${BASE_URL}${endpoint.path}`)
        expect(code).not.toContain('Content-Type: application/json')
      }
      const curl = buildExampleCode(endpoint, BASE_URL, '', 'curl')
      expect(curl).toContain('curl "')
      expect(curl).toContain('-H "Authorization: Bearer $API_KEY"')
      expect(curl).not.toContain('-d ')
    }
  })

  it('endpoint.exampleModel 优先于传入的 model', () => {
    const chat = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'chat-completions') as ApiDocEndpoint
    expect(buildExampleCode(chat, BASE_URL, 'my-own-model', 'curl')).toContain('"model": "gpt-5.5"')
    expect(buildExampleCode(chat, BASE_URL, 'my-own-model', 'curl')).not.toContain('my-own-model')

    const messages = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'messages') as ApiDocEndpoint
    expect(buildExampleCode(messages, BASE_URL, 'gpt-5.5', 'python')).toContain('claude-sonnet-4-5')
  })

  it('没有 exampleModel 的端点使用传入的 model，空值回落到默认模型', () => {
    const base = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'models') as ApiDocEndpoint
    const withBody: ApiDocEndpoint = { ...base, id: 'custom', method: 'POST', request: '{\n  "model": "placeholder"\n}' }
    expect(buildExampleCode(withBody, BASE_URL, ' my-model ', 'curl')).toContain('"model": "my-model"')
    expect(buildExampleCode(withBody, BASE_URL, '   ', 'curl')).toContain('"model": "gpt-5.5"')
  })

  it('baseURL 缺省时使用 $API_BASE 占位符', () => {
    const chat = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'chat-completions') as ApiDocEndpoint
    expect(buildExampleCode(chat, '', '', 'curl')).toContain('$API_BASE/v1/chat/completions')
  })

  it('Gemini 使用 x-goog-api-key 并把模型写进路径', () => {
    const gemini = API_DOC_ENDPOINTS.find((endpoint) => endpoint.group === 'Google 原生') as ApiDocEndpoint
    const curl = buildExampleCode(gemini, BASE_URL, '', 'curl')
    expect(curl).toContain('x-goog-api-key: $API_KEY')
    expect(curl).toContain(`${BASE_URL}/v1beta/models/gemini-2.5-pro:generateContent`)
    expect(buildExampleCode(gemini, BASE_URL, '', 'go')).toContain('req.Header.Set("x-goog-api-key", os.Getenv("API_KEY"))')
  })

  it('Anthropic 与 OpenAI 使用 Bearer 认证且转义单引号', () => {
    const messages = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'messages') as ApiDocEndpoint
    expect(buildExampleCode(messages, BASE_URL, '', 'java')).toContain('.header("Authorization", "Bearer " + apiKey)')
    const quoted: ApiDocEndpoint = { ...messages, request: '{"model":"x","system":"it\'s ok"}' }
    expect(buildExampleCode(quoted, BASE_URL, '', 'curl')).toContain(`'\\''`)
  })
})

describe('buildMarkdownDocument', () => {
  it('包含标题、分组、参数表与四种语言示例', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      const markdown = buildMarkdownDocument(endpoint, BASE_URL, '')
      expect(markdown).toContain(`# ${endpoint.title}`)
      expect(markdown).toContain(`\`${endpoint.method}\` \`${endpoint.path}\``)
      expect(markdown).toContain('| 参数名称 | 类型 | 是否可选 | 默认值 | 说明 |')
      expect(markdown).toContain('## 响应参数')
      for (const parameter of [...endpoint.requestParams, ...endpoint.responseParams]) {
        expect(markdown, parameter.name).toContain(`| \`${parameter.name}\` |`)
      }
      for (const language of EXAMPLE_LANGUAGES) {
        expect(markdown).toContain(`\`\`\`${language.id}`)
        expect(markdown).toContain(`${BASE_URL}${resolvedPath(endpoint).split('{')[0]}`)
      }
      expect(markdown.endsWith('\n')).toBe(true)
      expect(markdown).not.toContain('\n\n\n')
    }
  })

  it('无请求参数的端点写出「无参数」而不是空表', () => {
    const usage = API_DOC_ENDPOINTS.find((endpoint) => endpoint.id === 'usage') as ApiDocEndpoint
    const markdown = buildMarkdownDocument(usage, BASE_URL, '')
    expect(markdown).toContain('无参数。')
    expect(markdown).not.toContain('## 请求体示例')
  })

  it('参数中的竖线与换行被转义', () => {
    const endpoint: ApiDocEndpoint = {
      ...(API_DOC_ENDPOINTS[0] as ApiDocEndpoint),
      id: 'tricky',
      requestParams: [{ name: 'a|b', type: 'string', required: true, defaultValue: 'x\ny', description: '换行\n说明' }],
    }
    const markdown = buildMarkdownDocument(endpoint, BASE_URL, '')
    expect(markdown).toContain('`a\\|b`')
    expect(markdown).toContain('`x y`')
    expect(markdown).toContain('换行 说明')
  })
})

describe('tokenizeCode', () => {
  it('片段拼回原文', () => {
    for (const endpoint of API_DOC_ENDPOINTS) {
      for (const language of EXAMPLE_LANGUAGES) {
        const code = buildExampleCode(endpoint, BASE_URL, '', language.id)
        expect(tokenizeCode(code, language.id === 'curl' ? 'shell' : language.id).map((token) => token.text).join('')).toBe(code)
      }
    }
    const markdown = buildMarkdownDocument(API_DOC_ENDPOINTS[0] as ApiDocEndpoint, BASE_URL, '')
    expect(tokenizeCode(markdown, 'json').map((token) => token.text).join('')).toBe(markdown)
    expect(tokenizeCode('', 'json')).toEqual([])
  })

  it('识别字符串、数字、注释与关键字', () => {
    const code = 'curl "https://api.example.com" -H 42 # 备注'
    const tokens = tokenizeCode(code, 'shell')
    expect(tokens.map((token) => token.text).join('')).toBe(code)
    const withClass = (suffix: string) => tokens.find((token) => token.cls?.endsWith(`--${suffix}`))
    expect(withClass('string')?.text).toBe('"https://api.example.com"')
    expect(withClass('number')?.text).toBe('42')
    expect(withClass('comment')?.text).toBe('# 备注')
    expect(withClass('keyword')?.text).toBe('curl')
    expect(withClass('flag')?.text).toBe('-H')
    // 非高亮片段（空白）cls 为 null。
    expect(tokens.some((token) => token.cls === null && token.text.trim() === '')).toBe(true)
  })

  it('JSON 语言区分属性名、字符串与字面量', () => {
    const tokens = tokenizeCode('{"stream": true, "n": 1, bare: null}', 'json')
    // 带引号的键按字符串高亮，裸标识符按属性名高亮。
    expect(tokens.find((token) => token.cls?.endsWith('--property'))?.text).toBe('bare')
    expect(tokens.find((token) => token.cls?.endsWith('--string'))?.text).toBe('"stream"')
    expect(tokens.filter((token) => token.cls?.endsWith('--keyword')).map((token) => token.text)).toEqual(['true', 'null'])
    expect(tokens.find((token) => token.cls?.endsWith('--number'))?.text).toBe('1')
    expect(tokens.find((token) => token.cls?.endsWith('--punctuation'))?.text).toBe('{')
  })

  it('所有 cls 使用 api-doc- 前缀', () => {
    for (const token of tokenizeCode(buildExampleCode(API_DOC_ENDPOINTS[1] as ApiDocEndpoint, BASE_URL, '', 'go'), 'go')) {
      if (token.cls === null) continue
      for (const cls of token.cls.split(' ')) expect(cls.startsWith('api-doc-')).toBe(true)
    }
  })

  it('不修改共享正则状态：连续调用结果一致', () => {
    const code = 'curl "x" 1'
    expect(tokenizeCode(code, 'shell')).toEqual(tokenizeCode(code, 'shell'))
  })
})

describe('其他常量与工具', () => {
  it('codeLanguageLabel 复用源里的显示名', () => {
    expect(codeLanguageLabel('shell')).toBe('shell')
    expect(codeLanguageLabel('curl')).toBe('cURL')
    expect(codeLanguageLabel('python')).toBe('python')
    expect(codeLanguageLabel('json')).toBe('json')
  })

  it('EXAMPLE_LANGUAGES 覆盖四种语言', () => {
    expect(EXAMPLE_LANGUAGES.map((item) => item.id)).toEqual(LANGUAGES)
    expect(EXAMPLE_LANGUAGES.map((item) => item.label)).toEqual(['cURL', 'Python', 'Go', 'Java'])
  })

  it('QUICKSTART_CURL 仍指向 chat/completions 并用占位符', () => {
    expect(QUICKSTART_CURL).toContain('$API_BASE/v1/chat/completions')
    expect(QUICKSTART_CURL).toContain('Authorization: Bearer $API_KEY')
    expect(QUICKSTART_CURL).toContain('$EXAMPLE_MODEL')
    expect(QUICKSTART_CURL).not.toContain('gpt-4o-mini')
  })
})
