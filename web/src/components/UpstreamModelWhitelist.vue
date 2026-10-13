<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { seller } from '@/api'
import type { Upstream } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import ModelWhitelistSelector from '@/components/ModelWhitelistSelector.vue'

const props = defineProps<{ upstream: Upstream }>()
const emit = defineEmits<{ changed: [] }>()
const { busy, run } = useAction()
const editing = ref(false)
const valid = ref(false)
const models = ref<string[]>([])
const whitelist = ref<string[] | null>(null)
const selecting = computed(() => props.upstream.status === 'selecting_models')
const editable = computed(() => !['draft', 'authorizing', 'failed', 'removing', 'removed'].includes(props.upstream.status))

watch(() => props.upstream, (u) => {
  whitelist.value = u.model_whitelist ?? null
  if (u.status === 'selecting_models') editing.value = true
}, { immediate: true })

async function save() {
  if (!valid.value) return
  const updated = await run(() => seller.setUpstreamModels(props.upstream.id, [...models.value]), '挂售模型已保存')
  if (!updated) return
  whitelist.value = updated.model_whitelist ?? [...models.value]
  editing.value = false
  emit('changed')
}
</script>

<template>
  <section class="card">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h2 class="card-title !mb-0">挂售模型</h2>
      <button v-if="editable && !editing" type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="editing = true">{{ whitelist === null ? '补填挂售模型' : '修改白名单' }}</button>
    </div>
    <p v-if="selecting" class="callout callout-info mb-3">待选择挂售模型。保存白名单后开始上架检测，检测通过后可发布商品。</p>
    <p v-else-if="whitelist === null && editable" class="callout callout-warn mb-3">尚未声明挂售模型，目前不限制模型请求。请补填白名单，明确可供买家调用的模型。</p>
    <template v-if="editing && editable">
      <ModelWhitelistSelector v-model="models" :upstream-id="upstream.id" :disabled="busy" @valid="valid = $event" />
      <div class="mt-3 flex flex-wrap gap-2">
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy || !valid" @click="save">{{ selecting ? '确认模型并开始检测' : '保存白名单' }}</button>
        <button v-if="!selecting" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="editing = false">取消</button>
      </div>
    </template>
    <ul v-else-if="whitelist?.length" class="flex flex-wrap gap-2" aria-label="已挂售模型">
      <li v-for="model in whitelist" :key="model" class="border border-ink-200 px-2 py-1 font-mono text-xs break-all">{{ model }}</li>
    </ul>
    <p v-else class="text-sm text-ink-500">{{ editable ? '未声明挂售模型' : '完成账号接入后可选择挂售模型。' }}</p>
  </section>
</template>
