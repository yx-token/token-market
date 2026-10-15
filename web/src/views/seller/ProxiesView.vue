<script setup lang="ts">
import ProxyInventory from '@/components/ProxyInventory.vue'
import '@/styles/seller-center.css'
</script>

<template>
  <div class="seller-page">
    <p class="seller-callout">
      连通性检测只说明出口能用，和账号质量检测不是一回事。代理通过后，账号仍可在未做质量检测时挂寄售或额度包。每个代理同时只绑一个账号。
    </p>
    <ProxyInventory scope="seller" />
  </div>
</template>
