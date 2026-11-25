const modal = document.getElementById('modal')
const modalTitle = document.getElementById('modalTitle')
const modalPreflight = document.getElementById('modalPreflight')
const modalRunning = document.getElementById('modalRunning')
const modalDone = document.getElementById('modalDone')
const modalError = document.getElementById('modalError')
const modalRecharge = document.getElementById('modalRecharge')
const preflightLead = document.getElementById('preflightLead')
const preflightChecks = document.getElementById('preflightChecks')
const preflightActions = document.getElementById('preflightActions')
const preflightContinueBtn = document.getElementById('preflightContinueBtn')
const statusEl = document.getElementById('status')
const doneMsg = document.getElementById('doneMsg')
const errMsg = document.getElementById('errMsg')
const errHint = document.getElementById('errHint')
const errLoginBtn = document.getElementById('errLoginBtn')
const accountBar = document.getElementById('accountBar')
const idleLead = document.getElementById('idleLead')
const idleHint = document.getElementById('idleHint')
const credHint = document.getElementById('credHint')
const agentList = document.getElementById('agentList')
const donePrimaryBtn = document.getElementById('donePrimaryBtn')
const doneHelpBtn = document.getElementById('doneHelpBtn')
const errHelpBtn = document.getElementById('errHelpBtn')
const doneHint = document.getElementById('doneHint')
const doneGuide = document.getElementById('doneGuide')

let SITE = ''
let LOGIN = ''
const WALLET_POLL_MS = 5000
const RECHARGE_POLL_MS = 2000

async function refreshSiteLinks() {
  try {
    const s = await window.xianmaoHelper?.site?.()
    if (s?.api) {
      const api = String(s.api).replace(/\/$/, '')
      SITE = `${api}/`
      LOGIN = s.login || `${api}/login`
    } else {
      SITE = ''
      LOGIN = ''
    }
    return s
  } catch {
    return null
  }
}


const CHANNEL_LABEL = {
  alipay_page: '支付宝网页',
  alipay_f2f: '支付宝扫码',
  wechat_native: '微信扫码',
}
const CHANNEL_HINT = {
  alipay_page: '打开支付宝收银台付款（网页）',
  alipay_f2f: '用支付宝 App 扫码，留在助手内完成',
  wechat_native: '用微信扫码，留在助手内完成',
}

let lastClient = ''
let lastOpenCli = ''
let lastHelpUrl = ''
let agentsState = { bound: false, hasCredentials: false, agents: [] }
let modalMode = ''
let pendingPreflightClient = ''
let walletTimer = null
let lastWallet = null
let lastAccount = null
let rechargePollTimer = null
let rechargeClockTimer = null
let rechargeOrder = null
let rechargeOptions = { channels: [], recharge_min: 1, recharge_max: 50000 }
let selectedChannel = ''

function money(v) {
  if (v === null || v === undefined || Number.isNaN(Number(v))) return '—'
  const n = Number(v)
  const abs = Math.abs(n)
  const d = abs > 0 && abs < 0.01 ? 4 : 2
  return `¥${n.toFixed(d)}`
}

function renderWallet(wallet) {
  const row = document.getElementById('walletRow')
  const bal = document.getElementById('walletBalance')
  if (!row || !bal) return
  if (!wallet || wallet.ok === false) {
    row.hidden = true
    lastWallet = null
    return
  }
  lastWallet = wallet
  row.hidden = false
  const label =
    wallet.balance === null || wallet.balance === undefined
      ? '余额 —'
      : `余额 ${money(wallet.balance)}`
  bal.textContent = label
  bal.title = wallet.account ? `${wallet.account} · 实时余额` : '实时余额'
}

async function refreshWallet() {
  if (!window.xianmaoHelper?.wallet) {
    renderWallet(null)
    return null
  }
  try {
    const w = await window.xianmaoHelper.wallet()
    if (w?.ok) {
      renderWallet(w)
      return w
    }
    renderWallet(null)
    return null
  } catch {
    renderWallet(null)
    return null
  }
}

function stopWalletPoll() {
  if (walletTimer) {
    clearInterval(walletTimer)
    walletTimer = null
  }
}

function startWalletPoll() {
  stopWalletPoll()
  void refreshWallet()
  walletTimer = setInterval(() => {
    void refreshWallet()
  }, WALLET_POLL_MS)
}

