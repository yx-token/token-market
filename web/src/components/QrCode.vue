<script setup lang="ts">
// 把文本渲染成二维码图片（本地生成，不经过第三方服务）。
import { ref, watchEffect } from 'vue'
import QRCode from 'qrcode'

const props = withDefaults(defineProps<{ value: string; size?: number }>(), { size: 220 })
const src = ref('')

watchEffect(async () => {
  try {
    src.value = await QRCode.toDataURL(props.value, { width: props.size, margin: 1, errorCorrectionLevel: 'M', color: { dark: '#17140F', light: '#FCFAF5' } })
  } catch {
    src.value = ''
  }
})
</script>

<template>
  <!-- 取景框：四角折线 -->
  <div v-if="src" class="relative p-3">
    <span class="absolute left-0 top-0 h-5 w-5 border-l-2 border-t-2 border-ink-900" />
    <span class="absolute right-0 top-0 h-5 w-5 border-r-2 border-t-2 border-ink-900" />
    <span class="absolute bottom-0 left-0 h-5 w-5 border-b-2 border-l-2 border-ink-900" />
    <span class="absolute bottom-0 right-0 h-5 w-5 border-b-2 border-r-2 border-ink-900" />
    <img :src="src" :width="size" :height="size" alt="支付二维码" :data-qr="value" class="block bg-white" />
  </div>
</template>
