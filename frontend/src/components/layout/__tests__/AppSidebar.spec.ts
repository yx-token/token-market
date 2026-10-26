import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')
const bridgeStylePath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../../styles/xianmao-admin-bridge.css',
)
const bridgeStyleSource = readFileSync(bridgeStylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar collapsible groups', () => {
  it('lets the user collapse a group even while a child route is active', () => {
    // The expand state must come from the user's override first, falling back
    // to the active-route heuristic only when the user has not clicked yet.
    expect(componentSource).toContain('const groupExpandOverrides = ref<Map<string, boolean>>(new Map())')
    expect(componentSource).not.toContain('expandedGroups.value.has(item.path) || isGroupActive(item)')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar market menu hierarchy', () => {
  it('does not nest buyer entry as a fake L2 group under idle market', () => {
    expect(componentSource).not.toContain("path: '/admin/market/section-front'")
    expect(componentSource).not.toContain("label: t('nav.marketFront')")
    // buyer leaves hang directly under idle market (L2)
    expect(componentSource).toMatch(/path: '\/admin\/market'[\s\S]*?path: '\/'[\s\S]*?nav\.backToMarket/)
    expect(componentSource).not.toMatch(/nav\.marketShelf/)
    expect(componentSource).toMatch(/path: '\/shop\/center'[\s\S]*?nav\.marketCenter/)
  })

  it('keeps supply / funds / quality / ops as L2 groups with L3 children', () => {
    for (const section of [
      'section-supply',
      'section-funds',
      'section-quality',
      'section-ops',
    ]) {
      expect(componentSource).toContain(`path: '/admin/market/${section}'`)
    }
  })
})

describe('AppSidebar level indent styles', () => {
  it('indents L2/L3 via submenu padding so they are not flush with L1', () => {
    expect(bridgeStyleSource).toMatch(/\.sidebar-submenu--l2\s*\{[\s\S]*?padding:\s*2px 0 2px 18px/)
    expect(bridgeStyleSource).toMatch(/\.sidebar-submenu--l3\s*\{[\s\S]*?padding:\s*2px 0 2px 14px/)
  })

  it('does not paint L2 group headers with the same active pill as L1', () => {
    expect(bridgeStyleSource).toMatch(
      /\.sidebar-group\s*>\s*\.sidebar-link--l2\.sidebar-link-active\s*\{[\s\S]*?background:\s*transparent/,
    )
  })
})

describe('AppSidebar subscription feature flag', () => {
  it('gates the My Subscriptions entry behind the subscription public-settings flag', () => {
    expect(componentSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(componentSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('also hides the admin Subscription Management entry on recharge-only sites', () => {
    expect(componentSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(componentSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(componentSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(componentSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(componentSource).toMatch(/path: '\/purchase'[^\n]*label: purchaseNavLabel\.value/)
  })
})
