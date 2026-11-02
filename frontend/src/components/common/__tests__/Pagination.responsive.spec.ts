import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'

import Pagination from '../Pagination.vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function mountPagination(props: Record<string, unknown> = {}) {
  return mount(Pagination, {
    props: {
      total: 200,
      page: 1,
      pageSize: 20,
      showJump: true,
      pageSizeOptions: [15, 20, 50],
      ...props
    },
    global: { stubs: { Icon: true } }
  })
}

describe('Pagination options and responsive accessibility', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses the supplied pageSizeOptions instead of the global defaults', () => {
    const wrapper = mountPagination()
    const select = wrapper.getComponent(Select)

    expect(select.props('options')).toEqual([
      { value: 15, label: '15' },
      { value: 20, label: '20' },
      { value: 50, label: '50' }
    ])
  })

  it('emits a selected custom page size without normalizing it to another option set', async () => {
    const wrapper = mountPagination()

    await wrapper.getComponent(Select).vm.$emit('update:modelValue', 15)

    expect(wrapper.emitted('update:pageSize')).toEqual([[15]])
    expect(localStorage.getItem('table-page-size')).toBe('15')
  })

  it('keeps the current page size visible when it is absent from the supplied options', () => {
    const wrapper = mountPagination({ pageSize: 25, pageSizeOptions: [20, 50] })
    const select = wrapper.getComponent(Select)

    expect(select.props('options')).toContainEqual({ value: 25, label: '25' })
    expect(select.props('modelValue')).toBe(25)
    expect(wrapper.get('.page-size-select button').text()).toContain('25')
  })

  it('names the page size selector, jump input, and pagination navigation', () => {
    const wrapper = mountPagination()

    expect(wrapper.getComponent(Select).props('ariaLabel')).toBe('pagination.perPage')
    expect(wrapper.get('input[type="number"]').attributes('aria-label')).toBe('pagination.jumpTo')
    expect(wrapper.get('nav.pagination-nav').attributes('aria-label')).toBeTruthy()
  })

  it('allows medium-width toolbars and page navigation to wrap and defers jump controls', () => {
    const wrapper = mountPagination()

    expect(wrapper.get('.pagination-desktop').classes()).toContain('sm:flex-wrap')
    expect(wrapper.get('.pagination-tools').classes()).toContain('flex-wrap')
    expect(wrapper.get('.pagination-jump').classes()).toEqual(
      expect.arrayContaining(['hidden', 'lg:flex'])
    )
    expect(wrapper.get('nav.pagination-nav').classes()).toContain('flex-wrap')
  })
})