function setHelpVisible(url) {
  lastHelpUrl = url || ''
  const show = !!lastHelpUrl
  if (doneHelpBtn) {
    doneHelpBtn.classList.toggle('hidden', !show)
    doneHelpBtn.textContent = '打开安装说明'
    doneHelpBtn.dataset.action = 'open-help'
  }
  if (errHelpBtn) {
    errHelpBtn.classList.toggle('hidden', !show)
    errHelpBtn.textContent = '打开安装说明'
    errHelpBtn.dataset.action = 'open-help'
  }
}

function resetSteps() {
  document.querySelectorAll('#steps [data-step]').forEach((li) => {
    li.classList.remove('is-active', 'is-done', 'is-error')
  })
}

function syncErrorLoginBtn() {
  const loggedIn = isLoggedIn(lastAccount)
  if (errLoginBtn) errLoginBtn.classList.toggle('hidden', loggedIn)
  if (errHint) {
    errHint.textContent = loggedIn
      ? '可点下方按钮继续，或关闭后重试。'
      : '可以再试一次登录，或点「打开安装说明」。'
  }
}

function stopRechargePoll() {
  if (rechargePollTimer) {
    clearInterval(rechargePollTimer)
    rechargePollTimer = null
  }
  if (rechargeClockTimer) {
    clearInterval(rechargeClockTimer)
    rechargeClockTimer = null
  }
}

function openModal(mode, title) {
  if (mode !== 'recharge') stopRechargePoll()
  modalMode = mode
  if (modalTitle) {
    modalTitle.textContent =
      title ||
      (mode === 'preflight'
        ? '安装前检测'
        : mode === 'running'
          ? '处理中'
          : mode === 'done'
            ? '完成'
            : mode === 'recharge'
              ? '充值'
              : '提示')
  }
  modalPreflight?.classList.toggle('hidden', mode !== 'preflight')
  modalRunning?.classList.toggle('hidden', mode !== 'running')
  modalDone?.classList.toggle('hidden', mode !== 'done')
  modalError?.classList.toggle('hidden', mode !== 'error')
  modalRecharge?.classList.toggle('hidden', mode !== 'recharge')
  if (mode !== 'done') renderDoneGuide(null)
  if (mode === 'error') syncErrorLoginBtn()
  modal?.classList.remove('hidden')
  document.body.classList.add('modal-open')
}

function closeModal() {
  stopRechargePoll()
  modal?.classList.add('hidden')
  document.body.classList.remove('modal-open')
  modalMode = ''
  pendingPreflightClient = ''
  rechargeOrder = null
}

function setStep(id, status) {
  const li = document.querySelector(`#steps [data-step="${id}"]`)
  if (!li) return
  li.classList.remove('is-active', 'is-done', 'is-error')
  if (status === 'active') li.classList.add('is-active')
  if (status === 'done') li.classList.add('is-done')
  if (status === 'error') li.classList.add('is-error')
}

function clientOpenAction(client, opts = {}) {
  if (!client) return { action: 'open-site', label: '回闲猫网站' }
  if (opts.cli) return { action: `open-agent:${client}`, label: '再开终端' }
  return { action: `open-agent:${client}`, label: '打开客户端' }
}

function renderDonePrimary(client, opts = {}) {
  const cfg = clientOpenAction(client, opts)
  if (!donePrimaryBtn) return
  donePrimaryBtn.dataset.action = cfg.action
  donePrimaryBtn.textContent = cfg.label
}

function renderDoneGuide(steps) {
  if (!doneGuide) return
  const list = Array.isArray(steps) ? steps.filter((s) => String(s || '').trim()) : []
  doneGuide.innerHTML = ''
  if (!list.length) {
    doneGuide.classList.add('hidden')
    return
  }
  for (let i = 0; i < list.length; i++) {
    const li = document.createElement('li')
    li.textContent = String(list[i])
    doneGuide.appendChild(li)
  }
  doneGuide.classList.remove('hidden')
}

function accountLabel(acc) {
  if (!acc) return ''
  const name = String(acc.account || '').trim()
  if (name) return name
  return ''
}

function isLoggedIn(acc) {
  if (!acc) return false
  if (acc.logged_in === false) return false
  if (Number(acc.user_id) || 0) return true
  if (String(acc.account || '').trim()) return true
  if (acc.logged_in) return true
  return false
}

function loginChrome() {
  return {
    loginBtn: document.getElementById('loginBtn') || document.querySelector('[data-action="open-login"]'),
    cancelBtn: document.getElementById('cancelLoginBtn') || document.querySelector('[data-action="cancel-login"]'),
    logoutBtn: document.getElementById('logoutBtn') || document.querySelector('[data-action="logout-account"]'),
  }
}

