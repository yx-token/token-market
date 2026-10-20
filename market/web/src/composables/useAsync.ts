import { ref, type Ref } from 'vue'
import { useToast } from '@/stores/toast'

/** 包装异步操作：维护 busy 状态，失败时弹出错误提示，成功时可选提示。 */
export function useAction() {
  const busy = ref(false)
  const toast = useToast()
  async function run<T>(fn: () => Promise<T>, success?: string): Promise<T | undefined> {
    if (busy.value) return undefined
    busy.value = true
    try {
      const v = await fn()
      if (success) toast.success(success)
      return v
    } catch (e) {
      toast.error(e)
      return undefined
    } finally {
      busy.value = false
    }
  }
  return { busy, run }
}

/** 加载数据：维护 loading 与 data，失败时弹出错误提示。 */
export function useLoader<T>(fn: () => Promise<T>, initial: T) {
  const data = ref(initial) as Ref<T>
  const loading = ref(false)
  const toast = useToast()
  async function load() {
    loading.value = true
    try {
      data.value = await fn()
    } catch (e) {
      toast.error(e)
    } finally {
      loading.value = false
    }
  }
  return { data, loading, load }
}
