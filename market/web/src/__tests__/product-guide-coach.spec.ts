import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ProductGuideCoach from '@/components/ProductGuideCoach.vue'

const base = {
  open: true,
  stepIndex: 1,
  stepTotal: 8,
  title: '第 2 步 · 选好出口代理',
  body: '按厂商规则选择代理',
  primaryLabel: '知道了，我来选',
  secondaryLabel: '去添加代理',
  spotlight: '[data-guide="publish-panel"]',
}

function mountCoach(props: Record<string, unknown> = {}) {
  return mount(ProductGuideCoach, { props: { ...base, ...props }, attachTo: document.body })
}

describe('ProductGuideCoach', () => {
  it('打开期间在页面末尾垫留白，保证被卡片盖住的底部按钮能滚上来', async () => {
    const w = mountCoach()
    await nextTick()
    expect(document.body.querySelector('[data-testid="guide-reserve"]')).not.toBeNull()
    await w.setProps({ open: false })
    expect(document.body.querySelector('[data-testid="guide-reserve"]')).toBeNull()
    w.unmount()
  })

  it('collapseOnPrimary：点主按钮后收成胶囊，不再盖住表单；切换步骤自动展开', async () => {
    const w = mountCoach({ collapseOnPrimary: true })
    await nextTick()
    const primary = [...document.body.querySelectorAll<HTMLButtonElement>('.pg-btn.primary')]
    expect(primary).toHaveLength(1)
    primary[0]!.click()
    await nextTick()
    expect(w.emitted('primary')).toHaveLength(1)
    expect(document.body.querySelector('.pg-coach')).toBeNull()
    const pill = document.body.querySelector<HTMLButtonElement>('[data-testid="guide-pill"]')
    expect(pill?.textContent).toContain('2 / 8')

    await w.setProps({ stepIndex: 2, title: '第 3 步 · 接入上游账号' })
    await nextTick()
    expect(document.body.querySelector('[data-testid="guide-pill"]')).toBeNull()
    expect(document.body.querySelector('.pg-coach')?.textContent).toContain('第 3 步')
    w.unmount()
  })

  it('胶囊可以手动展开', async () => {
    const w = mountCoach({ collapseOnPrimary: true })
    await nextTick()
    document.body.querySelector<HTMLButtonElement>('.pg-btn.primary')!.click()
    await nextTick()
    document.body.querySelector<HTMLButtonElement>('[data-testid="guide-pill"]')!.click()
    await nextTick()
    expect(document.body.querySelector('.pg-coach')).not.toBeNull()
    w.unmount()
  })

  it('不开 collapseOnPrimary 时点主按钮保持展开（买家 Key 引导逐步推进）', async () => {
    const w = mountCoach()
    await nextTick()
    document.body.querySelector<HTMLButtonElement>('.pg-btn.primary')!.click()
    await nextTick()
    expect(w.emitted('primary')).toHaveLength(1)
    expect(document.body.querySelector('.pg-coach')).not.toBeNull()
    w.unmount()
  })
})
