<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { marketJson } from './marketApi'

const { t, locale } = useI18n()
const localeListSep = computed(() => (String(locale.value || '').toLowerCase().startsWith('zh') ? '、' : ', '))

type Listing = {
  listing_id: number
  platform: string
  title?: string
  price: number
  health?: string
  grade?: string
  mode?: string
  sku_kind?: string
  requests_24h?: number
  success_rate_24h?: number | null
  categories?: string[]
}

const route = useRoute()
const router = useRouter()
const rows = ref<Listing[]>([])
const hot = ref<string[]>([])
const error = ref('')
const loading = ref(true)
const platform = ref(typeof route.query.platform === 'string' ? route.query.platform : '')

const platforms = computed(() => {
  const set = new Set(rows.value.map((r) => r.platform).filter(Boolean))
  return [...set]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const q = platform.value ? `?platform=${encodeURIComponent(platform.value)}` : ''
    const [list, discovery] = await Promise.all([
      marketJson<Listing[]>(`/api/market/listings${q}`),
      marketJson<Record<string, unknown>>('/api/market/discovery').catch((): Record<string, unknown> => ({})),
    ])
    rows.value = Array.isArray(list) ? list : []
    const raw = discovery['hot_searches']
    hot.value = Array.isArray(raw)
      ? raw
          .map((v) => {
            if (typeof v === 'string' || typeof v === 'number') return String(v)
            if (v && typeof v === 'object') {
              const row = v as Record<string, unknown>
              if (row.label != null) return String(row.label)
              if (row.keyword != null) return String(row.keyword)
              if (row.q != null) return String(row.q)
            }
            return ''
          })
          .filter(Boolean)
      : []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function pickPlatform(name: string) {
  if (name === platform.value) return
  platform.value = name
  router.replace({ path: '/shop', query: name ? { platform: name } : {} })
  load()
}

watch(
  () => (typeof route.query.platform === 'string' ? route.query.platform : ''),
  (next) => {
    if (next === platform.value) return
    platform.value = next
    load()
  }
)

onMounted(load)
</script>

<template>
  <AppLayout>
    <section class="space-y-4">
      <header>
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('nav.idleMarket') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('nav.marketShelfDesc') }}
          <RouterLink class="text-primary-600 dark:text-primary-400" to="/shop/center">{{ t('nav.marketCenter') }}</RouterLink>
          ·
          <a class="text-primary-600 dark:text-primary-400" href="/" :title="t('nav.backToMarketHint')">{{ t('nav.marketFullClientHint') }}</a>
        </p>
      </header>

      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="rounded border px-3 py-1.5 text-sm"
          :class="
            !platform
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-300'
              : 'border-gray-200 bg-white text-gray-600 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300'
          "
          @click="pickPlatform('')"
        >
          {{ t('common.all') }}
        </button>
        <button
          v-for="name in platforms"
          :key="name"
          type="button"
          class="rounded border px-3 py-1.5 text-sm"
          :class="
            platform === name
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-300'
              : 'border-gray-200 bg-white text-gray-600 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300'
          "
          @click="pickPlatform(name)"
        >
          {{ name }}
        </button>
      </div>

      <p v-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="loading" class="text-sm text-gray-500 dark:text-gray-400">{{ t('nav.loading') }}</p>
      <p v-else-if="hot.length" class="text-xs text-gray-500 dark:text-gray-400">{{ t('nav.hotSearches') }}：{{ hot.join(localeListSep) }}</p>

      <div class="overflow-x-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3 font-medium">{{ t('nav.listingCol') }}</th>
              <th class="px-4 py-3 font-medium">{{ t('nav.platformCol') }}</th>
              <th class="px-4 py-3 font-medium">{{ t('nav.priceMultiplierCol') }}</th>
              <th class="px-4 py-3 font-medium">{{ t('nav.healthCol') }}</th>
              <th class="px-4 py-3 font-medium">24h</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in rows"
              :key="r.listing_id"
              class="border-t border-gray-100 text-gray-900 dark:border-dark-800 dark:text-gray-100"
            >
              <td class="px-4 py-3">
                <RouterLink class="font-medium text-primary-600 dark:text-primary-400" :to="`/shop/listings/${r.listing_id}`">
                  {{ r.title || `#${r.listing_id}` }}
                </RouterLink>
                <div class="text-xs text-gray-500 dark:text-gray-400">{{ r.sku_kind || r.mode || '—' }}</div>
              </td>
              <td class="px-4 py-3">{{ r.platform }}</td>
              <td class="px-4 py-3">{{ r.price }}</td>
              <td class="px-4 py-3">{{ r.health || r.grade || '—' }}</td>
              <td class="px-4 py-3 text-xs text-gray-500 dark:text-gray-400">
                {{ t('nav.requestsCount', { n: r.requests_24h ?? 0 }) }}
                <span v-if="r.success_rate_24h != null"> · {{ Math.round(r.success_rate_24h * 100) }}%</span>
              </td>
            </tr>
            <tr v-if="!loading && !rows.length">
              <td colspan="5" class="px-4 py-6 text-gray-500 dark:text-gray-400">{{ t('nav.noListings') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </AppLayout>
</template>
