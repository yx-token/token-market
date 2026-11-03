import { computed, ref } from 'vue'

export type GuideKind = 'seller-publish' | 'buyer-key'

const STORAGE_KEY = 'market-product-guide-v1'

const active = ref(false)
const kind = ref<GuideKind | null>(null)
/** buyer-key 子步骤；seller 跟随 PublishView.step */
const subStep = ref(0)

function persist() {
  try {
    if (!active.value || !kind.value) {
      sessionStorage.removeItem(STORAGE_KEY)
      return
    }
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ kind: kind.value, subStep: subStep.value }))
  } catch {
    /* ignore */
  }
}

export function restoreProductGuide() {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const o = JSON.parse(raw) as { kind?: GuideKind; subStep?: number }
    if (o.kind === 'seller-publish' || o.kind === 'buyer-key') {
      kind.value = o.kind
      subStep.value = Number(o.subStep) || 0
      active.value = true
    }
  } catch {
    /* ignore */
  }
}

export function startProductGuide(next: GuideKind, step = 0) {
  kind.value = next
  subStep.value = step
  active.value = true
  persist()
}

export function stopProductGuide() {
  active.value = false
  kind.value = null
  subStep.value = 0
  persist()
}

export function setGuideSubStep(n: number) {
  subStep.value = Math.max(0, n)
  persist()
}

export function useProductGuide() {
  return {
    active: computed(() => active.value),
    kind: computed(() => kind.value),
    subStep: computed(() => subStep.value),
    start: startProductGuide,
    stop: stopProductGuide,
    setSubStep: setGuideSubStep,
    restore: restoreProductGuide,
  }
}
