import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import TablePageLayout from '../TablePageLayout.vue'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../TablePageLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('TablePageLayout responsive table scrolling', () => {
  it('does not disable the table horizontal scroll container in mobile mode', () => {
    const tableWrapperBlocks = Array.from(
      componentSource.matchAll(/([^{}]*:deep\(\.table-wrapper\)[^{}]*)\{([^{}]*)\}/g)
    )

    expect(tableWrapperBlocks.length).toBeGreaterThan(0)

    const baseBlock = tableWrapperBlocks.find(([selector]) => !selector.includes('.mobile-mode'))
    const mobileBlocks = tableWrapperBlocks.filter(([selector]) => selector.includes('.mobile-mode'))

    expect(baseBlock?.[2]).toContain('overflow-x-auto')
    expect(mobileBlocks.every(([, , declarations]) => !declarations.includes('overflow-visible'))).toBe(
      true
    )
  })

  it('lets the mobile root grow with its content without clipping filters, cards, or pagination', () => {
    const mobileRoot = componentSource.match(/\.table-page-layout\.mobile-mode\s*\{([^{}]*)\}/)
    const declarations = mobileRoot?.[1].replace(/\s+/g, ' ')

    expect(declarations).toContain('height: auto')
    expect(declarations).toContain('min-height: 100%')
    expect(declarations).toContain('overflow: visible')
  })

  it('provides an opt-in vertical scroll chain for multi-section content', () => {
    const contentMode = componentSource.match(
      /\.table-page-layout \.y-container--tight\.content-scroll-mode\s*\{([^{}]*)\}/
    )
    const declarations = contentMode?.[1].replace(/\s+/g, ' ')

    expect(declarations).toContain('flex: 1')
    expect(declarations).toContain('min-height: 0')
    expect(declarations).toContain('overflow-y: auto')
  })
})

describe('TablePageLayout content slots', () => {
  it('does not render an empty table card when the table slot is absent', () => {
    const wrapper = mount(TablePageLayout, {
      slots: {
        filters: '<div data-test="filters">Filters</div>',
        pagination: '<div data-test="pagination">Pagination</div>'
      }
    })

    expect(wrapper.find('.table-scroll-container').exists()).toBe(false)
    expect(wrapper.find('[data-test="filters"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="pagination"]').exists()).toBe(true)
  })

  it('keeps rendering supplied table content and marks content scroll mode', () => {
    const wrapper = mount(TablePageLayout, {
      props: { contentMode: 'content' },
      slots: { table: '<div data-test="table-content">Table</div>' }
    })

    expect(wrapper.get('.table-scroll-container').find('[data-test="table-content"]').exists()).toBe(true)
    expect(wrapper.get('.y-container--tight').classes()).toContain('content-scroll-mode')
  })
})
