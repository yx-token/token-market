import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: Array<ReturnType<typeof mount>> = []

function mountDialog(props: Record<string, unknown>, slots: Record<string, string> = {}) {
  const wrapper = mount(BaseDialog, {
    attachTo: document.body,
    props,
    slots,
    global: { stubs: { Icon: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}

async function pressTab(shiftKey = false) {
  document.activeElement!.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true })
  )
  await nextTick()
}

describe('BaseDialog', () => {
  afterEach(() => {
    while (wrappers.length) wrappers.pop()!.unmount()
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mountDialog(
      { show: false, title: 'Details' },
      { default: '<div style="height: 2000px">content</div>' }
    )

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
  })

  it('cycles Tab and Shift+Tab within the dialog', async () => {
    mountDialog(
      { show: true, title: 'Edit', showCloseButton: false },
      {
        default: '<button id="dialog-first">First</button><button id="dialog-middle">Middle</button>',
        footer: '<button id="dialog-last">Save</button>'
      }
    )
    await nextTick()

    const first = document.getElementById('dialog-first')!
    const last = document.getElementById('dialog-last')!
    expect(document.activeElement).toBe(first)

    last.focus()
    await pressTab()
    expect(document.activeElement).toBe(first)

    await pressTab(true)
    expect(document.activeElement).toBe(last)

    await pressTab()
    expect(document.activeElement).toBe(first)
  })

  it('closes on Escape and restores focus to the opener', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Open'
    document.body.appendChild(opener)
    opener.focus()

    const wrapper = mountDialog(
      { show: false, title: 'Edit', showCloseButton: false },
      { default: '<button id="dialog-action">Action</button>' }
    )
    await wrapper.setProps({ show: true })
    await nextTick()
    expect(document.activeElement).toBe(document.getElementById('dialog-action'))

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toEqual([[]])

    await wrapper.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(opener)
  })

  it('only closes the topmost dialog on Escape and preserves the remaining dialog', async () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()

    const first = mountDialog(
      { show: true, title: 'First', showCloseButton: false },
      { default: '<button id="first-action">First action</button>' }
    )
    await nextTick()
    const second = mountDialog(
      { show: true, title: 'Second', showCloseButton: false },
      { default: '<button id="second-action">Second action</button>' }
    )
    await nextTick()
    expect(document.activeElement).toBe(document.getElementById('second-action'))

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(second.emitted('close')).toEqual([[]])
    expect(first.emitted('close')).toBeUndefined()

    await second.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(document.getElementById('first-action'))
    expect(document.body.classList.contains('modal-open')).toBe(true)

    await first.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(opener)
  })

  it('does not steal focus when a non-topmost dialog closes', async () => {
    const first = mountDialog(
      { show: true, title: 'First', showCloseButton: false },
      { default: '<button>First action</button>' }
    )
    await nextTick()
    const second = mountDialog(
      { show: true, title: 'Second', showCloseButton: false },
      { default: '<button id="second-action">Second action</button>' }
    )
    await nextTick()

    await first.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(document.getElementById('second-action'))
    expect(document.body.classList.contains('modal-open')).toBe(true)

    await second.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('gives the close button an accessible name', async () => {
    mountDialog({ show: true, title: 'Details' })
    await nextTick()
    expect(document.body.querySelector('.modal-header button')?.getAttribute('aria-label')).toBe('Close dialog')
  })
})
