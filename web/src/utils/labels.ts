// 状态、厂商等枚举值的中文名称与徽章颜色。

export type Tone = 'gray' | 'green' | 'blue' | 'yellow' | 'red' | 'purple'

export interface Label {
  text: string
  tone: Tone
}

const table: Record<string, Record<string, Label>> = {
  platform: {
    anthropic: { text: 'Claude', tone: 'purple' },
    openai: { text: 'ChatGPT / Codex', tone: 'green' },
    gemini: { text: 'Gemini', tone: 'blue' },
    antigravity: { text: 'Antigravity', tone: 'blue' },
    grok: { text: 'Grok', tone: 'gray' },
    kimi: { text: 'Kimi', tone: 'yellow' },
    zhipu: { text: '智谱', tone: 'yellow' },
    minimax: { text: 'MiniMax', tone: 'yellow' },
    opencode_go: { text: 'OpenCode Go', tone: 'yellow' },
    composite: { text: '全模型', tone: 'gray' },
  },
  authType: {
    oauth: { text: 'OAuth 授权', tone: 'blue' },
    'setup-token': { text: 'Setup Token', tone: 'blue' },
    google_one: { text: 'Google One', tone: 'blue' },
    coding: { text: 'Coding Plan', tone: 'yellow' },
    'coding-intl': { text: 'Coding Plan 国际站', tone: 'yellow' },
    go: { text: 'Go 订阅', tone: 'yellow' },
  },
  upstreamStatus: {
    draft: { text: '草稿', tone: 'gray' },
    authorizing: { text: '授权中', tone: 'blue' },
    verifying: { text: '检测中', tone: 'blue' },
    verify_failed: { text: '检测未通过', tone: 'red' },
    ready: { text: '待挂单', tone: 'green' },
    listed: { text: '已挂单', tone: 'green' },
    suspended: { text: '已冻结', tone: 'red' },
    removing: { text: '删除中', tone: 'gray' },
    removed: { text: '已删除', tone: 'gray' },
    failed: { text: '失败', tone: 'red' },
  },
  health: {
    unknown: { text: '未检测', tone: 'gray' },
    healthy: { text: '正常', tone: 'green' },
    limited: { text: '窗口限额', tone: 'yellow' },
    suspected: { text: '疑似降智', tone: 'yellow' },
    degraded: { text: '降智', tone: 'red' },
    unavailable: { text: '不可用', tone: 'red' },
  },
  mode: {
    consignment: { text: '寄售', tone: 'blue' },
    buyback: { text: '平台回收', tone: 'purple' },
  },
  listingStatus: {
    pending_quote: { text: '待报价', tone: 'gray' },
    pending_confirm: { text: '待确认报价', tone: 'yellow' },
    active: { text: '生效中', tone: 'green' },
    paused: { text: '已暂停', tone: 'yellow' },
    closed: { text: '已关闭', tone: 'gray' },
  },
  quoteStatus: {
    offered: { text: '待确认', tone: 'yellow' },
    accepted: { text: '已接受', tone: 'green' },
    rejected: { text: '已拒绝', tone: 'gray' },
    superseded: { text: '已更新', tone: 'gray' },
    expired: { text: '已过期', tone: 'gray' },
  },
  keyStatus: {
    provisioning: { text: '开通中', tone: 'blue' },
    active: { text: '启用', tone: 'green' },
    disabled: { text: '停用', tone: 'gray' },
    deleted: { text: '已删除', tone: 'gray' },
  },
  hopState: {
    ready: { text: '正常', tone: 'green' },
    provisioning: { text: '开通中', tone: 'blue' },
    skipped: { text: '暂不可用（跳过）', tone: 'yellow' },
  },
  rechargeStatus: {
    pending: { text: '待支付', tone: 'yellow' },
    crediting: { text: '到账中', tone: 'blue' },
    credited: { text: '已到账', tone: 'green' },
    failed: { text: '失败', tone: 'red' },
    cancelled: { text: '已取消', tone: 'gray' },
    expired: { text: '已过期', tone: 'gray' },
  },
  paymentChannel: {
    alipay_page: { text: '支付宝网页支付', tone: 'blue' },
    alipay_f2f: { text: '支付宝扫码', tone: 'blue' },
    wechat_native: { text: '微信扫码', tone: 'green' },
    manual: { text: '人工确认', tone: 'gray' },
  },
  closeReason: {
    'cancelled by user': { text: '已由你取消', tone: 'gray' },
    'payment timeout': { text: '超时未支付', tone: 'gray' },
    'closed by payment channel': { text: '支付渠道已关闭交易', tone: 'gray' },
    'crediting sub2api balance failed': { text: '入账失败，平台会人工处理', tone: 'red' },
  },
  payoutStatus: {
    pending: { text: '待发起', tone: 'blue' },
    processing: { text: '退款中', tone: 'blue' },
    success: { text: '已退回', tone: 'green' },
    failed: { text: '失败', tone: 'red' },
    manual: { text: '待人工打款', tone: 'yellow' },
  },
  notifyResult: {
    credited: { text: '已入账', tone: 'green' },
    duplicate: { text: '重复通知', tone: 'gray' },
    expired: { text: '已过期', tone: 'gray' },
    ignored: { text: '忽略', tone: 'gray' },
    amount_mismatch: { text: '金额不一致', tone: 'red' },
    unknown_order: { text: '未知订单', tone: 'yellow' },
    invalid: { text: '验签失败', tone: 'red' },
    error: { text: '处理出错', tone: 'red' },
  },
  tpStatus: {
    pending: { text: '排队中', tone: 'blue' },
    running: { text: '检测中', tone: 'blue' },
    completed: { text: '已完成', tone: 'green' },
    failed: { text: '失败', tone: 'red' },
    timeout: { text: '超时', tone: 'yellow' },
    skipped: { text: '已跳过', tone: 'gray' },
  },
  tpOutcome: {
    pass: { text: '身份一致', tone: 'green' },
    submodel_mismatch: { text: '型号不一致', tone: 'yellow' },
    mismatch: { text: '身份不一致', tone: 'red' },
    spoof: { text: '疑似伪装', tone: 'red' },
    inconclusive: { text: '证据不足', tone: 'gray' },
  },
  tpAction: {
    degraded: { text: '判为降智', tone: 'red' },
    suspected: { text: '判为疑似', tone: 'yellow' },
    ignore: { text: '仅记录', tone: 'gray' },
  },
  refundStatus: {
    locking: { text: '暂停 Key 中', tone: 'blue' },
    subtracting: { text: '扣减余额中', tone: 'blue' },
    paying: { text: '待打款', tone: 'yellow' },
    done: { text: '已完成', tone: 'green' },
    failed: { text: '失败', tone: 'red' },
  },
  withdrawalStatus: {
    pending_review: { text: '待审核', tone: 'yellow' },
    approved: { text: '已通过，待打款', tone: 'blue' },
    rejected: { text: '已驳回', tone: 'gray' },
    paid: { text: '已打款', tone: 'green' },
    failed: { text: '打款失败', tone: 'red' },
  },
  kycStatus: {
    none: { text: '未实名', tone: 'gray' },
    pending: { text: '审核中', tone: 'yellow' },
    verified: { text: '已实名', tone: 'green' },
    rejected: { text: '未通过', tone: 'red' },
  },
  verdict: {
    normal: { text: '正常', tone: 'green' },
    suspected: { text: '疑似', tone: 'yellow' },
    degraded: { text: '降智', tone: 'red' },
    unavailable: { text: '不可用', tone: 'red' },
    inconclusive: { text: '结论不足', tone: 'gray' },
  },
  trigger: {
    onboarding: { text: '上架检测', tone: 'blue' },
    scheduled: { text: '定时检测', tone: 'gray' },
    recovery: { text: '恢复复测', tone: 'blue' },
    seller_manual: { text: '手动复测', tone: 'blue' },
    admin: { text: '平台检测', tone: 'purple' },
    adaptive: { text: '追加检测', tone: 'yellow' },
    appeal: { text: '申诉复测', tone: 'purple' },
  },
  attribution: {
    account: { text: '账号本身', tone: 'red' },
    proxy: { text: '代理线路', tone: 'yellow' },
    vendor_wide: { text: '官方全局', tone: 'gray' },
    unknown: { text: '原因未明', tone: 'gray' },
  },
  appealStatus: {
    open: { text: '待处理', tone: 'yellow' },
    retesting: { text: '复测中', tone: 'blue' },
    overturned: { text: '已改判', tone: 'green' },
    upheld: { text: '维持原判', tone: 'gray' },
  },
  channel: {
    pool: { text: '默认分组', tone: 'blue' },
    direct: { text: '指定卖家', tone: 'purple' },
    probe: { text: '平台检测', tone: 'gray' },
    unknown: { text: '未知', tone: 'gray' },
  },
  endReason: {
    completed: { text: '完成', tone: 'green' },
    client_cancel: { text: '买家断开', tone: 'yellow' },
    upstream_error: { text: '上游错误', tone: 'red' },
    timeout: { text: '超时', tone: 'red' },
    skipped: { text: '跳过', tone: 'gray' },
    retry: { text: '失败后切换', tone: 'yellow' },
  },
  proxyStatus: {
    available: { text: '空闲', tone: 'green' },
    assigned: { text: '已分配', tone: 'blue' },
    disabled: { text: '已停用', tone: 'gray' },
  },
  anomalyStatus: {
    open: { text: '待处理', tone: 'yellow' },
    explained: { text: '已解释', tone: 'green' },
    ignored: { text: '已忽略', tone: 'gray' },
    adjusted: { text: '已调账', tone: 'blue' },
  },
  riskStatus: {
    open: { text: '待处理', tone: 'yellow' },
    resolved: { text: '已处理', tone: 'green' },
    ignored: { text: '已忽略', tone: 'gray' },
  },
  userStatus: {
    active: { text: '正常', tone: 'green' },
    frozen: { text: '已冻结', tone: 'red' },
  },
}

