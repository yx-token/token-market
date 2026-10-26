import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { effectScope } from 'vue'
import { useAppStore } from '@/stores/app'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
import SystemUpdateSettings from '../SystemUpdateSettings.vue'
import VersionBadge from '@/components/common/VersionBadge.vue'

const { check, update, restart, versions, rollback, copy, auth } = vi.hoisted(() => ({
  check: vi.fn(), update: vi.fn(), restart: vi.fn(), versions: vi.fn(), rollback: vi.fn(),
  copy: vi.fn(), auth: { isAdmin: true }
}))
vi.mock('@/api/admin/system', () => ({
  checkUpdates: check, performUpdate: update, restartService: restart,
  getRollbackVersions: versions, rollback
}))
vi.mock('@/stores', async () => ({
  useAppStore: (await import('@/stores/app')).useAppStore,
  useAuthStore: () => auth
}))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') })
}))
vi.mock('@/composables/useClipboard', async () => {
  const { ref } = await import('vue')
  return { useClipboard: () => ({ copied: ref(false), copyToClipboard: copy }) }
})

const release = {
  current_version: '1.0.0', latest_version: '1.1.0', has_update: true,
  build_type: 'release', cached: false
}
const rollbackVersions = ['0.9.0', '0.8.0', '0.7.0', '0.6.0'].map(version => ({
  version, published_at: '2026-10-01T00:00:00Z', html_url: '#'
}))
let wrapper: VueWrapper | undefined
const find = (id: string) => wrapper!.get(`[data-testid="${id}"]`)
async function render() {
  wrapper = mount(SystemUpdateSettings, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}
async function openRollback() {
  await find('toggle-rollback').trigger('click')
  await flushPromises()
}

describe('SystemUpdateSettings and shared update actions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    vi.resetAllMocks()
    auth.isAdmin = true
    check.mockResolvedValue({ ...release })
    update.mockResolvedValue({ message: 'ok', need_restart: true })
    rollback.mockResolvedValue({ message: 'ok', need_restart: true })
    restart.mockResolvedValue({ message: 'ok' })
    versions.mockResolvedValue({ versions: rollbackVersions })
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('shows both versions, release controls and Docker instructions; checks with force', async () => {
    await render()
    expect(check).toHaveBeenCalledWith(false)
    expect(wrapper!.text()).toContain('1.0.0')
    expect(wrapper!.text()).toContain('1.1.0')
    expect(find('version-status').text()).toBe('version.updateAvailable')
    expect(find('perform-update').exists()).toBe(true)
    expect(wrapper!.text()).toContain('admin.settings.systemUpdate.releaseBuild')
    expect(wrapper!.text()).toContain('admin.settings.systemUpdate.dockerHint')
    expect(wrapper!.text()).toContain('docker compose pull && docker compose up -d')
    expect(wrapper!.text()).toContain('image: weishaw/sub2api:1.1.0')
    await find('check-updates').trigger('click')
    await flushPromises()
    expect(check).toHaveBeenLastCalledWith(true)
    expect(wrapper!.findAll('button').every(button => button.attributes('type') === 'button')).toBe(true)
  })

  it('shows up-to-date status without an update button and retains rollback access', async () => {
    check.mockResolvedValue({ ...release, has_update: false, latest_version: '1.0.0' })
    await render()
    expect(find('version-status').text()).toBe('version.upToDate')
    expect(wrapper!.find('[data-testid="perform-update"]').exists()).toBe(false)
    expect(find('toggle-rollback').exists()).toBe(true)
  })

  it.each(['source', 'docker', 'development'])('offers only manual instructions for %s builds', async build_type => {
    check.mockResolvedValue({ ...release, build_type })
    await render()
    expect(find('source-update-hint').text()).toBe('admin.settings.systemUpdate.sourceHint')
    expect(wrapper!.text()).toContain('admin.settings.systemUpdate.dockerInstructions')
    expect(wrapper!.find('[data-testid="perform-update"]').exists()).toBe(false)
    expect(wrapper!.find('[data-testid="toggle-rollback"]').exists()).toBe(false)
    expect(update).not.toHaveBeenCalled()
  })

  it('shows check errors, avoids an up-to-date claim, and recovers after retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    check.mockRejectedValueOnce({ message: 'Cannot reach releases' })
    useAppStore().siteVersion = '0.9.8'
    await render()
    expect(wrapper!.text()).toContain('0.9.8')
    expect(wrapper!.get('[role="alert"]').text()).toContain('Cannot reach releases')
    expect(wrapper!.find('[data-testid="version-status"]').exists()).toBe(false)
    await find('check-updates').trigger('click')
    await flushPromises()
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
    expect(find('version-status').text()).toBe('version.updateAvailable')
  })

  it('treats HTTP 200 warning as an unsuccessful check and preserves it in cached data', async () => {
    check.mockResolvedValue({ ...release, warning: 'GitHub rate limit', has_update: false })
    await render()
    expect(wrapper!.get('[role="alert"]').text()).toContain('GitHub rate limit')
    expect(wrapper!.find('[data-testid="version-status"]').exists()).toBe(false)
    expect(wrapper!.find('[data-testid="perform-update"]').exists()).toBe(false)
    expect((await useAppStore().fetchVersion())?.warning).toBe('GitHub rate limit')
    expect(check).toHaveBeenCalledTimes(1)
    check.mockResolvedValue({ ...release })
    await find('check-updates').trigger('click')
    await flushPromises()
    expect(useAppStore().versionWarning).toBe('')
    expect(find('perform-update').exists()).toBe(true)
  })

  it('disables duplicate checks while the request is pending', async () => {
    let resolve!: (value: typeof release) => void
    check.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    wrapper = mount(SystemUpdateSettings, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(find('check-updates').attributes('disabled')).toBeDefined()
    await find('check-updates').trigger('click')
    expect(check).toHaveBeenCalledTimes(1)
    resolve(release)
    await flushPromises()
    expect(find('check-updates').attributes('disabled')).toBeUndefined()
  })

  it('shows update success, restart requirement and countdown, then clears timers on unmount', async () => {
    await render()
    await find('perform-update').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('version.updateComplete')
    expect(wrapper!.text()).toContain('version.restartRequired')
    expect(find('check-updates').attributes('disabled')).toBeDefined()
    expect(restart).not.toHaveBeenCalled()
    await find('restart-service').trigger('click')
    await flushPromises()
    expect(restart).toHaveBeenCalledTimes(1)
    expect(wrapper!.text()).toContain('"seconds":8')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper!.text()).toContain('"seconds":6')
    wrapper!.unmount()
    wrapper = undefined
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetch).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('aborts health checks if unmounted after the restart countdown', async () => {
    let resolve!: (value: Response) => void
    vi.mocked(fetch).mockImplementation(() => new Promise(r => { resolve = r }))
    await render()
    await find('perform-update').trigger('click')
    await flushPromises()
    await find('restart-service').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(8000)
    expect(fetch).toHaveBeenCalledTimes(1)
    const signal = vi.mocked(fetch).mock.calls[0][1]?.signal
    expect(signal?.aborted).toBe(false)
    wrapper!.unmount()
    wrapper = undefined
    expect(signal?.aborted).toBe(true)
    resolve({ ok: true } as Response)
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows explicit restart rejection and allows retry without starting a countdown', async () => {
    restart.mockRejectedValueOnce({ status: 403, message: 'Restart forbidden' })
    await render()
    await find('perform-update').trigger('click')
    await flushPromises()
    await find('restart-service').trigger('click')
    await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('Restart forbidden')
    expect(vi.getTimerCount()).toBe(0)
    expect(find('restart-service').attributes('disabled')).toBeUndefined()
    await find('restart-service').trigger('click')
    await flushPromises()
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
    expect(restart).toHaveBeenCalledTimes(2)
  })

  it('shows an update error and allows retry', async () => {
    update.mockRejectedValueOnce({ message: 'Download failed' })
    await render()
    await find('perform-update').trigger('click')
    await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('Download failed')
    await find('perform-update').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('version.updateComplete')
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not require restart when the update result says it is unnecessary', async () => {
    update.mockResolvedValue({ need_restart: false })
    await render()
    await find('perform-update').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('version.updateComplete')
    expect(wrapper!.find('[data-testid="restart-service"]').exists()).toBe(false)
  })

  it('lists the last three versions, selects a target, copies commands and rolls back', async () => {
    await render()
    await openRollback()
    const targets = wrapper!.findAll('[data-testid="rollback-version"]')
    expect(targets).toHaveLength(3)
    await targets[1].trigger('click')
    expect(targets[1].attributes('aria-pressed')).toBe('true')
    expect(wrapper!.text()).toContain('/v0.8.0/deploy/install.sh')
    const copyButton = wrapper!.findAll('button').find(button => button.text() === 'version.copyCommand')!
    // There are Docker command blocks above; select the rollback command copy button.
    const copies = wrapper!.findAll('button').filter(button => button.text() === copyButton.text())
    await copies[copies.length - 1].trigger('click')
    expect(copy).toHaveBeenLastCalledWith(expect.stringContaining('rollback v0.8.0'))
    await wrapper!.findAll('button').find(button => button.text() === 'version.deployDocker')!.trigger('click')
    expect(wrapper!.text()).toContain('image: weishaw/sub2api:0.8.0')
    await find('rollback-submit').trigger('click')
    await flushPromises()
    expect(rollback).toHaveBeenCalledWith('0.8.0')
    expect(wrapper!.text()).toContain('version.rollbackComplete')
    expect(wrapper!.text()).toContain('version.restartRequired')
    expect(find('restart-service').exists()).toBe(true)
  })

  it('reports rollback list failure, retries, and handles an empty list', async () => {
    versions.mockRejectedValueOnce({ message: 'Cannot load versions' }).mockResolvedValueOnce({ versions: [] })
    await render()
    await openRollback()
    expect(wrapper!.get('[role="alert"]').text()).toContain('Cannot load versions')
    await find('retry-rollback').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('version.noRollbackVersions')
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
  })

  it('shows rollback error and supports deselecting or retrying the target', async () => {
    rollback.mockRejectedValueOnce({ message: 'Rollback download failed' })
    await render()
    await openRollback()
    const target = find('rollback-version')
    await target.trigger('click')
    await find('rollback-submit').trigger('click')
    await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('Rollback download failed')
    await target.trigger('click')
    expect(wrapper!.find('[data-testid="rollback-submit"]').exists()).toBe(false)
    await target.trigger('click')
    await find('rollback-submit').trigger('click')
    await flushPromises()
    expect(rollback).toHaveBeenCalledTimes(2)
  })

  it('guards simultaneous update/rollback actions and source builds in the shared composable', async () => {
    const store = useAppStore()
    store.buildType = 'release'
    let resolve!: (value: { need_restart: boolean }) => void
    update.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const scope = effectScope()
    const state = scope.run(useSystemUpdate)!
    state.selectedRollbackVersion.value = '0.9.0'
    const pending = state.handleUpdate()
    await state.handleUpdate()
    await state.handleRollback()
    await state.refreshVersion()
    expect(update).toHaveBeenCalledTimes(1)
    expect(rollback).not.toHaveBeenCalled()
    expect(check).not.toHaveBeenCalled()
    resolve({ need_restart: true })
    await pending
    await state.handleUpdate()
    expect(update).toHaveBeenCalledTimes(1)
    store.buildType = 'source'
    state.needRestart.value = false
    await state.handleUpdate()
    await state.handleRollback()
    expect(update).toHaveBeenCalledTimes(1)
    expect(rollback).not.toHaveBeenCalled()
    scope.stop()
  })

  it('preserves the non-admin sidebar badge without admin API requests', async () => {
    auth.isAdmin = false
    wrapper = mount(VersionBadge, { props: { version: '1.2.3' }, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toBe('v1.2.3')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(check).not.toHaveBeenCalled()
  })

  it('keeps the admin badge dropdown and shared update action working', async () => {
    wrapper = mount(VersionBadge, { props: { version: '1.2.3' }, global: { stubs: { Icon: true } } })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    const button = wrapper.findAll('button').find(button => button.text().includes('version.updateNow'))!
    await button.trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('version.updateComplete')
    expect(wrapper.text()).toContain('version.restartRequired')
  })
})
