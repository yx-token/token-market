import { describe, expect, it } from 'vitest'
import { consolePathToAdmin } from './consoleToAdmin'

describe('consolePathToAdmin', () => {
  it('rewrites console bookmarks to admin', () => {
    expect(consolePathToAdmin('/console')).toBe('/admin')
    expect(consolePathToAdmin('/console/funds')).toBe('/admin/funds')
    expect(consolePathToAdmin('/console/upstreams')).toBe('/admin/upstreams')
  })
})
