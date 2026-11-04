import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'

const { list, create, update, getAllWithCount, showError } = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), getAllWithCount: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { list, create, update, getAllWithCount } } }))
const { getEngineProxySharing, setEngineProxySharing, testEngineProxy } = vi.hoisted(() => ({ getEngineProxySharing: vi.fn(), setEngineProxySharing: vi.fn(), testEngineProxy: vi.fn() }))
vi.mock('@/market-admin/api', () => ({
  getEngineProxySharing,
  setEngineProxySharing,
  testEngineProxy,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /></div>' },
    Select: {
      props: ['modelValue', 'options'],
      emits: ['update:modelValue'],
      template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
    },
    DataTable: { props: ['data', 'columns', 'loading'], emits: ['sort'], template: '<div><div v-for="row in data" :key="row.id" :data-proxy-id="row.id"><slot name="cell-market_sharing" :row="row" /><slot name="cell-actions" :row="row" /></div></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  } },
})
let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  vi.resetAllMocks()
  list.mockResolvedValue({ items: [{ id: 9, name: 'proxy', protocol: 'http', host: 'proxy.example', port: 8080, username: 'old-user', status: 'active' }], total: 1, pages: 1 })
  getAllWithCount.mockResolvedValue([])
  update.mockResolvedValue({})
  create.mockResolvedValue({ id: 10, name: 'new proxy', protocol: 'http', host: 'proxy.example', port: 8080, status: 'active' })
  getEngineProxySharing.mockResolvedValue([])
  setEngineProxySharing.mockResolvedValue({ proxy_id: 10, is_shared: true })
})
afterEach(() => wrapper?.unmount())
async function edit() {
  wrapper = mountView(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
}
async function submit() {
  await wrapper.get('#edit-proxy-form').trigger('submit'); await flushPromises()
  expect(update).toHaveBeenCalledTimes(1)
  return update.mock.calls[0][1]
}

describe('proxy credential updates', () => {
  it('sends an explicit empty username when cleared', async () => {
    await edit()
    const username = wrapper.findAll<HTMLInputElement>('#edit-proxy-form input').find(input => input.element.value === 'old-user')!
    await username.setValue('')
    const payload = await submit()
    expect(payload.username).toBe('')
    expect(payload).not.toHaveProperty('password')
  })

  it('sends an explicit empty password when the user clears the field', async () => {
    await edit()
    await wrapper.get('#edit-proxy-form input[type="password"]').setValue('')
    expect((await submit()).password).toBe('')
  })

  it('omits an untouched password when saving other settings', async () => {
    await edit()
    const payload = await submit()
    expect(payload.username).toBe('old-user')
    expect(payload).not.toHaveProperty('password')
  })

  it('keeps trimming a replacement password', async () => {
    await edit()
    await wrapper.get('#edit-proxy-form input[type="password"]').setValue(' new-password ')
    expect((await submit()).password).toBe('new-password')
  })
})

describe('IdleCat proxy sharing controls', () => {
  async function openCreate() {
    wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.createProxy')!.trigger('click')
    await flushPromises()
    await wrapper.get('input[placeholder="admin.proxies.enterProxyName"]').setValue('new proxy')
    await wrapper.get('input[placeholder="admin.proxies.form.hostPlaceholder"]').setValue('proxy.example')
    await wrapper.get('input[placeholder="admin.proxies.form.portPlaceholder"]').setValue('8080')
    const checkbox = wrapper.findAll('input[type="checkbox"]').find((input) =>
      input.element.parentElement?.textContent?.includes('marketSharingLabel'),
    )!
    await checkbox.setValue(true)
  }

  it('creates the engine proxy first, then enables sharing with its returned ID', async () => {
    await openCreate()
    await wrapper.get('#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledOnce()
    expect(setEngineProxySharing).toHaveBeenCalledWith(10, true)
  })

  it('does not repeat creation when enabling sharing fails and opens the new proxy editor', async () => {
    setEngineProxySharing.mockRejectedValueOnce(new Error('active binding'))
    await openCreate()
    await wrapper.get('#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledOnce()
    expect(showError).toHaveBeenCalledWith(expect.stringContaining('marketSharingEnableFailed'))
    expect(wrapper.find('#edit-proxy-form').exists()).toBe(true)
  })

  it('hydrates sharing from the mapping and cancels sharing before updating native fields', async () => {
    getEngineProxySharing.mockResolvedValue([{ proxy_id: 9, is_shared: true }])
    setEngineProxySharing.mockResolvedValue({ proxy_id: 9, is_shared: false })
    await edit()
    const checkbox = wrapper.get<HTMLInputElement>('#edit-proxy-form input[type="checkbox"]')
    expect(checkbox.element.checked).toBe(true)
    await checkbox.setValue(false)
    await submit()
    expect(setEngineProxySharing).toHaveBeenCalledWith(9, false)
    expect(setEngineProxySharing.mock.invocationCallOrder[0]).toBeLessThan(update.mock.invocationCallOrder[0]!)
  })

  it('keeps native configuration untouched when cancellation is blocked by a market binding', async () => {
    getEngineProxySharing.mockResolvedValue([{ proxy_id: 9, is_shared: true }])
    setEngineProxySharing.mockRejectedValueOnce(new Error('active upstreams'))
    await edit()
    await wrapper.get('#edit-proxy-form input[type="checkbox"]').setValue(false)
    await wrapper.get('#edit-proxy-form').trigger('submit')
    await flushPromises()
    expect(update).not.toHaveBeenCalled()
    expect(wrapper.find('#edit-proxy-form').exists()).toBe(true)
  })

  it('cancels the market share before setting an existing proxy inactive', async () => {
    getEngineProxySharing.mockResolvedValue([{ proxy_id: 9, is_shared: true }])
    setEngineProxySharing.mockResolvedValue({ proxy_id: 9, is_shared: false })
    await edit()
    await wrapper.get('#edit-proxy-form input[type="checkbox"]').setValue(false)
    const statusSelect = wrapper.findAll<HTMLSelectElement>('#edit-proxy-form select').find(select =>
      Array.from(select.element.options).some(option => option.value === 'inactive'),
    )!
    await statusSelect.setValue('inactive')
    await wrapper.get('#edit-proxy-form').trigger('submit')
    await flushPromises()

    expect(setEngineProxySharing).toHaveBeenCalledWith(9, false)
    expect(update).toHaveBeenCalledWith(9, expect.objectContaining({ status: 'inactive' }))
  })

  it('persists connectivity checks through the market relay test', async () => {
    const result = { proxy_id: 9, is_shared: false, connection_report: { success: false, message: 'unreachable' } }
    let resolveTest!: (value: typeof result) => void
    testEngineProxy.mockReturnValueOnce(new Promise<typeof result>(resolve => { resolveTest = resolve }))
    wrapper = mountView()
    await flushPromises()
    const row = wrapper.get('[data-proxy-id="9"]')
    await row.findAll('button').find(button => button.text() === 'admin.proxies.testConnection')!.trigger('click')
    expect(testEngineProxy).toHaveBeenCalledOnce()
    expect(testEngineProxy).toHaveBeenCalledWith(9)
    expect(showError).not.toHaveBeenCalled()

    resolveTest(result)
    await vi.waitFor(() => expect(showError).toHaveBeenCalledWith('unreachable'))
  })
})
