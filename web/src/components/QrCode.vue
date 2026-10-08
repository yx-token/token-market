<script setup lang="ts">
// 把文本渲染成二维码图片（本地生成，不经过第三方服务）。
import { ref, watchEffect } from 'vue'
import QRCode from 'qrcode'

const props = withDefaults(defineProps<{ value: string; size?: number }>(), { size: 220 })
const src = ref('')

watchEffect(async () => {
  try {
    src.value = await QRCode.toDataURL(props.value, { width: props.size, margin: 1, errorCorrectionLevel: 'M' })
  } catch {
    src.value = ''
  }
})
</script>

<template>
  <img v-if="src" :src="src" :width="size" :height="size" alt="支付二维码" :data-qr="value" class="rounded border border-slate-200" />
</template>
