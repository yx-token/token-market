import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ConfirmDialog from '../ConfirmDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: Array<ReturnType<typeof mount>> = []

function mountConfirm(props: Record<string, unknown>) {
  const wrapper = mount(ConfirmDialog, {
    attachTo: document.body,
    props: { show: true, title: 'Confirm', message: 'Continue?', ...props },
    global: { stubs: { Icon: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}

function footerButtons() {
  return Array.from(document.body.querySelectorAll<HTMLButtonElement>('.modal-footer button'))
}

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount()
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('ConfirmDialog', () => {
  it('preserves the existing no-payload confirm and cancel contract', async () => {
    const wrapper = mountConfirm({ confirmText: 'Delete', cancelText: 'Back' })
    await nextTick()
    const [cancel, confirm] = footerButtons()

    cancel.click()
    confirm.click()

    expect(wrapper.emitted('cancel')).toEqual([[]])
    expect(wrapper.emitted('confirm')).toEqual([[]])
  })

  it('associates the reason label and emits the entered reason', async () => {
    const wrapper = mountConfirm({
      show: false,
      reason: true,
      reasonLabel: '删除原因',
      reasonInitialValue: 'old'
    })
    await wrapper.setProps({ show: true })
    await nextTick()

    const textarea = document.body.querySelector<HTMLTextAreaElement>('.modal-body textarea')!
    const label = document.body.querySelector<HTMLLabelElement>(`label[for="${textarea.id}"]`)!
    expect(label.textContent).toBe('删除原因')

    textarea.value = '  duplicate review  '
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    footerButtons()[1].click()
    expect(wrapper.emitted('confirm')).toEqual([['  duplicate review  ']])

    await wrapper.setProps({ show: false, reasonInitialValue: 'fresh' })
    await wrapper.setProps({ show: true })
    await nextTick()
    expect(document.body.querySelector<HTMLTextAreaElement>('.modal-body textarea')!.value).toBe('fresh')
  })

  it('blocks confirm, cancel, close, and Escape while busy', async () => {
    const wrapper = mountConfirm({ busy: true, busyText: 'Working', reason: true })
    await nextTick()
    const [cancel, confirm] = footerButtons()

    expect(cancel.disabled).toBe(true)
    expect(confirm.disabled).toBe(true)
    expect(confirm.textContent).toBe('Working')

    cancel.click()
    confirm.click()
    document.body.querySelector<HTMLButtonElement>('.modal-header button')!.click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    expect(wrapper.emitted('confirm')).toBeUndefined()
    expect(wrapper.emitted('cancel')).toBeUndefined()
  })
})
