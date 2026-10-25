import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('PlatformTypeBadge Kimi', () => {
  it.each([['oauth', 'OAuth'], ['apikey', 'Key']] as const)('renders Kimi %s', (type, label) => {
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'kimi', type }, global: { stubs: { PlatformIcon: true } } })
    expect(wrapper.text()).toContain('Kimi')
    expect(wrapper.text()).toContain(label)
    wrapper.unmount()
  })
})


describe('PlatformTypeBadge GLM', () => {
  it.each([['oauth', 'OAuth'], ['apikey', 'Key']] as const)('renders GLM %s', (type, label) => {
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'zhipu', type }, global: { stubs: { PlatformIcon: true } } })
    expect(wrapper.text()).toContain('Zhipu GLM')
    expect(wrapper.text()).toContain(label)
    wrapper.unmount()
  })
})
