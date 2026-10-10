<script setup lang="ts">
// 登录 / 注册：闲猫黄壳，对齐市场品牌（非账本印章页）
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="xm-auth mx-auto grid max-w-5xl grid-cols-1 overflow-hidden rounded-[18px] border border-black/8 bg-white md:grid-cols-[1.05fr_1fr]">
    <div class="xm-auth-hero relative hidden flex-col justify-between p-10 md:flex">
      <div>
        <div class="flex items-center gap-2.5 text-[28px] font-black tracking-tight text-[#303030]">
          <img src="/assets/xianmao-pet.svg" alt="" width="36" height="36" />
          闲猫
        </div>
        <p class="mt-10 text-[28px] font-extrabold leading-[1.35] tracking-tight text-[#1f1f1f]">
          用不完的订阅，<br />有人正需要。
        </p>
        <p class="mt-4 max-w-xs text-[14px] leading-[1.75] text-[#555]">
          同一个账号既能买也能卖：按官方价打折调用大模型，或者把闲置额度挂出来结算收益。
        </p>
      </div>
      <img
        class="xm-auth-pet pointer-events-none absolute bottom-4 right-2 w-[160px] opacity-95"
        src="/assets/xianmao-pet-lie.svg"
        alt=""
      />
    </div>
    <div class="p-8 md:p-12">
      <div class="mb-3 text-[13px] font-extrabold text-[#888]">闲猫 · Token 集市</div>
      <h1 class="text-[28px] font-extrabold tracking-tight text-[#1f1f1f]">{{ title }}</h1>
      <p v-if="subtitle" class="mt-2 text-[14px] text-[#666]">{{ subtitle }}</p>
      <div class="mt-8"><slot /></div>
    </div>
  </div>
</template>

<style scoped>
.xm-auth-hero {
  background: #ffe60f;
  min-height: 420px;
}
</style>
