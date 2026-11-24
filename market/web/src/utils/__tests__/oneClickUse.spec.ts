import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  ONE_CLICK_CLIENTS,
  ONE_CLICK_DEFAULT_CLIENT,
  ONE_CLICK_HELPER_LOCAL,
  buildHelperProtocolURL,
  buildOneClickImportURL,
  clearHelperInstalledMark,
  downloadsFor,
  getOneClickClient,
  hasHelperInstalledMark,
  initialProgressSteps,
  logoutHelperSession,
  markHelperInstalled,
  primaryDownload,
  readPreferredClient,
  setStep,
  setupPlatformToOS,
  syncHelperSession,
  writePreferredClient,
} from '@/utils/oneClickUse'

describe('oneClickUse', () => {
  it('lists flux + local clients for the picker', () => {
    expect(ONE_CLICK_CLIENTS.map((c) => c.id)).toEqual([
      'codex',
      'claude-code',
      'claude-desktop',
      'pi',
      'hermes',
      'openclaw',
      'paseo',
      'zcode',
      'deepseek-harness',
      'obsidian',
      'cherry-studio',
      'chatbox',
    ])
  })

  it('defaults preferred client and remembers last choice', () => {
    localStorage.clear()
    expect(readPreferredClient()).toBe(ONE_CLICK_DEFAULT_CLIENT)
    writePreferredClient('codex')
    expect(readPreferredClient()).toBe('codex')
  })

  it('clears stale helper-installed mark after uninstall', () => {
    localStorage.clear()
    markHelperInstalled()
    expect(hasHelperInstalledMark()).toBe(true)
    clearHelperInstalledMark()
    expect(hasHelperInstalledMark()).toBe(false)
  })

  it('maps setup platform to OS', () => {
    expect(setupPlatformToOS('windows')).toBe('windows')
    expect(setupPlatformToOS('unix')).toBe('macos')
  })

  it('returns OS-specific downloads without CC Switch', () => {
    const codex = getOneClickClient('codex')
    const mac = downloadsFor(codex, 'macos')
    expect(mac.some((d) => d.id === 'cc-switch')).toBe(false)
    expect(mac.every((d) => d.url.startsWith('http'))).toBe(true)
    expect(ONE_CLICK_CLIENTS.every((c) => !('preferCCSwitch' in c))).toBe(true)
  })

  it('picks a single primary download instead of spraying tabs', () => {
    const codex = primaryDownload(getOneClickClient('codex'), 'macos')
    expect(codex.id).toBe('codex-desktop')
    expect(codex.url.startsWith('http')).toBe(true)
    const cherry = primaryDownload(getOneClickClient('cherry-studio'), 'macos')
    expect(cherry.id).toBe('cherry')
  })

  it('builds helper protocol URL without embedding the API key', () => {
    const url = buildHelperProtocolURL({
      client: 'codex',
      ticket: 't-abc',
      os: 'macos',
      apiBase: 'https://idlicat.com/',
    })
    expect(url).toBe('xianmao://one-click?ticket=t-abc&client=codex&os=macos&api=https%3A%2F%2Fidlicat.com')
    expect(url).not.toMatch(/sk-|apiKey/i)
  })

  it('pushes browser login/logout to the local helper bridge', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === `${ONE_CLICK_HELPER_LOCAL}/session/sync`) {
        expect(init?.method).toBe('POST')
        const body = JSON.parse(String(init?.body || '{}'))
        expect(body.user_id).toBe(1)
        expect(body.account).toBe('admin@dev.local')
        return new Response(JSON.stringify({ ok: true }), { status: 200 })
      }
      if (url === `${ONE_CLICK_HELPER_LOCAL}/session/logout`) {
        expect(init?.method).toBe('POST')
        return new Response(JSON.stringify({ ok: true }), { status: 200 })
      }
      return new Response('no', { status: 404 })
    })
    vi.stubGlobal('fetch', fetchMock)
    expect(
      await syncHelperSession({ userId: 1, account: 'admin@dev.local', apiBase: 'http://127.0.0.1:8080' }),
    ).toBe(true)
    expect(await logoutHelperSession()).toBe(true)
    vi.unstubAllGlobals()
  })

  it('does not build CC Switch import URL for Codex anymore', () => {
    const url = buildOneClickImportURL({
      client: getOneClickClient('codex'),
      key: { id: 1, key: 'sk-test-key', name: 'k', status: 'active', created_at: '' },
      baseURL: 'https://idlicat.com',
    })
    expect(url).toBe('')
    expect(url).not.toMatch(/ccswitch:/)
  })

  it('builds direct import URL for Cherry Studio', () => {
    const url = buildOneClickImportURL({
      client: getOneClickClient('cherry-studio'),
      key: { id: 2, key: 'sk-cherry', name: 'k', status: 'active', created_at: '' },
      baseURL: 'https://idlicat.com',
    })
    expect(url.startsWith('cherrystudio://')).toBe(true)
  })

  it('updates progress step status', () => {
    let steps = initialProgressSteps('Codex')
    steps = setStep(steps, 'key', 'active')
    expect(steps.find((s) => s.id === 'key')?.status).toBe('active')
    steps = setStep(steps, 'key', 'done')
    steps = setStep(steps, 'download', 'active')
    expect(steps.find((s) => s.id === 'key')?.status).toBe('done')
    expect(steps.find((s) => s.id === 'download')?.status).toBe('active')
  })
})

