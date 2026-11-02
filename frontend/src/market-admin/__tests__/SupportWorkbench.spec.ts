import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SupportWorkbench from '../SupportWorkbench.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const PREFIX = '/api/v1/admin/market/support'

type Conversation = {
  id: number
  user_id: number
  user_email: string
  status: string
  last_message_at: string
  last_preview: string
  last_sender: string
  user_unread_count: number
  staff_unread_count: number
  awaiting_staff_since?: string | null
  user_last_read_msg_id?: number
  overdue?: boolean
}

type Message = {
  id: number
  conversation_id: number
  sender: string
  body: string
  created_at: string
}

type ThreadBundle = {
  conversation: Conversation
  messages: Message[]
}

function conversation(id: number, overrides: Partial<Conversation> = {}): Conversation {
  return {
    id,
    user_id: id + 100,
    user_email: `user${id}@example.com`,
    status: 'open',
    last_message_at: '2026-11-01T08:00:00Z',
    last_preview: `会话 ${id} 预览`,
    last_sender: 'user',
    user_unread_count: 0,
    staff_unread_count: 0,
    user_last_read_msg_id: 0,
    ...overrides,
  }
}

function message(id: number, conversationId: number, body: string, sender = 'user'): Message {
  return {
    id,
    conversation_id: conversationId,
    sender,
    body,
    created_at: '2026-11-01T08:00:00Z',
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

let conversations: Conversation[]
let threadMessages: Record<number, Message[]>
let alerts: {
  pending_count: number
  overdue_count: number
  staff_unread_total: number
  pending_alert_threshold: number
  reply_timeout_minutes: number
  pending_alert_fired: boolean
  overdue_conversations: Conversation[]
}

function bundle(id: number): ThreadBundle {
  return {
    conversation: conversation(id),
    messages: structuredClone(threadMessages[id] ?? []),
  }
}

function defaultGet(path: string): Promise<unknown> {
  if (path === `${PREFIX}/alerts`) return Promise.resolve(alerts)
  if (path.startsWith(`${PREFIX}/alerts?`)) return new Promise(() => undefined)
  if (path.startsWith(`${PREFIX}/conversations?`)) {
    return Promise.resolve({ items: structuredClone(conversations), total: conversations.length })
  }
  const match = path.match(/\/conversations\/(\d+)/)
  if (match) {
    if (path.includes('wait_ms=')) return new Promise(() => undefined)
    return Promise.resolve(bundle(Number(match[1])))
  }
  return Promise.reject(new Error(`unexpected GET ${path}`))
}

async function mountPage() {
  const wrapper = mount(SupportWorkbench, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

async function clickConversation(wrapper: VueWrapper, selector: string, id: number) {
  const target = wrapper
    .findAll(`${selector} button`)
    .find((button) => button.text().includes(`user${id}@example.com`))
  expect(target, `找不到会话 ${id}`).toBeDefined()
  await target!.trigger('click')
  await flushPromises()
}

function messageRequests() {
  return api.send.mock.calls.filter(([, path]) => String(path).endsWith('/messages'))
}

function currentDialog() {
  const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]')
  return dialogs[dialogs.length - 1]
}

beforeEach(() => {
  vi.clearAllMocks()
  conversations = [conversation(1), conversation(2)]
  threadMessages = {
    1: [message(11, 1, '会话一消息')],
    2: [message(22, 2, '会话二消息')],
  }
  alerts = {
    pending_count: 2,
    overdue_count: 0,
    staff_unread_total: 0,
    pending_alert_threshold: 5,
    reply_timeout_minutes: 15,
    pending_alert_fired: false,
    overdue_conversations: [],
  }
  api.get.mockImplementation(defaultGet)
  api.send.mockImplementation(async (method: string, path: string, body?: { body?: string }) => {
    if (path.endsWith('/messages')) {
      const id = Number(path.match(/\/conversations\/(\d+)/)?.[1])
      return message(1000 + id, id, body?.body ?? '', 'staff')
    }
    return {}
  })
})

enableAutoUnmount(afterEach)

describe('客服工作台竞态与草稿', () => {
  it('右侧超时会话不依赖当前筛选列表也能显示完整线程', async () => {
    conversations = [conversation(1)]
    alerts.overdue_count = 1
    alerts.overdue_conversations = [conversation(99, { overdue: true })]
    threadMessages[99] = [message(99, 99, '筛选外超时消息')]
    const wrapper = await mountPage()

    await clickConversation(wrapper, '.sw-queue-overdue', 99)

    expect(wrapper.get('.sw-detail-head').text()).toContain('user99@example.com')
    expect(wrapper.get('.sw-msgs').text()).toContain('筛选外超时消息')
  })

  it('快速切换会话时清空旧消息并忽略迟到的线程响应', async () => {
    const wrapper = await mountPage()
    const slowTwo = deferred<ThreadBundle>()
    const fastOne = deferred<ThreadBundle>()
    api.get.mockImplementation((path: string) => {
      if (path === `${PREFIX}/conversations/2`) return slowTwo.promise
      if (path === `${PREFIX}/conversations/1`) return fastOne.promise
      return defaultGet(path)
    })

    const pendingSwitch = clickConversation(wrapper, '.sw-cards', 2)
    await flushPromises()
    expect(wrapper.get('.sw-msgs').text()).not.toContain('会话一消息')
    const firstSwitch = clickConversation(wrapper, '.sw-cards', 1)
    fastOne.resolve({ conversation: conversation(1), messages: [message(111, 1, '当前会话消息')] })
    await firstSwitch
    slowTwo.resolve({ conversation: conversation(2), messages: [message(222, 2, '迟到会话消息')] })
    await pendingSwitch
    await flushPromises()

    expect(wrapper.get('.sw-detail-head').text()).toContain('user1@example.com')
    expect(wrapper.get('.sw-msgs').text()).toContain('当前会话消息')
    expect(wrapper.get('.sw-msgs').text()).not.toContain('迟到会话消息')
  })

  it('切换会话会取消旧线程请求，失败与空消息状态彼此区分', async () => {
    const wrapper = await mountPage()
    const pendingTwo = deferred<ThreadBundle>()
    let twoSignal: AbortSignal | undefined
    api.get.mockImplementation((path: string, options?: { signal?: AbortSignal }) => {
      if (path === `${PREFIX}/conversations/2`) {
        twoSignal = options?.signal
        return pendingTwo.promise
      }
      return defaultGet(path)
    })

    const switching = clickConversation(wrapper, '.sw-cards', 2)
    await flushPromises()
    expect(wrapper.get('.sw-msgs').get('[role="status"]').text()).toBe('加载中…')
    await clickConversation(wrapper, '.sw-cards', 1)
    expect(twoSignal?.aborted).toBe(true)
    pendingTwo.reject(new Error('旧请求失败'))
    await switching
    expect(wrapper.get('.sw-msgs').text()).toContain('会话一消息')
    expect(wrapper.get('.sw-msgs').text()).not.toContain('旧请求失败')

    api.get.mockImplementation(defaultGet)
    threadMessages[2] = []
    await clickConversation(wrapper, '.sw-cards', 2)
    expect(wrapper.get('.sw-msgs').text()).toContain('暂无消息')
    api.get.mockImplementation((path: string) => {
      if (path === `${PREFIX}/conversations/2`) return Promise.reject(new Error('线程加载失败'))
      return defaultGet(path)
    })
    await clickConversation(wrapper, '.sw-cards', 2)
    expect(wrapper.get('.sw-msgs').get('[role="alert"]').text()).toContain('线程加载失败')
    expect(wrapper.get('.sw-msgs').text()).not.toContain('暂无消息')
  })

  it('发送途中切换会话不会串消息、清错草稿或更新错的列表项', async () => {
    const wrapper = await mountPage()
    const sendingOne = deferred<Message>()
    api.send.mockImplementation((method: string, path: string) => {
      if (path === `${PREFIX}/conversations/1/messages`) return sendingOne.promise
      return Promise.resolve({})
    })
    await wrapper.get('textarea').setValue('会话一草稿')
    await wrapper.get('.sw-compose button').trigger('click')
    await flushPromises()
    expect(messageRequests()).toHaveLength(1)

    await clickConversation(wrapper, '.sw-cards', 2)
    await wrapper.get('textarea').setValue('会话二新输入')
    sendingOne.resolve(message(101, 1, '会话一已发送', 'staff'))
    await flushPromises()

    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('会话二新输入')
    expect(wrapper.get('.sw-msgs').text()).toContain('会话二消息')
    expect(wrapper.get('.sw-msgs').text()).not.toContain('会话一已发送')
    const rows = wrapper.findAll('.sw-cards li')
    expect(rows[0]!.text()).toContain('会话一草稿')
    expect(rows[1]!.text()).not.toContain('会话一草稿')
  })

  it('同一会话发送期间继续输入时保留新内容', async () => {
    const wrapper = await mountPage()
    const pendingSend = deferred<Message>()
    api.send.mockImplementation((method: string, path: string) => {
      if (path.endsWith('/messages')) return pendingSend.promise
      return Promise.resolve({})
    })
    const textarea = wrapper.get<HTMLTextAreaElement>('textarea')
    await textarea.setValue('首段内容')
    await wrapper.get('.sw-compose button').trigger('click')
    await flushPromises()
    await textarea.setValue('首段内容，发送中又补充了一段')

    pendingSend.resolve(message(101, 1, '首段内容', 'staff'))
    await flushPromises()

    expect(textarea.element.value).toBe('首段内容，发送中又补充了一段')
  })

  it('发送失败保留当前草稿并显示错误', async () => {
    const wrapper = await mountPage()
    api.send.mockImplementation((method: string, path: string) => {
      if (path.endsWith('/messages')) return Promise.reject(new Error('网络发送失败'))
      return Promise.resolve({})
    })
    await wrapper.get('textarea').setValue('不能丢失的草稿')

    await wrapper.get('.sw-compose button').trigger('click')
    await flushPromises()

    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('不能丢失的草稿')
    expect(wrapper.get('[role="alert"]').text()).toContain('网络发送失败')
  })

  it('每个会话独立保留未发送草稿', async () => {
    const wrapper = await mountPage()
    await wrapper.get('textarea').setValue('会话一未发送')

    await clickConversation(wrapper, '.sw-cards', 2)
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    await wrapper.get('textarea').setValue('会话二未发送')
    await clickConversation(wrapper, '.sw-cards', 1)
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('会话一未发送')
    await clickConversation(wrapper, '.sw-cards', 2)
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('会话二未发送')
  })

  it('IME 组合输入期间 Enter 不发送，组合结束后才发送', async () => {
    const wrapper = await mountPage()
    const textarea = wrapper.get('textarea')
    await textarea.setValue('中文草稿')

    await textarea.trigger('keydown', { key: 'Enter', isComposing: true })
    await flushPromises()
    expect(messageRequests()).toHaveLength(0)

    await textarea.trigger('keydown', { key: 'Enter', isComposing: false })
    await flushPromises()
    expect(messageRequests()).toHaveLength(1)
  })

  it('远离底部时只提示新增消息，跳到最新后才清除提示', async () => {
    const polling = deferred<ThreadBundle>()
    let firstPoll = true
    api.get.mockImplementation((path: string) => {
      if (path.includes('/conversations/1?') && path.includes('wait_ms=')) {
        if (firstPoll) {
          firstPoll = false
          return polling.promise
        }
        return new Promise(() => undefined)
      }
      return defaultGet(path)
    })
    const wrapper = await mountPage()
    await flushPromises()
    const messagesEl = wrapper.get<HTMLElement>('.sw-msgs')
    Object.defineProperties(messagesEl.element, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, writable: true, value: 100 },
    })

    polling.resolve({
      conversation: conversation(1),
      messages: [message(12, 1, '远离底部的新消息')],
    })
    await flushPromises()

    expect(messagesEl.element.scrollTop).toBe(100)
    const jump = wrapper.get('.sw-new-messages button')
    expect(jump.text()).toContain('新消息')
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('收到 1 条新消息')
    await jump.trigger('click')
    expect(messagesEl.element.scrollTop).toBe(1000)
    expect(wrapper.find('.sw-new-messages').exists()).toBe(false)
  })

  it('接近底部时新增消息自动跟随，不显示跳转提示', async () => {
    const polling = deferred<ThreadBundle>()
    let firstPoll = true
    api.get.mockImplementation((path: string) => {
      if (path.includes('/conversations/1?') && path.includes('wait_ms=')) {
        if (firstPoll) {
          firstPoll = false
          return polling.promise
        }
        return new Promise(() => undefined)
      }
      return defaultGet(path)
    })
    const wrapper = await mountPage()
    await flushPromises()
    const messagesEl = wrapper.get<HTMLElement>('.sw-msgs')
    Object.defineProperties(messagesEl.element, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, writable: true, value: 650 },
    })

    polling.resolve({
      conversation: conversation(1),
      messages: [message(12, 1, '接近底部的新消息')],
    })
    await flushPromises()

    expect(messagesEl.element.scrollTop).toBe(1000)
    expect(wrapper.find('.sw-new-messages').exists()).toBe(false)
  })

  it('会话、超时项、页签和输入框提供键盘及可访问名称', async () => {
    alerts.overdue_count = 1
    alerts.overdue_conversations = [conversation(1, { overdue: true })]
    const wrapper = await mountPage()

    expect(wrapper.find('.sw-cards li button[aria-current="true"]').exists()).toBe(true)
    expect(wrapper.find('.sw-queue-mini[aria-current="true"]').exists()).toBe(true)
    for (const tab of [...wrapper.findAll('.sw-queue-item'), ...wrapper.findAll('.sw-detail-tabs button')]) {
      expect(tab.attributes('aria-pressed')).toBeDefined()
    }
    expect(wrapper.get('.sw-msgs').attributes('tabindex')).toBe('0')
    expect(wrapper.get('input[type="search"]').attributes('aria-label')).toBeTruthy()
    expect(wrapper.get('textarea').attributes('aria-label')).toBeTruthy()
  })

  it('取消结束会话时不发送关闭请求，确认操作使用危险样式', async () => {
    const wrapper = await mountPage()
    await wrapper.get('.sw-close-conversation').trigger('click')
    await flushPromises()

    const dialog = currentDialog()
    expect(dialog?.textContent).toContain('user1@example.com')
    const buttons = Array.from(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? [])
    const confirm = buttons.find((button) => button.textContent?.trim() === '确认结束')
    const cancel = buttons.find((button) => button.textContent?.trim() === '取消')
    expect(confirm?.className).toContain('bg-red-600')
    expect(cancel).toBeDefined()
    cancel!.click()
    await flushPromises()

    expect(api.send.mock.calls.some(([, path]) => String(path).endsWith('/close'))).toBe(false)
  })

  it('结束会话期间展示 busy 并阻止重复请求', async () => {
    const closing = deferred<Record<string, never>>()
    api.send.mockImplementation((method: string, path: string) => {
      if (path.endsWith('/close')) return closing.promise
      return Promise.resolve({})
    })
    const wrapper = await mountPage()
    await wrapper.get('.sw-close-conversation').trigger('click')
    await flushPromises()

    const dialog = currentDialog()
    const confirm = Array.from(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? []).find(
      (button) => button.textContent?.trim() === '确认结束',
    )
    expect(confirm).toBeDefined()
    confirm!.click()
    await flushPromises()

    const closeRequests = () => api.send.mock.calls.filter(([, path]) => String(path).endsWith('/close'))
    expect(closeRequests()).toHaveLength(1)
    expect(confirm!.textContent).toContain('结束中')
    expect(confirm!.disabled).toBe(true)
    confirm!.click()
    await flushPromises()
    expect(closeRequests()).toHaveLength(1)

    closing.resolve({})
    await flushPromises()
  })
})
