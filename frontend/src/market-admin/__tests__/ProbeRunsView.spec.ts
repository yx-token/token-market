import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import ProbeRunsView from '../ProbeRunsView.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function run(id: number) {
  return {
    id,
    code: id === 1 ? 'legacy-run' : `run-${id}`,
    upstream_id: id,
    upstream_code: `UP-${id}`,
    status: 'succeeded',
    trigger: 'recovery',
    platform: 'zhipu',
    account_email: id === 1 ? 'legacy@example.test' : `user-${id}@example.test`,
  }
}

let rows: ReturnType<typeof run>[]
let router: Router

async function page(query: Record<string, string> = { q: 'legacy' }) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: ProbeRunsView }],
  })
  await router.push({ path: '/', query })
  await router.isReady()
  const wrapper = mount(ProbeRunsView, {
    global: {
      plugins: [router],
      stubs: { Icon: true, Select: true, Teleport: true },
    },
  })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.setItem('table-page-size', '10')
  rows = [run(1), run(2)]
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/probe-runs') return structuredClone(rows)
    throw new Error(`unexpected GET ${path}`)
  })
})

afterEach(() => {
  window.localStorage.removeItem('table-page-size')
})
enableAutoUnmount(afterEach)

describe('检测记录页', () => {
  it('query 仅在挂载和变化时应用，重置后刷新不会重放旧关键词', async () => {
    const wrapper = await page()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(1)])
    expect(wrapper.getComponent(DataTable).props('stickyFirstColumn')).toBe(false)
    expect(wrapper.text()).toContain('智谱')
    expect(wrapper.text()).toContain('恢复')

    await click(wrapper, '重置')
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(1), run(2)])
    await click(wrapper, '刷新')
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(1), run(2)])

    await router.push({ path: '/', query: { q: 'run-2' } })
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(2)])
  })

  it('明细失败可重试，并区分待判定、通过与未通过', async () => {
    let detailAttempts = 0
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/probe-runs') return structuredClone(rows)
      if (path === '/api/v1/admin/market/probe-runs/1') {
        detailAttempts += 1
        if (detailAttempts === 1) throw new Error('明细加载失败')
        return {
          run: run(1),
          results: [
            { suite: 'inspection_quality', check_key: 'Q1', passed: null },
            { suite: 'inspection_quality', check_key: 'Q2', passed: true },
            { suite: 'inspection_quality', check_key: 'Q3', passed: false },
          ],
        }
      }
      throw new Error(`unexpected GET ${path}`)
    })

    const wrapper = await page()
    await click(wrapper, '明细')
    expect(wrapper.get('[role="dialog"]').get('[role="alert"]').text()).toBe('明细加载失败')

    await click(wrapper, '重试')
    expect(detailAttempts).toBe(2)
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('成功')
    expect(dialog.text()).toContain('恢复')
    const chips = dialog.findAll('.probe-result-chip')
    expect(chips.map((chip) => chip.text())).toEqual(['待判定', '通过', '未通过'])
    expect(chips[0]!.classes()).toContain('probe-result-chip--idle')
    expect(chips[1]!.classes()).toContain('probe-result-chip--ok')
    expect(chips[2]!.classes()).toContain('probe-result-chip--err')
  })

  it('刷新后记录减少会回收页码', async () => {
    rows = Array.from({ length: 11 }, (_, index) => run(index + 1))
    const wrapper = await page({})
    const pagination = wrapper.getComponent(Pagination)
    expect(pagination.props('pageSize')).toBe(10)

    pagination.vm.$emit('update:page', 2)
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(11)])

    rows = [run(1)]
    await click(wrapper, '刷新')
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([run(1)])
  })
})
