import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { usePopoverPosition } from '@/composables/usePopoverPosition'

describe('共享浮层定位兼容性', () => {
  it.each([undefined, 304, 688])('保留原有默认宽度及日期组件固定宽度 %s', async (width) => {
    const wrapper = mount(defineComponent({
      setup() {
        const trigger = ref<HTMLElement | null>(null)
        const position = usePopoverPosition(trigger, { width })
        return { trigger, ...position }
      },
      render() { return h('button', { ref: 'trigger' }) },
    }))
    vi.spyOn(wrapper.get('button').element, 'getBoundingClientRect').mockReturnValue(new DOMRect(100, 100, 200, 36))
    await wrapper.vm.open()
    expect(wrapper.vm.style.width).toBe(`${width ?? 680}px`)
    wrapper.unmount()
    vi.restoreAllMocks()
  })
})
