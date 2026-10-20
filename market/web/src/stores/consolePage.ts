import { computed, ref } from 'vue'

/** 控制台顶栏标题（对齐 YourHub AppHeader：标题在顶栏，不在内容区重复） */
const title = ref('')
const description = ref('')

export function useConsolePage() {
  function setMeta(t: string, d = '') {
    title.value = t
    description.value = d
  }

  function clearMeta() {
    title.value = ''
    description.value = ''
  }

  return {
    title: computed(() => title.value),
    description: computed(() => description.value),
    setMeta,
    clearMeta,
  }
}
