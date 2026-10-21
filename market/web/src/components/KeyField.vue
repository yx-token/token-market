<script setup lang="ts">
// 密钥 / 证书输入：可以粘贴内容，也可以从本地文件读取（文件只在浏览器内读取，随表单提交）。
import { ref } from 'vue'

defineProps<{ id: string; label: string; placeholder?: string; hint?: string; rows?: number }>()
const model = defineModel<string>({ default: '' })
const file = ref<HTMLInputElement | null>(null)

function pick(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => (model.value = String(reader.result ?? '').trim())
  reader.readAsText(f)
  ;(e.target as HTMLInputElement).value = ''
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between">
      <label class="label" :for="id">{{ label }}</label>
      <button type="button" class="btn-ghost btn-sm" @click="file?.click()">从文件读取</button>
      <input ref="file" type="file" class="hidden" accept=".pem,.crt,.cer,.txt,.key" @change="pick" />
    </div>
    <textarea :id="id" v-model="model" class="input font-mono text-xs" :rows="rows ?? 4" :placeholder="placeholder" spellcheck="false" />
    <p v-if="hint" class="hint">{{ hint }}</p>
  </div>
</template>
