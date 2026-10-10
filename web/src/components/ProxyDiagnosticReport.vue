<script setup lang="ts">
import type { Proxy } from '@/api/types'
import { dateTime } from '@/utils/format'
import { useIsConsole } from '@/composables/useIsConsole'
defineProps<{ proxy: Proxy }>()
const isConsole = useIsConsole()
const statusText: Record<string, string> = { pass: '通过', warn: '警告', fail: '失败', challenge: '人机验证' }
const targets: Record<string, string> = { base_connectivity: '基础连通性', http_client: '检测客户端', openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini', grok: 'Grok' }
</script>

<template>
  <div class="space-y-6">
    <section>
      <h3 :class="isConsole ? 'mb-3 font-semibold' : 'card-title'">连通性与出口地区</h3>
      <template v-if="proxy.connection_report">
        <p class="break-words text-sm leading-relaxed" :class="proxy.connection_report.success ? 'text-emerald-700' : 'text-red-600'">{{ proxy.connection_report.success ? '连接成功' : '连接失败' }} · {{ proxy.connection_report.message }}</p>
        <dl class="kv mt-3">
          <dt>出口 IP</dt><dd class="font-mono">{{ proxy.connection_report.ip_address || '—' }}</dd>
          <dt>检测地区</dt><dd>{{ [proxy.connection_report.country, proxy.connection_report.region, proxy.connection_report.city].filter(Boolean).join(' / ') || '—' }}</dd>
          <dt>连接延迟</dt><dd>{{ proxy.connection_report.success ? `${proxy.connection_report.latency_ms} ms` : '—' }}</dd>
          <dt>检测时间</dt><dd>{{ dateTime(proxy.connection_checked_at) }}</dd>
        </dl>
      </template>
      <p v-else class="text-sm text-ink-500">尚未检测连通性。</p>
    </section>
    <section v-if="proxy.quality_report" class="border-t border-ink-300 pt-5">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h3 :class="isConsole ? 'font-semibold' : 'card-title !mb-0'">质量报告</h3>
        <span class="chip chip-ink font-mono">{{ proxy.quality_report.grade }} · {{ proxy.quality_report.score }} 分</span>
      </div>
      <p class="text-sm leading-relaxed">{{ proxy.quality_report.summary }}</p>
      <p class="mt-2 text-sm text-ink-600">通过 {{ proxy.quality_report.passed_count }} · 警告 {{ proxy.quality_report.warn_count }} · 失败 {{ proxy.quality_report.failed_count }} · 人机验证 {{ proxy.quality_report.challenge_count }}</p>
      <dl class="kv mt-3">
        <dt>出口 IP / 地区</dt><dd>{{ proxy.quality_report.exit_ip || '—' }} / {{ proxy.quality_report.country || proxy.quality_report.country_code || '—' }}</dd>
        <dt>基础延迟</dt><dd>{{ proxy.quality_report.exit_ip ? `${proxy.quality_report.base_latency_ms} ms` : '—' }}</dd>
        <dt>检测时间</dt><dd>{{ dateTime(proxy.quality_checked_at) }}</dd>
      </dl>
      <ul class="mt-4 divide-y divide-dashed divide-ink-200 border-y border-ink-300">
        <li v-for="(item, index) in proxy.quality_report.items" :key="index" class="py-3 text-sm">
          <div class="flex flex-wrap justify-between gap-2">
            <span class="font-medium">{{ targets[item.target] || item.target }}</span>
            <span class="text-xs tabular-nums" :class="item.status === 'pass' ? 'text-emerald-700' : item.status === 'fail' ? 'text-red-600' : 'text-amber-700'">{{ statusText[item.status] || item.status }} · {{ item.latency_ms }} ms<span v-if="item.http_status"> · HTTP {{ item.http_status }}</span></span>
          </div>
          <p v-if="item.message" class="mt-1 break-words text-ink-600">{{ item.message }}</p>
          <p v-if="item.cf_ray" class="mt-1 font-mono text-xs text-ink-500">CF Ray: {{ item.cf_ray }}</p>
        </li>
      </ul>
      <p class="hint mt-3">报告反映出口连通性及 AI 站点可达性，不代表账号凭证或订阅有效。结果可能随网络和站点策略变化。</p>
    </section>
    <p v-else class="text-sm text-ink-500">尚未生成质量报告，请运行质量检测。</p>
  </div>
</template>

<style scoped>
.kv dd {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
