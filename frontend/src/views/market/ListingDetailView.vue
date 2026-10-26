<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { marketJson, marketSessionToken } from './marketApi'

type Listing = {
  listing_id: number
  seller_id: number
  platform: string
  title?: string
  description?: string
  price: number
  health?: string
  grade?: string
  mode?: string
  sku_kind?: string
  face_value?: number
  min_amount?: number
  stock?: number
  stock_sold?: number
  requests_24h?: number
  success_rate_24h?: number | null
}

const route = useRoute()
const item = ref<Listing | null>(null)
const error = ref('')
const msg = ref('')
const loading = ref(true)
const busy = ref(false)

const loggedIn = computed(() => !!marketSessionToken())
const title = computed(() => item.value?.title || (item.value ? `挂单 #${item.value.listing_id}` : '挂单'))

async function load() {
  loading.value = true
  error.value = ''
  try {
    item.value = await marketJson<Listing>(`/api/market/listings/${route.params.id}`)
  } catch (e) {
    item.value = null
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function joinMerchant() {
  if (!item.value) return
  msg.value = ''
  error.value = ''
  busy.value = true
  try {
    await marketJson(`/api/buyer/merchants/${item.value.seller_id}`, { method: 'PUT' })
    msg.value = '已加入商家清单'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加入失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <AppLayout>
    <section class="space-y-4">
      <header>
        <a class="text-sm text-primary-600 dark:text-primary-400" href="/">返回市场</a>
        <h1 class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ title }}</h1>
      </header>
      <p v-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="loading" class="text-sm text-gray-500">加载中…</p>
      <div v-else-if="item" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
        <dl class="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt class="text-xs text-gray-500">平台</dt>
            <dd class="mt-1 text-gray-900 dark:text-gray-100">{{ item.platform }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">价格倍率</dt>
            <dd class="mt-1 text-gray-900 dark:text-gray-100">{{ item.price }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">健康</dt>
            <dd class="mt-1 text-gray-900 dark:text-gray-100">{{ item.health || item.grade || '—' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">24h</dt>
            <dd class="mt-1 text-gray-900 dark:text-gray-100">
              {{ item.requests_24h ?? 0 }} 次
              <span v-if="item.success_rate_24h != null"> · {{ Math.round(item.success_rate_24h * 100) }}%</span>
            </dd>
          </div>
        </dl>
        <p class="mt-4 text-sm text-gray-600 dark:text-gray-300">
          {{ item.description || '先加入商家清单，再到买家 Key 页选择挂单。' }}
        </p>
        <p v-if="msg" class="mt-3 text-sm text-green-600 dark:text-green-400">{{ msg }}</p>
        <div class="mt-4 flex gap-3">
          <button
            v-if="loggedIn"
            class="rounded bg-primary-600 px-3 py-1.5 text-sm text-white disabled:opacity-60"
            type="button"
            :disabled="busy"
            @click="joinMerchant"
          >
            {{ busy ? '加入中…' : '加入商家' }}
          </button>
          <a v-else class="text-sm text-primary-600 dark:text-primary-400" href="/login">登录后加入商家</a>
        </div>
      </div>
    </section>
  </AppLayout>
</template>
