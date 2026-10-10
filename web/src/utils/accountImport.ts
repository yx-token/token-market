export interface ImportChoice {
  name: string
  platform?: string
  type?: string
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

/** 仅返回账号名称及类型用于选择，令牌不进入预览。后端独立校验原始数据。 */
export function accountImportChoices(value: unknown, depth = 0): ImportChoice[] {
  if (depth > 2) throw new Error('导入文件嵌套过深')
  if (record(value) && 'data' in value) return accountImportChoices(value.data, depth + 1)
  let accounts: unknown[]
  if (Array.isArray(value)) accounts = value
  else if (record(value) && 'accounts' in value) {
    if (value.type && !['sub2api-data', 'sub2api-bundle'].includes(String(value.type))) throw new Error('不支持的导入文件类型')
    if (value.version && value.version !== 1) throw new Error('不支持的导入文件版本')
    if (!Array.isArray(value.accounts)) throw new Error('accounts 必须是账号数组')
    accounts = value.accounts
  } else accounts = [value]
  if (!accounts.length) throw new Error('文件中没有可导入的账号')
  return accounts.map((a, i) => {
    if (!record(a)) throw new Error('账号格式不正确')
    const credentials = 'credentials' in a ? a.credentials : a
    if (!record(credentials) || (!credentials.access_token && !credentials.api_key)) throw new Error(`账号 ${i + 1} 缺少 access_token 或 api_key`)
    return {
      name: typeof a.name === 'string' && a.name ? a.name : `账号 ${i + 1}`,
      platform: typeof a.platform === 'string' ? a.platform : undefined,
      type: typeof a.type === 'string' ? a.type : undefined,
    }
  })
}
