export interface ImportProxy {
  name?: string
  protocol: string
  host: string
  port: number
}

export interface ImportChoice {
  name: string
  platform?: string
  type?: string
  /** 导出文件 proxies 中该账号 proxy_key 对应的代理，不含认证信息。 */
  proxy?: ImportProxy
  /** 账号引用了代理，但文件中找不到对应条目。 */
  proxyMissing?: boolean
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

const text = (v: unknown) => (typeof v === 'string' ? v.trim() : '')

// 与 sub2api 一致：缺少 proxy_key 时由地址和认证信息拼出。
function proxyKey(p: Record<string, unknown>): string {
  if (typeof p.proxy_key === 'string' && p.proxy_key) return p.proxy_key
  return `${text(p.protocol)}|${text(p.host)}|${Number(p.port) || 0}|${text(p.username)}|${text(p.password)}`
}

/** 仅返回账号名称、类型及代理地址用于选择，令牌和代理密码不进入预览。后端独立校验原始数据。 */
export function accountImportChoices(value: unknown, depth = 0): ImportChoice[] {
  if (depth > 2) throw new Error('导入文件嵌套过深')
  if (record(value) && 'data' in value) return accountImportChoices(value.data, depth + 1)
  let accounts: unknown[]
  let proxies: Record<string, unknown>[] = []
  if (Array.isArray(value)) accounts = value
  else if (record(value) && 'accounts' in value) {
    if (value.type && !['sub2api-data', 'sub2api-bundle'].includes(String(value.type))) throw new Error('不支持的导入文件类型')
    if (value.version && value.version !== 1) throw new Error('不支持的导入文件版本')
    if (!Array.isArray(value.accounts)) throw new Error('accounts 必须是账号数组')
    if (value.proxies != null && !Array.isArray(value.proxies)) throw new Error('proxies 必须是代理数组')
    accounts = value.accounts
    proxies = ((value.proxies as unknown[] | undefined) ?? []).filter(record)
  } else accounts = [value]
  if (!accounts.length) throw new Error('文件中没有可导入的账号')
  return accounts.map((a, i) => {
    if (!record(a)) throw new Error('账号格式不正确')
    const credentials = 'credentials' in a ? a.credentials : a
    if (!record(credentials) || (!credentials.access_token && !credentials.api_key)) throw new Error(`账号 ${i + 1} 缺少 access_token 或 api_key`)
    const choice: ImportChoice = {
      name: typeof a.name === 'string' && a.name ? a.name : `账号 ${i + 1}`,
      platform: typeof a.platform === 'string' ? a.platform : undefined,
      type: typeof a.type === 'string' ? a.type : undefined,
    }
    if (typeof a.proxy_key === 'string' && a.proxy_key) {
      const p = proxies.find((p) => proxyKey(p) === a.proxy_key)
      if (p) choice.proxy = { name: text(p.name) || undefined, protocol: text(p.protocol), host: text(p.host), port: Number(p.port) || 0 }
      else choice.proxyMissing = true
    }
    return choice
  })
}
