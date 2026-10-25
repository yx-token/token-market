import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountTestModal from '../AccountTestModal.vue'
import AdminAccountTestModal from '@/components/admin/account/AccountTestModal.vue'

const { getAvailableModelsMock } = vi.hoisted(() => ({
  getAvailableModelsMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels: getAvailableModelsMock
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: { type: [String, Number, Boolean, null], default: '' },
    options: { type: Array, default: () => [] },
    valueKey: { type: String, default: 'value' },
    labelKey: { type: String, default: 'label' }
  },
  emits: ['update:modelValue'],
  template: `
    <select
      v-bind="$attrs"
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option
        v-for="option in options"
        :key="option[valueKey]"
        :value="option[valueKey]"
      >
        {{ option[labelKey] }}
      </option>
    </select>
  `
})

const TextAreaStub = defineComponent({
  name: 'TextArea',
  props: {
    modelValue: { type: String, default: '' }
  },
  emits: ['update:modelValue'],
  template: `
    <textarea
      v-bind="$attrs"
      :value="modelValue"
      @input="$emit('update:modelValue', $event.target.value)"
    />
  `
})

function buildAccount() {
  return {
    id: 1,
    name: 'OpenAI OAuth',
    platform: 'openai',
    type: 'oauth',
    status: 'active',
    credentials: {},
    extra: {},
    concurrency: 1,
    priority: 1,
    proxy_id: null,
    auto_pause_on_expired: false
  } as any
}

describe('AccountTestModal', () => {
  const originalFetch = global.fetch

  beforeEach(() => {
    getAvailableModelsMock.mockReset()
    getAvailableModelsMock.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: vi.fn().mockResolvedValue({ done: true, value: undefined })
        })
      }
    } as any)
    localStorage.setItem('auth_token', 'test-token')
  })

  afterEach(() => {
    global.fetch = originalFetch
    localStorage.clear()
  })

  it('posts compact mode for OpenAI compact probe', async () => {
    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount()
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body)).toMatchObject({
      model_id: 'gpt-5.4',
      mode: 'compact'
    })
  })

  it('renders Chat Completions path status from test SSE', async () => {
    const encoder = new TextEncoder()
    const chunks = [
      encoder.encode('data: {"type":"status","text":"已通过 /v1/chat/completions 验证"}\n\n'),
      encoder.encode('data: {"type":"test_complete","success":true}\n\n')
    ]
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: vi.fn().mockImplementation(() => Promise.resolve(
            chunks.length > 0
              ? { done: false, value: chunks.shift() }
              : { done: true, value: undefined }
          ))
        })
      }
    } as any)

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount()
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(wrapper.text()).toContain('已通过 /v1/chat/completions 验证')
  })
})

describe.each([
  ['account', AccountTestModal],
  ['admin account', AdminAccountTestModal]
] as const)('%s test model selection', (_name, component) => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    getAvailableModelsMock.mockReset()
    getAvailableModelsMock.mockResolvedValue([
      { id: 'native-first', display_name: 'First Model' },
      { id: 'custom-sonnet', display_name: 'Sonnet Alias' },
      { id: 'native-selected', display_name: 'Selected Model' }
    ])
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: { getReader: () => ({ read: vi.fn().mockResolvedValue({ done: true }) }) }
    })
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  const openModal = async (platform = 'kimi') => {
    const wrapper = mount(component, {
      props: { show: false, account: { ...buildAccount(), platform } },
      global: {
        stubs: { BaseDialog: BaseDialogStub, Select: SelectStub, TextArea: TextAreaStub, Icon: true }
      }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    return wrapper
  }

  it.each(['kimi', 'zhipu', 'deepseek', 'minimax', 'openai', 'gemini', 'antigravity'])(
    'selects the first model for %s instead of a Sonnet lookalike', async (platform) => {
      const wrapper = await openModal(platform)
      expect(wrapper.get('select').element.value).toBe('native-first')
      wrapper.unmount()
    }
  )

  it('keeps Sonnet as the Anthropic default', async () => {
    const wrapper = await openModal('anthropic')
    expect(wrapper.get('select').element.value).toBe('custom-sonnet')
    wrapper.unmount()
  })

  it.each(['kimi', 'zhipu'])('sends the selected %s model in the test request', async (platform) => {
    const wrapper = await openModal(platform)
    await wrapper.get('select').setValue('native-selected')
    const startButton = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!
    await startButton.trigger('click')
    await flushPromises()
    expect(globalThis.fetch).toHaveBeenCalledTimes(1)
    const [url, options] = vi.mocked(globalThis.fetch).mock.calls[0]
    expect(String(url)).toContain('/admin/accounts/1/test')
    expect(JSON.parse(String(options?.body))).toMatchObject({ model_id: 'native-selected' })
    wrapper.unmount()
  })

  it('explains an empty model list and disables the test', async () => {
    getAvailableModelsMock.mockResolvedValue([])
    const wrapper = await openModal()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.noAvailableTestModels')
    const startButton = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!
    expect(startButton.attributes('disabled')).toBeDefined()
    expect(wrapper.get('select').element.value).toBe('')
    expect(globalThis.fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('explains a model loading failure', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getAvailableModelsMock.mockRejectedValue(new Error('unavailable'))
    const wrapper = await openModal()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.failedToLoadTestModels')
    wrapper.unmount()
    consoleError.mockRestore()
  })
})
