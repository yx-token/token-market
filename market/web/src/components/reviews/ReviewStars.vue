<script setup lang="ts">
defineProps<{
  modelValue?: number
  value?: number
  readonly?: boolean
  size?: 'sm' | 'md'
}>()
const emit = defineEmits<{ 'update:modelValue': [n: number] }>()
</script>

<template>
  <div class="rev-stars" :class="[`is-${size || 'md'}`, { 'is-ro': readonly }]" role="img" :aria-label="`${value ?? modelValue ?? 0} 星`">
    <button
      v-for="n in 5"
      :key="n"
      type="button"
      class="rev-star"
      :class="{ on: n <= (readonly ? (value ?? 0) : (modelValue ?? 0)) }"
      :disabled="readonly"
      :aria-label="`${n} 星`"
      @click="!readonly && emit('update:modelValue', n)"
    >
      ★
    </button>
  </div>
</template>

<style scoped>
.rev-stars {
  display: inline-flex;
  gap: 2px;
  line-height: 1;
}
.rev-star {
  border: 0;
  background: transparent;
  padding: 0;
  color: #d4d4d4;
  cursor: pointer;
  font-size: 18px;
}
.rev-stars.is-sm .rev-star {
  font-size: 14px;
}
.rev-star.on {
  color: #f59e0b;
}
.rev-stars.is-ro .rev-star {
  cursor: default;
}
</style>
