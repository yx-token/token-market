import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SystemUpdateView from '@/views/admin/SystemUpdateView.vue'
import { admin } from '@/api'
import type { SystemVersion } from '@/api/types'

vi.mock('@/api', () => ({ admin: { systemVersion: vi.fn(), checkSystemUpdate: vi.fn(), startSystemUpdate: vi.fn() } }))
let wrapper: VueWrapper | undefined
const current: SystemVersion = { version: 'v1.0.0', commit: 'abcd1234', platform: 'linux/amd64', repository: 'yx-token/token-market', enabled: true }
const latest = { update_available: true, release: { version: 'v1.1.0', name: '新版本', notes: '## 新增\n\n- **站内更新** 详见 [文档](https://example.com/docs)\n\n<script>unsafe()</script>\n\n[坏链接](javascript:alert(1))', url: 'https://github.com/yx-token/token-market/releases/tag/v1.1.0', published_at: '2026-10-09T00:00:00Z', compatible: true } }

async function render() {
  wrapper = mount(SystemUpdateView, { global: { stubs: { Teleport: true } } })
  await flushPromises()
  return wrapper
}

describe('system update', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.mocked(admin.systemVersion).mockResolvedValue({ ...current })
    vi.mocked(admin.checkSystemUpdate).mockResolvedValue(structuredClone(latest))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.clearAllMocks(); vi.useRealTimers() })

  it('renders release notes as safe markdown and asks before submitting the checked version', async () => {
    const w = await render()
    expect(w.text()).toContain('v1.1.0')
    const notes = w.find('[data-testid="release-notes"]')
    expect(notes.find('h2').text()).toBe('新增')
    expect(notes.find('li strong').text()).toBe('站内更新')
    const link = notes.find('a[href="https://example.com/docs"]')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(notes.text()).toContain('<script>unsafe()</script>')
    expect(w.find('script').exists()).toBe(false)
    expect(notes.find('a[href^="javascript:"]').exists()).toBe(false)
    await w.findAll('button').find((b) => b.text() === '更新系统')!.trigger('click')
    expect(admin.startSystemUpdate).not.toHaveBeenCalled()
    expect(w.find('[role="dialog"]').text()).toContain('进行中的模型请求可能中断')
    vi.mocked(admin.startSystemUpdate).mockResolvedValue({ id: 'job', version: 'v1.1.0', phase: 'queued', message: '更新任务已创建', started_at: '', updated_at: '' })
    await w.findAll('button').find((b) => b.text() === '确认更新')!.trigger('click')
    await flushPromises()
    expect(admin.startSystemUpdate).toHaveBeenCalledTimes(1)
    expect(admin.startSystemUpdate).toHaveBeenCalledWith('v1.1.0')
    expect(w.text()).toContain('正在更新…')
  })

  it.each(['disabled', 'missing-package', 'current', 'check-error'])('blocks updating when %s', async (state) => {
    if (state === 'disabled') vi.mocked(admin.systemVersion).mockResolvedValue({ ...current, enabled: false, reason: '更新未启用' })
    if (state === 'missing-package') vi.mocked(admin.checkSystemUpdate).mockResolvedValue({ ...latest, release: { ...latest.release, compatible: false } })
    if (state === 'current') vi.mocked(admin.checkSystemUpdate).mockResolvedValue({ ...latest, update_available: false })
    if (state === 'check-error') vi.mocked(admin.checkSystemUpdate).mockRejectedValue(new Error('无法连接 GitHub'))
    const w = await render()
    expect(w.findAll('button').find((b) => b.text() === '更新系统')!.attributes('disabled')).toBeDefined()
  })

  it('keeps polling through a restart and shows rollback result', async () => {
    const job = { id: 'job', version: 'v1.1.0', phase: 'restarting' as const, message: '等待健康检查', started_at: '', updated_at: '' }
    vi.mocked(admin.systemVersion).mockResolvedValueOnce({ ...current, enabled: false, job })
    const w = await render()
    vi.mocked(admin.systemVersion).mockRejectedValueOnce(new Error('network error'))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.text()).toContain('正在自动重试')
    vi.mocked(admin.systemVersion).mockResolvedValueOnce({ ...current, job: { ...job, phase: 'rolled_back', message: '新版本健康检查失败，已恢复原版本' } })
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.text()).toContain('已恢复原版本')
    expect(w.text()).not.toContain('正在自动重试')
    w.unmount()
    wrapper = undefined
    const calls = vi.mocked(admin.systemVersion).mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(admin.systemVersion).toHaveBeenCalledTimes(calls)
  })
})