/** 等待浏览器授权时：隐藏登录、显示「取消授权」，并展示倒计时文案 */
function setLoginPendingUI(pending, message) {
  const { loginBtn, cancelBtn, logoutBtn } = loginChrome()
  if (pending) {
    accountBar.textContent = message || '等待浏览器授权…'
    accountBar.classList.remove('is-bound')
    accountBar.title = '请在浏览器完成授权，超时或取消后可重新登录'
    if (loginBtn) loginBtn.hidden = true
    if (cancelBtn) cancelBtn.hidden = false
    if (logoutBtn) logoutBtn.hidden = true
    return
  }
  if (cancelBtn) cancelBtn.hidden = true
}

function renderAccount(acc) {
  lastAccount = acc || null
  if (modalMode === 'error') syncErrorLoginBtn()
  const { loginBtn, cancelBtn, logoutBtn } = loginChrome()
  if (cancelBtn) cancelBtn.hidden = true
  if (isLoggedIn(acc)) {
    const label = accountLabel(acc)
    let text = label ? `已登录 · ${label}` : '已登录'
    if (!acc.has_credentials) text += '（待同步密钥）'
    accountBar.textContent = text
    accountBar.classList.add('is-bound')
    accountBar.title = label || '已登录闲猫'
    idleLead.textContent = acc.has_credentials
      ? '已登录闲猫。在下方选择客户端，点「一键使用」或「打开」。'
      : '已登录闲猫，但本地还没有密钥。请回网站再点一次「一键使用」任意客户端。'
    idleHint.textContent = '配置只会写入闲猫，不会改成其他上游。'
    if (logoutBtn) logoutBtn.hidden = false
    if (loginBtn) loginBtn.hidden = true
    if (acc.has_credentials) startWalletPoll()
    else {
      stopWalletPoll()
      renderWallet(null)
    }
  } else {
    accountBar.textContent = '未登录'
    accountBar.classList.remove('is-bound')
    accountBar.title = '请先登录闲猫'
    idleLead.textContent = '点右上角「浏览器登录」，在网页完成授权；或在网站已登录时点「一键使用」把账号带过来。'
    idleHint.textContent = '登录方式与 Cursor / Claude Code 类似：助手打开浏览器，网页确认后自动回到助手。授权最长等待 2 分钟。'
    if (logoutBtn) logoutBtn.hidden = true
    if (loginBtn) loginBtn.hidden = false
    stopWalletPoll()
    renderWallet(null)
  }
}

function renderPreflight(result) {
  if (!preflightChecks) return
  preflightChecks.innerHTML = ''
  const checks = result?.checks || []
  for (const c of checks) {
    const li = document.createElement('li')
    li.className = `check-item ${c.ok ? 'is-ok' : c.required ? 'is-bad' : 'is-warn'}`
    const mark = document.createElement('span')
    mark.className = 'check-mark'
    mark.textContent = c.ok ? '✓' : c.required ? '✕' : '!'
    const body = document.createElement('div')
    const title = document.createElement('p')
    title.className = 'check-title'
    title.textContent = c.label
    const detail = document.createElement('p')
    detail.className = 'check-detail'
    detail.textContent = c.detail
    body.appendChild(title)
    body.appendChild(detail)
    li.appendChild(mark)
    li.appendChild(body)
    preflightChecks.appendChild(li)
  }

  if (preflightLead) {
    if (result?.ok) {
      preflightLead.textContent = result.installed
        ? `${result.name} 已安装，本机资源满足写入闲猫配置的要求。`
        : `${result.name} 本机资源检测通过，可以开始安装并配置闲猫。`
    } else {
      preflightLead.textContent = `${result?.name || '该客户端'} 本机资源不满足安装要求，请先处理标红项。`
    }
  }

  if (preflightActions) preflightActions.hidden = false
  if (preflightContinueBtn) {
    preflightContinueBtn.disabled = !result?.ok
    preflightContinueBtn.textContent = '一键使用'
    preflightContinueBtn.title = result?.ok ? '' : '请先满足必需要求'
  }
}

