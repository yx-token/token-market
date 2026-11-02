import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')
const main = read('../../../main.ts')
const layout = read('../AppLayout.vue')
const header = read('../AppHeader.vue')
const bridge = read('../../../styles/xianmao-admin-bridge.css')
const consoleStyle = read('../../../styles/xianmao-console.css')
const datePicker = read('../../common/DateRangePicker.vue')

describe('console style entry', () => {
  it('keeps AppLayout as the only bridge importer with a fixed console order', () => {
    expect(main).not.toContain("import './styles/xianmao-admin-bridge.css'")
    const imports = [
      "@/styles/onboarding.css",
      "@/styles/xianmao-console.css",
      "@/styles/console-liquid.css",
      "@/styles/xianmao-admin-bridge.css",
    ].map((path) => layout.indexOf(path))
    expect(imports.every((index) => index >= 0)).toBe(true)
    expect(imports).toEqual([...imports].sort((a, b) => a - b))
  })
})

describe('console responsive shell', () => {
  it('uses the 1024px drawer breakpoint and dvh with vh fallback', () => {
    expect(bridge).toContain('@media (min-width: 1024px)')
    expect(bridge).toContain('@media (max-width: 1023.98px)')
    expect(consoleStyle).not.toContain('@media (max-width: 900px)')
    expect(bridge).toMatch(/height: 100vh;\s*height: 100dvh;/)
    expect(bridge).toMatch(/\.console-app \.sidebar\.is-mobile-open\s*\{\s*transform: translateX\(0\);/)
  })

  it('keeps body scrolling in console-body-slot only', () => {
    expect(bridge).toMatch(/\.console-app \.console-main-col\s*\{[\s\S]*?overflow: hidden;/)
    expect(bridge).toMatch(/\.console-app \.console-body-slot\s*\{[\s\S]*?overflow-y: auto;/)
    expect(layout).toContain('class="console-body-slot"')
  })

  it('collapses header actions against the actual header width inside the console main column', () => {
    expect(bridge).toContain('container: console-main / inline-size;')
    expect(header).toContain('container: console-header / inline-size;')
    expect(header).toContain('@container console-header (max-width: 1000px)')
    expect(header).toContain('console-header-announcement')
    expect(header).toContain('console-header-support')
    expect(header).toContain('console-header-user')
  })
})

describe('console theme and table safeguards', () => {
  it('defines elevated and secondary tokens in both themes with accessible light status colors', () => {
    expect(bridge).toContain('--color-bg-elevated: #ffffff;')
    expect(bridge).toContain('--color-bg-secondary: #f2f6fc;')
    expect(bridge).toContain('--color-bg-elevated: #1c1c1c;')
    expect(bridge).toContain('--color-bg-secondary: #111111;')
    expect(bridge).toContain('--color-text-secondary: #64748b;')
    expect(bridge).toContain('--color-success: #237804;')
    expect(bridge).toContain('--color-warning: #ad4e00;')
    expect(bridge).toContain('--color-error: #cf1322;')
    expect(bridge).toMatch(/html\.dark \.console-app \.btn-secondary[\s\S]*?background: var\(--color-bg-elevated/)
  })

  it('excludes fixed columns from zebra rows and restores opaque sticky cells', () => {
    const zebra = bridge.match(/tr:nth-child\(even\)[^{\n]+/g) || []
    expect(zebra).toHaveLength(3)
    for (const selector of zebra) {
      expect(selector).toContain(':not(.sticky-col)')
      expect(selector).toContain(':not(.y-col-op)')
    }
    expect(bridge).toContain('.console-app .y-page tbody td.sticky-col')
  })

  it('uses a named Element Plus popper with dark variables and targeted reduced motion', () => {
    expect(datePicker).toContain('popper-class="console-date-popper"')
    expect(bridge).toContain('html.dark .console-date-popper.el-popper')
    expect(bridge).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.console-page\s*\{\s*animation: none;/)
  })
})
