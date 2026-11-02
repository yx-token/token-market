import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import DataTable from '../DataTable.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../DataTable.vue')
const componentSource = readFileSync(componentPath, 'utf8')

const stubViewport = (desktop: boolean) => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: desktop,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn()
    }))
  })
}

const columns = [{ key: 'name', label: 'Name' }]
const data = [
  { id: 1, name: 'Enabled' },
  { id: 2, name: 'Disabled' }
]

describe('DataTable shared layout hooks', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('applies rowClass to desktop rows', () => {
    stubViewport(true)
    const wrapper = mount(DataTable, {
      props: {
        columns,
        data,
        rowKey: 'id',
        rowClass: (row: { id: number }) => (row.id === 2 ? 'row-disabled' : '')
      }
    })

    expect(wrapper.get('tr[data-row-id="1"]').classes()).not.toContain('row-disabled')
    expect(wrapper.get('tr[data-row-id="2"]').classes()).toContain('row-disabled')
  })

  it('applies rowClass to mobile cards', () => {
    stubViewport(false)
    const wrapper = mount(DataTable, {
      props: {
        columns,
        data,
        rowKey: 'id',
        rowClass: (row: { id: number }) => (row.id === 2 ? 'row-disabled' : '')
      }
    })

    const cards = wrapper.findAll('[data-test="data-table-mobile-card"]')
    expect(cards).toHaveLength(2)
    expect(cards[0].classes()).not.toContain('row-disabled')
    expect(cards[1].classes()).toContain('row-disabled')
  })

  it('allows disabling only the sticky first column', () => {
    stubViewport(true)
    const wrapper = mount(DataTable, {
      props: {
        columns: [...columns, { key: 'actions', label: 'Actions' }],
        data,
        stickyFirstColumn: false
      }
    })

    const headers = wrapper.findAll('th')
    expect(headers[0].classes()).not.toContain('sticky-col-left')
    expect(headers[1].classes()).toContain('sticky-col-right')
  })

  it('defines wrapping for mobile values and a reusable desktop truncation class', () => {
    const mobileValue = componentSource.match(/\.mobile-card-value\s*\{([^{}]*)\}/)?.[1]
    const truncation = componentSource.match(/\.data-table-cell-truncate\s*\{([^{}]*)\}/)?.[1]

    expect(mobileValue).toContain('white-space: normal')
    expect(mobileValue).toContain('overflow-wrap: anywhere')
    expect(truncation).toContain('max-width:')
    expect(truncation).toContain('text-overflow: ellipsis')
  })
})
