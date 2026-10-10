<script setup lang="ts">
import { computed } from 'vue'
import { platToApi, type MarketAction } from '@/market/config'

const props = defineProps<{
  modelValue: MarketAction
  /** 可选标签（来自市场分类树） */
  tags?: { id: string; name: string }[]
}>()

const emit = defineEmits<{ 'update:modelValue': [MarketAction] }>()

const plats = Object.keys(platToApi)
const kinds = [
  { value: '', label: '不限形态' },
  { value: 'channel', label: '按量通道' },
  { value: 'pack', label: '额度包' },
  { value: 'all', label: '全部供给' },
]

function patch(partial: Partial<MarketAction>) {
  emit('update:modelValue', { ...props.modelValue, ...partial })
}

const summary = computed(() => {
  const a = props.modelValue
  const parts: string[] = []
  if (a.plat) parts.push(a.plat)
  if (a.q) parts.push(`搜「${a.q}」`)
  if (a.tag) parts.push(`#${a.tag}`)
  if (a.kindTab) parts.push(kinds.find((k) => k.value === a.kindTab)?.label || a.kindTab)
  if (a.href) parts.push(a.href)
  return parts.length ? parts.join(' · ') : '点击后不跳转筛选'
})
</script>

<template>
  <div class="af">
    <select class="input" :value="modelValue.plat ?? ''" @change="patch({ plat: ($event.target as HTMLSelectElement).value || undefined })">
      <option value="">厂商（可选）</option>
      <option v-for="p in plats" :key="p" :value="p">{{ p }}</option>
      <option value="保障池">保障池</option>
    </select>
    <select class="input" :value="modelValue.kindTab ?? ''" @change="patch({ kindTab: ($event.target as HTMLSelectElement).value || undefined })">
      <option v-for="k in kinds" :key="k.value || 'none'" :value="k.value">{{ k.label }}</option>
    </select>
    <select
      v-if="tags?.length"
      class="input"
      :value="modelValue.tag ?? ''"
      @change="patch({ tag: ($event.target as HTMLSelectElement).value || undefined })"
    >
      <option value="">标签（可选）</option>
      <option v-for="t in tags" :key="t.id" :value="t.id">{{ t.name }}</option>
    </select>
    <input
      v-else
      class="input"
      :value="modelValue.tag ?? ''"
      placeholder="标签 id"
      @input="patch({ tag: ($event.target as HTMLInputElement).value || undefined })"
    />
    <input
      class="input"
      :value="modelValue.q ?? ''"
      placeholder="搜索词"
      @input="patch({ q: ($event.target as HTMLInputElement).value || undefined })"
    />
    <input
      class="input"
      :value="modelValue.href ?? ''"
      placeholder="链接 /seller/publish"
      @input="patch({ href: ($event.target as HTMLInputElement).value || undefined })"
    />
    <p class="af-sum">{{ summary }}</p>
  </div>
</template>

<style scoped>
.af {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(7rem, 1fr));
  gap: 0.35rem;
  width: 100%;
}
.af-sum {
  grid-column: 1 / -1;
  margin: 0;
  font-size: 11px;
  color: #94a3b8;
}
</style>