function renderAgents(state) {
  agentsState = state || agentsState
  if (!agentList) return
  const { bound, loggedIn, hasCredentials, agents = [], account, userId } = agentsState
  // 登录态以账号关联为准，不能单凭本地密钥（退出后密钥可能残留/被迁移写回）
  const isBound = !!(loggedIn || bound || Number(userId) || String(account || '').trim())
  renderAccount(
    isBound
      ? {
          account: account || '',
          user_id: Number(userId) || 0,
          has_credentials: !!hasCredentials,
          logged_in: true,
        }
      : null,
  )
  if (credHint) {
    credHint.textContent = '配置仅限闲猫，不支持自定义上游'
  }
  agentList.innerHTML = ''
  for (const a of agents) {
    const card = document.createElement('article')
    card.className = `agent-card${a.installed ? '' : ' is-off'}`
    card.dataset.client = a.id

    const top = document.createElement('div')
    top.className = 'agent-top'

    const mark = document.createElement('div')
    mark.className = 'agent-mark'
    mark.setAttribute('aria-hidden', 'true')
    mark.textContent = String(a.name || '?')
      .replace(/\s+/g, '')
      .slice(0, 2)
      .toUpperCase()

    const meta = document.createElement('div')
    meta.className = 'agent-meta'
    const name = document.createElement('p')
    name.className = 'agent-name'
    name.textContent = a.name
    const blurb = document.createElement('p')
    blurb.className = 'agent-blurb'
    blurb.textContent = a.blurb || ''
    meta.appendChild(name)
    meta.appendChild(blurb)

    const badges = document.createElement('div')
    badges.className = 'agent-badges'

    if (a.save_label) {
      const save = document.createElement('span')
      save.className = 'badge badge-save'
      save.textContent = a.save_label
      save.title =
        a.best_mult != null
          ? `当前市场最低约 ${Number(a.best_mult).toFixed(2)}× 官网价`
          : '相对官网价的优惠'
      badges.appendChild(save)
    }

    if (a.open === 'cli') {
      const cliBadge = document.createElement('span')
      cliBadge.className = 'badge badge-cli'
      cliBadge.textContent = '终端版'
      cliBadge.title = '没有图形窗口，会打开黑色终端；配置已由助手写好'
      badges.appendChild(cliBadge)
    }
    const badge = document.createElement('span')
    badge.className = `badge ${a.installed ? 'badge-on' : 'badge-off'}`
    badge.textContent = a.installed ? '已装' : '未装'
    badges.appendChild(badge)

    top.appendChild(mark)
    top.appendChild(meta)
    top.appendChild(badges)

    const actions = document.createElement('div')
    actions.className = 'agent-actions'

    const setupBtn = document.createElement('button')
    setupBtn.type = 'button'
    setupBtn.className = 'btn btn-accent'
    setupBtn.dataset.action = `setup-agent:${a.id}`
    setupBtn.textContent = '一键使用'
    setupBtn.disabled = !isBound || !hasCredentials
    setupBtn.title = !isBound
      ? '请先登录闲猫'
      : !hasCredentials
        ? '请先在网站点一次「一键使用」'
        : a.installed
          ? '自动配置闲猫并打开'
          : '自动安装、配置闲猫并打开'

    const openBtn = document.createElement('button')
    openBtn.type = 'button'
    openBtn.className = 'btn'
    openBtn.dataset.action = `open-agent:${a.id}`
    openBtn.textContent = '打开'
    // 未安装也可点：弹出提示引导「一键使用」，避免灰按钮点了没反应
    openBtn.disabled = !isBound
    openBtn.title = !isBound ? '请先登录闲猫' : a.installed ? '打开客户端' : '尚未安装，点此查看安装提示'

    actions.appendChild(setupBtn)
    actions.appendChild(openBtn)
    card.appendChild(top)
    card.appendChild(actions)
    agentList.appendChild(card)
  }
  if (!agents.length) {
    const empty = document.createElement('p')
    empty.className = 'muted'
    empty.textContent = '暂无可用客户端列表'
    agentList.appendChild(empty)
  }
}

async function refreshAccount() {
  if (!window.xianmaoHelper?.account) {
    renderAccount(null)
    return null
  }
  const acc = await window.xianmaoHelper.account()
  renderAccount(acc)
  return acc
}

async function refreshAgents() {
  await refreshAccount()
  if (!window.xianmaoHelper?.agents) {
    renderAgents({ bound: false, hasCredentials: false, agents: [] })
    return
  }
  const state = await window.xianmaoHelper.agents()
  renderAgents(state)
}

async function startSetupAfterPreflight(client) {
  resetSteps()
  openModal('running', '正在配置')
  statusEl.textContent = '已排队：安装并写入闲猫配置…'
  const r = await window.xianmaoHelper.setupAgent?.(client, { skipPreflight: true })
  if (!r?.ok) {
    openModal('error', '无法开始')
    errMsg.textContent = r?.error || '无法开始配置'
    setHelpVisible('')
  }
}

function preferChannels(channels) {
  const list = Array.isArray(channels) ? channels.slice() : []
  const rank = (c) => (c === 'alipay_f2f' || c === 'wechat_native' ? 0 : 1)
  return list.sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}

