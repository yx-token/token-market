import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: ReturnType<typeof mount>
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

async function press(key: string) {
  document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  await nextTick()
}

async function open(searchable: boolean | 'auto' = 'auto') {
  wrapper = mount(Select, {
    attachTo: document.body,
    props: { modelValue: 'alpha', searchable, options: [
      { value: 'alpha', label: 'Alpha' },
      { value: 'disabled', label: 'Disabled', disabled: true },
      { value: 'beta', label: 'Beta' },
    ] },
  })
  wrapper.get<HTMLButtonElement>('button').element.focus()
  await press('ArrowDown')
  await nextTick()
}

describe('Select keyboard focus', () => {
  it.each([false, 'auto'] as const)('selects an enabled option without a search input (%s)', async (searchable) => {
    await open(searchable)
    expect(document.activeElement).toBe(document.querySelector('[role="listbox"]'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it('allows Escape to close a non-searchable dropdown and restore trigger focus', async () => {
    await open(false)
    await press('Escape')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps focus in the search input when search is enabled', async () => {
    await open(true)
    expect(document.activeElement).toBe(document.querySelector('.select-search-input'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
  })

  it('links the trigger, listbox, and active option with stable ARIA IDs', async () => {
    await open(false)
    const trigger = wrapper.get('button')
    const listbox = document.querySelector<HTMLElement>('[role="listbox"]')!
    const options = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]'))

    expect(trigger.attributes('aria-haspopup')).toBe('listbox')
    expect(trigger.attributes('aria-controls')).toBe(listbox.id)
    expect(trigger.attributes('aria-activedescendant')).toBe(options[0].id)
    expect(new Set(options.map((option) => option.id)).size).toBe(options.length)

    await press('ArrowDown')
    expect(options[1].getAttribute('aria-disabled')).toBe('true')
    expect(trigger.attributes('aria-activedescendant')).toBe(options[2].id)

    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(trigger.attributes('aria-activedescendant')).toBeUndefined()
  })

  it('does not select a disabled option on click', async () => {
    await open(false)
    const disabled = document.querySelectorAll<HTMLElement>('[role="option"]')[1]
    disabled.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
  })
})
