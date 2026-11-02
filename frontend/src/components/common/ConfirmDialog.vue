<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="narrow"
    :close-on-escape="!busy"
    :close-on-click-outside="false"
    @close="handleCancel"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ message }}</p>
      <slot></slot>
      <div v-if="reason" class="space-y-1">
        <label :for="reasonInputId" class="block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ reasonLabelText }}
        </label>
        <textarea
          :id="reasonInputId"
          v-model="reasonValue"
          rows="3"
          :disabled="busy"
          :placeholder="reasonPlaceholder"
          class="y-textarea w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
        ></textarea>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end space-x-3">
        <button
          @click="handleCancel"
          type="button"
          :disabled="busy"
          class="rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200 dark:hover:bg-dark-600 dark:focus:ring-offset-dark-800"
        >
          {{ cancelText }}
        </button>
        <button
          @click="handleConfirm"
          type="button"
          :disabled="busy"
          :class="[
            'rounded-md px-4 py-2 text-sm font-medium text-white focus:outline-none focus:ring-2 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:focus:ring-offset-dark-800',
            danger
              ? 'bg-red-600 hover:bg-red-700 focus:ring-red-500'
              : 'bg-primary-600 hover:bg-primary-700 focus:ring-primary-500'
          ]"
        >
          {{ busy ? busyText : confirmText }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'

const { t } = useI18n()

interface Props {
  show: boolean
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
  /** 异步确认进行中：禁用按钮、阻止 Escape 关闭，由调用方控制 show */
  busy?: boolean
  busyText?: string
  /** 是否展示原因输入框（用于替代 window.prompt 的危险操作原因采集） */
  reason?: boolean
  reasonLabel?: string
  reasonPlaceholder?: string
  /** 原因输入框初始值，用于“编辑已有原因”场景 */
  reasonInitialValue?: string
}

interface Emits {
  (e: 'confirm', reason?: string): void
  (e: 'cancel'): void
}

const props = withDefaults(defineProps<Props>(), {
  danger: false,
  busy: false,
  reason: false,
  reasonInitialValue: ''
})

const confirmText = computed(() => props.confirmText || t('common.confirm'))
const cancelText = computed(() => props.cancelText || t('common.cancel'))
const busyText = computed(() => props.busyText || t('common.saving'))
const reasonLabelText = computed(() => props.reasonLabel || '原因 / Reason')

const emit = defineEmits<Emits>()

const reasonValue = ref(props.reasonInitialValue)
const reasonInputId = `confirm-reason-${Math.random().toString(36).substring(2, 9)}`

// 每次打开时重置原因，避免上一次输入残留。
watch(
  () => props.show,
  (open) => {
    if (open) reasonValue.value = props.reasonInitialValue
  }
)

const handleConfirm = () => {
  if (props.busy) return
  if (props.reason) emit('confirm', reasonValue.value)
  else emit('confirm')
}

const handleCancel = () => {
  if (props.busy) return
  emit('cancel')
}
</script>
