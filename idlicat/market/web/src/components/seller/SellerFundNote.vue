<script setup lang="ts">
import { computed } from 'vue'
import { useConfig } from '@/stores/config'
import { freezeTimingText, resolveFreezeHours } from './funds'

const cfg = useConfig()
const hours = computed(() => resolveFreezeHours(cfg.config))
const rate = computed(() => cfg.config.credit_usd_to_balance_rate)
</script>

<template>
  <aside class="seller-callout seller-callout-warn" data-testid="seller-fund-note">
    <p class="seller-kicker">资金怎么结算</p>
    <p>
      买家付款和你的待结算，在调用完成后就分开记下，不用等到提现日。可提现还要过冻结期。
      当前{{ freezeTimingText(hours) }}平台每分钟按最新小时数解冻，后台改完下一轮就生效。
    </p>
    <p>
      1 个收益单位 = 1 元。平台检测消耗你自己的额度时，也会按规则计入收益或标明由你承担。
      提现前需要实名，到账仍走审核。
    </p>
    <p v-if="rate">
      按当前兑换率，$1 活动额度可抵 ¥{{ rate }} 店铺消费。购买后按购买时兑换率使用，后续调整不影响已购包。
    </p>
  </aside>
</template>