function showRechargeForm() {
  const form = document.getElementById('rechargeForm')
  const pay = document.getElementById('rechargePay')
  if (form) form.classList.remove('hidden')
  if (pay) pay.classList.add('hidden')
  const formErr = document.getElementById('rechargeFormErr')
  if (formErr) {
    formErr.textContent = ''
    formErr.classList.add('hidden')
  }
}

function showRechargePay() {
  const form = document.getElementById('rechargeForm')
  const pay = document.getElementById('rechargePay')
  if (form) form.classList.add('hidden')
  if (pay) pay.classList.remove('hidden')
}

function renderChannelList(channels) {
  const box = document.getElementById('rechargeChannels')
  if (!box) return
  box.innerHTML = ''
  const list = preferChannels(channels)
  if (!list.length) {
    box.innerHTML = '<p class="muted">暂无可用的支付方式，请联系平台。</p>'
    selectedChannel = ''
    return
  }
  if (!list.includes(selectedChannel)) selectedChannel = list[0]
  for (const c of list) {
    const label = document.createElement('label')
    label.className = `channel-option${c === selectedChannel ? ' is-on' : ''}`
    label.innerHTML = `<input type="radio" name="rechargeChannel" value="${c}" ${
      c === selectedChannel ? 'checked' : ''
    } /><span><strong>${CHANNEL_LABEL[c] || c}</strong><span>${CHANNEL_HINT[c] || ''}</span></span>`
    const input = label.querySelector('input')
    input.addEventListener('change', () => {
      selectedChannel = c
      box.querySelectorAll('.channel-option').forEach((el) => el.classList.remove('is-on'))
      label.classList.add('is-on')
      const submit = document.getElementById('rechargeSubmitBtn')
      if (submit) submit.textContent = c === 'alipay_page' ? '去支付宝付款' : '下一步'
    })
    box.appendChild(label)
  }
  const submit = document.getElementById('rechargeSubmitBtn')
  if (submit) submit.textContent = selectedChannel === 'alipay_page' ? '去支付宝付款' : '下一步'
}

