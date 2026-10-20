<script setup lang="ts">
import PageHeader from '@/components/PageHeader.vue'

defineProps<{ id: string; title: string; description?: string }>()
</script>

<template>
  <div :id="id" class="min-w-0">
    <PageHeader :title="title" :description="description">
      <slot name="actions" />
    </PageHeader>
    <div class="card">
      <form v-if="$slots.filters" class="mb-5 grid grid-cols-1 items-end gap-4 border-b border-ink-200 pb-5 sm:grid-cols-[minmax(0,1fr)_9rem_auto]" @submit.prevent>
        <slot name="filters" />
      </form>
      <slot />
      <div v-if="$slots.footer" class="mt-4 flex flex-wrap items-center justify-end gap-2 border-t border-ink-200 pt-4">
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>
