import { describe, expect, it, vi, afterEach } from 'vitest'
import { nextTick, ref } from 'vue'
import { usePopoverPosition } from '@/composables/usePopoverPosition'

afterEach(() => {
  vi.restoreAllMocks()
})

function mockRect(el: HTMLElement, rect: Partial<DOMRect>) {
  vi.spyOn(el, 'getBoundingClientRect').mockReturnValue({
    x: 0,
    y: 0,
    top: 100,
    left: 40,
    bottom: 140,
    right: 340,
    width: 300,
    height: 40,
    toJSON() {
      return {}
    },
    ...rect,
  } as DOMRect)
}

describe('usePopoverPosition', () => {
  it('未指定 width 时与触发器同宽，不会变成 0px', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    mockRect(trigger, { width: 300, left: 40, right: 340 })
    const triggerRef = ref<HTMLElement | null>(trigger)
    const { style, open, close } = usePopoverPosition(triggerRef, { maxHeight: 320 })
    await open()
    await nextTick()
    expect(style.value.width).toBe('300px')
    expect(style.value.left).toBe('40px')
    close()
    trigger.remove()
  })

  it('显式 width ≤0 时仍回退到触发器宽度', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    mockRect(trigger, { width: 280, left: 20, right: 300 })
    const triggerRef = ref<HTMLElement | null>(trigger)
    const { style, open, close } = usePopoverPosition(triggerRef, { width: 0 })
    await open()
    expect(style.value.width).toBe('280px')
    close()
    trigger.remove()
  })

  it('显式正数 width 优先生效', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    mockRect(trigger, { width: 200, left: 10, right: 210 })
    const triggerRef = ref<HTMLElement | null>(trigger)
    const { style, open, close } = usePopoverPosition(triggerRef, { width: 304 })
    await open()
    expect(style.value.width).toBe('304px')
    close()
    trigger.remove()
  })
})