export function label(kind: keyof typeof table | string, value: string | null | undefined): Label {
  if (!value) return { text: '—', tone: 'gray' }
  return table[kind]?.[value] ?? { text: value, tone: 'gray' }
}

// 同一接入方式在不同厂商下的具体名称
const authTypeNames: Record<string, string> = {
  'gemini:oauth': 'GCP Code Assist',
  'gemini:google_one': 'Google One 个人订阅',
  'zhipu:coding': 'Coding Plan 国内站',
  'zhipu:coding-intl': 'Coding Plan 国际站（Z.ai）',
  'minimax:coding': 'Coding Plan 国内站',
  'minimax:coding-intl': 'Coding Plan 国际站',
}

/** 上游接入方式的名称，按厂商区分（例如 Gemini 的 oauth 是 GCP Code Assist） */
export function authTypeText(platform: string | null | undefined, authType: string | null | undefined): string {
  return authTypeNames[`${platform}:${authType}`] ?? label('authType', authType).text
}

/** 用 key 接入的订阅（Coding Plan、OpenCode Go），其余接入方式走 OAuth 授权 */
export function isKeyAuthType(authType: string | null | undefined): boolean {
  return authType === 'coding' || authType === 'coding-intl' || authType === 'go'
}

export function options(kind: string): { value: string; text: string }[] {
  return Object.entries(table[kind] ?? {}).map(([value, l]) => ({ value, text: l.text }))
}

/** 分布对象（{值: 数量}）转成文字，例如"正常 3，降智 1" */
export function distribution(kind: string, d: Record<string, number> | null | undefined): string {
  const parts = Object.entries(d ?? {}).map(([k, n]) => `${kind === 'grade' ? (k === '-' ? '未定级' : `${k} 级`) : label(kind, k).text} ${n}`)
  return parts.join('，') || '—'
}

export const anomalyRules: Record<string, string> = {
  R1_failed_with_charge: 'Edge 记为失败但 sub2api 产生了费用',
  R2_success_without_charge: 'Edge 记为成功但没有费用记录',
  R3_orphan_charge: '有费用但找不到 Edge 记录（可能绕过 Edge）',
  R4_duplicate: '同一请求多条费用',
  balance_mismatch: '买家余额与账本不一致',
  payment_amount_mismatch: '支付渠道通知的金额与订单不一致（未入账）',
}

export const riskKinds: Record<string, string> = {
  buyer_seller_linkage: '买卖双方使用过相同 IP',
  self_dealing: '自买自卖',
  direct_concentration: '新卖家收益集中来自单一买家',
}
