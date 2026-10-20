import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError } from '@/api/client'

export interface Toast {
  id: number
  kind: 'success' | 'error' | 'info'
  text: string
}

export const useToast = defineStore('toast', () => {
  const items = ref<Toast[]>([])
  let seq = 0

  function push(kind: Toast['kind'], text: string, ms = 4000) {
    const id = ++seq
    items.value.push({ id, kind, text })
    setTimeout(() => dismiss(id), ms)
  }

  function dismiss(id: number) {
    items.value = items.value.filter((t) => t.id !== id)
  }

  return {
    items,
    dismiss,
    success: (text: string) => push('success', text),
    info: (text: string) => push('info', text),
    error: (err: unknown) => push('error', err instanceof ApiError ? err.text : err instanceof Error ? err.message : String(err), 6000),
  }
})
