import { nextTick, onUnmounted, ref, type Ref } from 'vue'

/** 将浮层挂到 body 时，按触发器位置计算 fixed 坐标，避免被 overflow 裁切。
 *  width 未传或 ≤0 时与触发器同宽（勿把 0 当成有效宽度，否则会出现 0px 竖线）。 */
export function usePopoverPosition(
  triggerRef: Ref<HTMLElement | null>,
  opts: { width?: number; gap?: number; maxHeight?: number } = {},
) {
  const gap = opts.gap ?? 8
  const style = ref<Record<string, string>>({
    position: 'fixed',
    top: '0',
    left: '0',
    zIndex: '3000',
  })

  function update() {
    const el = triggerRef.value
    if (!el) return
    const rect = el.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    const prefer = opts.width && opts.width > 0 ? opts.width : rect.width
    const width = Math.min(Math.max(prefer, 1), vw - 16)
    let left = rect.left
    if (left + width > vw - 8) left = Math.max(8, vw - width - 8)
    if (left < 8) left = 8

    const spaceBelow = vh - rect.bottom - gap
    const spaceAbove = rect.top - gap
    const estimatedH = opts.maxHeight ?? 420
    const placeAbove = spaceBelow < Math.min(estimatedH, 320) && spaceAbove > spaceBelow

    if (placeAbove) {
      style.value = {
        position: 'fixed',
        left: `${left}px`,
        bottom: `${vh - rect.top + gap}px`,
        top: 'auto',
        zIndex: '3000',
        width: `${width}px`,
      }
    } else {
      style.value = {
        position: 'fixed',
        left: `${left}px`,
        top: `${rect.bottom + gap}px`,
        bottom: 'auto',
        zIndex: '3000',
        width: `${width}px`,
      }
    }
  }

  async function open() {
    await nextTick()
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
  }

  function close() {
    window.removeEventListener('resize', update)
    window.removeEventListener('scroll', update, true)
  }

  onUnmounted(close)

  return { style, update, open, close }
}