function formatRemain(expiresAt) {
  if (!expiresAt) return ''
  const s = Math.max(0, Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000))
  return `剩余支付时间 ${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

function renderRechargeOrder(order) {
  rechargeOrder = order || null
  if (!order) return
  showRechargePay()
  const meta = document.getElementById('rechargePayMeta')
  const status = document.getElementById('rechargePayStatus')
  const qrWrap = document.getElementById('rechargeQrWrap')
  const qrImg = document.getElementById('rechargeQrImg')
  const qrHint = document.getElementById('rechargeQrHint')
  const redirectWrap = document.getElementById('rechargeRedirectWrap')
  const success = document.getElementById('rechargeSuccess')
  const payErr = document.getElementById('rechargePayErr')
  const remain = document.getElementById('rechargeRemain')
  const backBtn = document.getElementById('rechargeBackBtn')
  const ch = CHANNEL_LABEL[order.channel] || order.channel || ''
  if (meta) meta.innerHTML = `${ch} · <b>${money(order.amount)}</b>`
  if (status) status.textContent = order.status || 'pending'
  if (payErr) {
    payErr.textContent = ''
    payErr.classList.add('hidden')
  }
  if (success) success.classList.add('hidden')
  if (qrWrap) qrWrap.classList.add('hidden')
  if (redirectWrap) redirectWrap.classList.add('hidden')
  if (remain) remain.textContent = order.status === 'pending' ? formatRemain(order.expires_at) : ''
  if (backBtn) backBtn.classList.toggle('hidden', order.status === 'credited' || order.status === 'crediting')

  if (order.status === 'credited' || order.status === 'crediting') {
    if (success) {
      success.textContent =
        order.status === 'credited' ? '支付成功，余额已到账。' : '支付成功，余额正在到账…'
      success.classList.remove('hidden')
    }
    stopRechargePoll()
    void refreshWallet()
    return
  }
  if (order.status !== 'pending') {
    if (payErr) {
      payErr.textContent = `订单${order.status}${order.close_reason ? `（${order.close_reason}）` : ''}。请重新下单。`
      payErr.classList.remove('hidden')
    }
    stopRechargePoll()
    return
  }

  const payment = order.payment || {}
  if (payment.kind === 'qrcode' && (payment.qr_data_url || payment.qr_code)) {
    if (qrWrap) qrWrap.classList.remove('hidden')
    if (qrImg && payment.qr_data_url) qrImg.src = payment.qr_data_url
    if (qrHint) {
      qrHint.textContent =
        order.channel === 'wechat_native'
          ? '请用微信扫码付款，到账后助手会自动更新余额。'
          : '请用支付宝扫码付款，到账后助手会自动更新余额。'
    }
  } else if (payment.kind === 'redirect' && payment.url) {
    if (redirectWrap) redirectWrap.classList.remove('hidden')
  } else if (payErr) {
    payErr.textContent = '当前支付方式暂无法在助手内展示，请换扫码支付或稍后再试。'
    payErr.classList.remove('hidden')
  }
}

function startRechargePoll() {
  stopRechargePoll()
  rechargeClockTimer = setInterval(() => {
    const remain = document.getElementById('rechargeRemain')
    if (remain && rechargeOrder?.status === 'pending') {
      remain.textContent = formatRemain(rechargeOrder.expires_at)
    }
  }, 1000)
  rechargePollTimer = setInterval(() => {
    void refreshRechargeOrder()
  }, RECHARGE_POLL_MS)
  void refreshRechargeOrder()
}

async function refreshRechargeOrder() {
  if (!rechargeOrder?.id || !window.xianmaoHelper?.getRecharge) return
  try {
    const r = await window.xianmaoHelper.getRecharge(rechargeOrder.id)
    if (r?.ok && r.order) renderRechargeOrder(r.order)
  } catch {
    // 下次再查
  }
}

async function openRechargeModal() {
  if (!isLoggedIn(lastAccount)) {
    openModal('error', '请先登录')
    errMsg.textContent = '登录闲猫后才能充值。'
    setHelpVisible('')
    return
  }
  if (!lastAccount?.has_credentials && !agentsState.hasCredentials) {
    openModal('error', '尚未同步密钥')
    errMsg.textContent = '请回网站再点一次「一键使用」，把密钥同步到助手后再充值。'
    setHelpVisible('')
    return
  }
  openModal('recharge', '充值')
  showRechargeForm()
  rechargeOrder = null
  const amountEl = document.getElementById('rechargeAmount')
  if (amountEl && !amountEl.value) amountEl.value = '100'
  const limitHint = document.getElementById('rechargeLimitHint')
  if (limitHint) limitHint.textContent = '正在加载支付方式…'
  const box = document.getElementById('rechargeChannels')
  if (box) box.innerHTML = ''
  const opts = await window.xianmaoHelper.rechargeOptions?.()
  if (!opts?.ok) {
    if (limitHint) limitHint.textContent = opts?.error || '无法加载支付方式'
    return
  }
  rechargeOptions = {
    channels: opts.channels || [],
    recharge_min: Number(opts.recharge_min) || 1,
    recharge_max: Number(opts.recharge_max) || 50000,
  }
  if (limitHint) {
    limitHint.textContent = `单笔 ${money(rechargeOptions.recharge_min)} – ${money(
      rechargeOptions.recharge_max,
    )}，1 元 = 1 个余额单位。`
  }
  renderChannelList(rechargeOptions.channels)
}

async function submitRecharge() {
  const formErr = document.getElementById('rechargeFormErr')
  const amountEl = document.getElementById('rechargeAmount')
  const raw = String(amountEl?.value || '').trim()
  const amount = Number(raw)
  if (!raw || !Number.isFinite(amount) || amount <= 0) {
    if (formErr) {
      formErr.textContent = '请输入正确的金额（最多两位小数）'
      formErr.classList.remove('hidden')
    }
    return
  }
  if (amount < rechargeOptions.recharge_min || amount > rechargeOptions.recharge_max) {
    if (formErr) {
      formErr.textContent = `单笔充值金额需在 ${money(rechargeOptions.recharge_min)} 到 ${money(
        rechargeOptions.recharge_max,
      )} 之间`
      formErr.classList.remove('hidden')
    }
    return
  }
  if (!selectedChannel) {
    if (formErr) {
      formErr.textContent = '请选择支付方式'
      formErr.classList.remove('hidden')
    }
    return
  }
  const submitBtn = document.getElementById('rechargeSubmitBtn')
  if (submitBtn) submitBtn.disabled = true
  try {
    const r = await window.xianmaoHelper.createRecharge?.(amount, selectedChannel)
    if (!r?.ok) {
      if (formErr) {
        formErr.textContent = r?.error || '创建订单失败'
        formErr.classList.remove('hidden')
      }
      return
    }
    renderRechargeOrder(r.order)
    startRechargePoll()
    const payURL = r.order?.payment?.url
    if (r.order?.payment?.kind === 'redirect' && payURL) {
      await window.xianmaoHelper.openPayURL?.(payURL)
    }
  } finally {
    if (submitBtn) submitBtn.disabled = false
  }
}

async function onAction(action) {
  if (!window.xianmaoHelper) return
  if (action === 'modal-dismiss') {
    if (modalMode === 'running') return
    closeModal()
    await refreshAgents()
    return
  }
  if (action === 'cancel') {
    await window.xianmaoHelper.cancel?.()
    return
  }
  if (action === 'refresh-agents') {
    await refreshAgents()
    return
  }
  if (action === 'open-help') {
    if (lastHelpUrl) await window.xianmaoHelper.openExternal(lastHelpUrl)
    return
  }
  if (action === 'open-site') {
    await refreshSiteLinks()
    await window.xianmaoHelper.openExternal(SITE || 'https://idlicat.com/')
    return
  }
  if (action === 'open-recharge') {
    await openRechargeModal()
    return
  }
  if (action.startsWith('recharge-preset:')) {
    const v = action.slice('recharge-preset:'.length)
    const amountEl = document.getElementById('rechargeAmount')
    if (amountEl) amountEl.value = v
    return
  }
  if (action === 'recharge-submit') {
    await submitRecharge()
    return
  }
  if (action === 'recharge-back') {
    stopRechargePoll()
    rechargeOrder = null
    showRechargeForm()
    if (modalTitle) modalTitle.textContent = '充值'
    return
  }
  if (action === 'recharge-open-pay') {
    const url = rechargeOrder?.payment?.url
    if (url) await window.xianmaoHelper.openPayURL?.(url)
    return
  }
  if (action === 'cancel-login') {
    await window.xianmaoHelper.cancelLogin?.()
    setLoginPendingUI(false)
    renderAccount(lastAccount && isLoggedIn(lastAccount) ? lastAccount : null)
    return
  }
  if (action === 'open-login') {
    setLoginPendingUI(true, '等待浏览器授权… 120s')
    const r = await window.xianmaoHelper.loginAccount?.()
    if (!r?.ok) {
      setLoginPendingUI(false)
      renderAccount(null)
      const errText = String(r?.error || '')
      if (/取消/.test(errText)) {
        accountBar.textContent = '未登录'
        return
      }
      if (/超时|过期/.test(errText)) {
        accountBar.textContent = '授权已超时，请重新登录'
        return
      }
      if (errText) {
        openModal('error', '登录失败')
        errMsg.textContent = errText
        setHelpVisible('')
        if (errHelpBtn) {
          errHelpBtn.classList.remove('hidden')
          errHelpBtn.textContent = '打开闲猫网站'
          errHelpBtn.dataset.action = 'open-site'
        }
      }
    }
    await refreshAgents()
    return
  }
  if (action === 'logout-account') {
    await window.xianmaoHelper.cancelLogin?.()
    await window.xianmaoHelper.logoutAccount()
    closeModal()
    await refreshAgents()
    return
  }
  if (action === 'preflight-continue') {
    const client = pendingPreflightClient
    if (!client) return
    await startSetupAfterPreflight(client)
    return
  }
  if (action.startsWith('setup-agent:')) {
    const client = action.slice('setup-agent:'.length)
    lastClient = client
    pendingPreflightClient = client
    openModal('preflight', '安装前检测')
    if (preflightLead) preflightLead.textContent = '正在检测本机资源是否满足安装需要…'
    if (preflightChecks) preflightChecks.innerHTML = ''
    if (preflightActions) preflightActions.hidden = true

    const pf = await window.xianmaoHelper.preflight?.(client)
    if (!pf || pf.error) {
      openModal('error', '检测失败')
      errMsg.textContent = pf?.error || '无法检测本机资源'
      setHelpVisible('')
      return
    }
    renderPreflight(pf)
    return
  }
  if (action.startsWith('open-agent:')) {
    const client = action.slice('open-agent:'.length)
    lastClient = client
    const agent = (agentsState.agents || []).find((a) => a.id === client)
    if (agent && !agent.installed) {
      openModal('error', '尚未安装')
      errMsg.textContent = `还没有安装 ${agent.name || '该客户端'}。请点「一键使用」自动安装并配置闲猫。`
      setHelpVisible('')
      // 提供一键跳到安装
      pendingPreflightClient = client
      if (errHelpBtn) {
        errHelpBtn.classList.remove('hidden')
        errHelpBtn.textContent = '去一键使用'
        errHelpBtn.dataset.action = `setup-agent:${client}`
      }
      return
    }
    const r = await window.xianmaoHelper.openAgent?.(client)
    if (!r?.ok) {
      const needInstall = /未安装|请先/.test(String(r?.error || ''))
      openModal('error', needInstall ? '尚未安装' : '无法打开')
      errMsg.textContent =
        r?.error ||
        (needInstall
          ? '还没有安装该客户端。请点「一键使用」自动安装并配置闲猫。'
          : '无法打开')
      setHelpVisible('')
      if (needInstall && errHelpBtn) {
        errHelpBtn.classList.remove('hidden')
        errHelpBtn.textContent = '去一键使用'
        errHelpBtn.dataset.action = `setup-agent:${client}`
      }
    }
  }
}

document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-action]')
  if (!btn || btn.disabled) return
  void onAction(btn.getAttribute('data-action'))
})

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && modalMode && modalMode !== 'running') {
    void onAction('modal-dismiss')
  }
})

if (window.xianmaoHelper) {
  window.xianmaoHelper.onProgress((payload) => {
    if (!payload) return
    // 浏览器授权 / 会话同步：只刷新顶栏，不开安装弹窗
    if (payload.sessionSync || payload.step === 'session') {
      if (payload.loginPending) {
        setLoginPendingUI(true, payload.message || '等待浏览器授权…')
        return
      }
      if (payload.account) {
        renderAccount({
          ...payload.account,
          has_credentials: !!payload.account.has_credentials,
          logged_in: true,
        })
      } else if (payload.status === 'error') {
        setLoginPendingUI(false)
        renderAccount(payload.account && isLoggedIn(payload.account) ? payload.account : null)
        const msg = String(payload.message || '')
        if (/取消|超时|过期/.test(msg)) {
          if (/超时|过期/.test(msg)) accountBar.textContent = '授权已超时，请重新登录'
        } else if (msg) {
          openModal('error', '登录失败')
          errMsg.textContent = msg
          setHelpVisible('')
          if (errHelpBtn) {
            errHelpBtn.classList.remove('hidden')
            errHelpBtn.textContent = '打开闲猫网站'
            errHelpBtn.dataset.action = 'open-site'
          }
        }
      } else {
        setLoginPendingUI(false)
        renderAccount(payload.account && isLoggedIn(payload.account) ? payload.account : null)
      }
      void refreshAgents()
      return
    }
    if (payload.account) renderAccount({ ...payload.account, has_credentials: true })
    if (payload.client) lastClient = payload.client
    if (payload.openCli) lastOpenCli = payload.openCli
    const nextHelp =
      payload.helpUrl ||
      (Array.isArray(payload.helpUrls) && payload.helpUrls[0]) ||
      ''
    if (payload.step === 'exchange' && payload.status === 'active') {
      lastHelpUrl = ''
      setHelpVisible('')
      resetSteps()
    }
    if (payload.step === 'error' || payload.status === 'error') {
      if (payload.cancelled) {
        lastHelpUrl = ''
        openModal('error', '已停止')
        errMsg.textContent = payload.message || '已停止'
        setHelpVisible('')
        void refreshAgents()
        return
      }
      lastHelpUrl = nextHelp || lastHelpUrl
      openModal('error', '出错了')
      errMsg.textContent = payload.message || '操作失败'
      setHelpVisible(lastHelpUrl)
      void refreshAgents()
      return
    }
    if (!payload.done) {
      openModal('running', '正在配置')
      if (payload.step) setStep(payload.step, payload.status || 'active')
      if (payload.message) statusEl.textContent = payload.message
      return
    }
    lastHelpUrl = nextHelp || ''
    if (payload.step) setStep(payload.step, 'done')
    const isCliDone = payload.openedKind === 'cli' || payload.openMode === 'cli' || !!payload.cliGuide
    openModal('done', isCliDone ? '终端已就绪' : '完成')
    doneMsg.textContent =
      payload.message || (isCliDone ? '已配好，请看黑色终端窗口' : '搞定了，已经配好闲猫。')
    if (doneHint) {
      doneHint.textContent = isCliDone
        ? '这是终端版：没有图形窗口，按下面步骤用即可。'
        : payload.openedKind
          ? '客户端应该已经打开。如果没有，点下方再打开一次。'
          : lastHelpUrl
            ? '还差一步，点「打开安装说明」继续。'
            : '如果客户端没打开，点下方按钮。'
    }
    renderDoneGuide(isCliDone ? payload.cliGuide : null)
    renderDonePrimary(lastClient || payload.client || '', { cli: isCliDone })
    setHelpVisible(lastHelpUrl)
    void refreshAgents()
  })
}

void refreshSiteLinks().then(() => refreshAgents())