describe('handoffToHelper', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts job only when local helper health is ok', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/health')) {
        return new Response(JSON.stringify({ ok: true }), { status: 200 })
      }
      if (url.endsWith('/status')) {
        return new Response(JSON.stringify({ ok: true, jobSeq: 0 }), { status: 200 })
      }
      if (url.endsWith('/one-click')) {
        expect(init?.method).toBe('POST')
        return new Response(JSON.stringify({ ok: true }), { status: 202 })
      }
      return new Response('no', { status: 404 })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { handoffToHelper } = await import('@/utils/oneClickUse')
    const ok = await handoffToHelper({
      ticket: 't1',
      client: 'cherry-studio',
      os: 'macos',
      api: 'http://127.0.0.1:8080',
    })
    expect(ok).toBe(true)
    expect(fetchMock.mock.calls.some((c) => String(c[0]).endsWith('/one-click'))).toBe(true)
  })

  it('falls back to xianmao:// then treats advanced jobSeq as success without re-posting ticket', async () => {
    let alive = false
    let seq = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/health')) {
        if (!alive) throw new TypeError('Failed to fetch')
        return new Response(JSON.stringify({ ok: true }), { status: 200 })
      }
      if (url.endsWith('/status')) {
        return new Response(JSON.stringify({ ok: true, jobSeq: seq, last: { client: 'cherry-studio' } }), {
          status: 200,
        })
      }
      if (url.endsWith('/one-click')) {
        return new Response(JSON.stringify({ ok: false, error: 'ticket already used' }), { status: 400 })
      }
      return new Response('no', { status: 404 })
    })
    vi.stubGlobal('fetch', fetchMock)
    const clicks: string[] = []
    const realCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = realCreate(tag) as HTMLAnchorElement
      if (tag === 'a') {
        el.click = () => {
          clicks.push(el.href)
          // 深链唤起后本机桥上线并已开始处理
          alive = true
          seq = 1
        }
      }
      return el
    })
    const { handoffToHelper } = await import('@/utils/oneClickUse')
    const ok = await handoffToHelper({
      ticket: 't-deeplink',
      client: 'cherry-studio',
      os: 'macos',
      api: 'http://127.0.0.1:8080',
    })
    expect(ok).toBe(true)
    expect(clicks.some((u) => u.startsWith('xianmao://one-click?'))).toBe(true)
    expect(fetchMock.mock.calls.some((c) => String(c[0]).endsWith('/status'))).toBe(true)
    vi.restoreAllMocks()
  })
})

describe('launchImportURL', () => {
  it('clicks a hidden anchor instead of navigating the page', async () => {
    const clicks: string[] = []
    const realCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = realCreate(tag) as HTMLAnchorElement
      if (tag === 'a') {
        el.click = () => {
          clicks.push(el.href)
        }
      }
      return el
    })
    const { launchImportURL } = await import('@/utils/oneClickUse')
    launchImportURL('ccswitch://v1/import?app=codex')
    expect(clicks.some((u) => u.startsWith('ccswitch://'))).toBe(true)
    vi.restoreAllMocks()
  })
})
