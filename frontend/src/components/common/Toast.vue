<template>
  <Teleport to="body">
    <div
      class="pointer-events-none fixed left-1/2 top-16 z-[9999] flex w-[min(440px,calc(100vw-24px))] -translate-x-1/2 flex-col items-center gap-2.5"
      aria-live="polite"
      aria-atomic="true"
    >
      <TransitionGroup
        enter-active-class="transition ease-out duration-300"
        enter-from-class="opacity-0 -translate-y-2"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition ease-in duration-200"
        leave-from-class="opacity-100 translate-y-0"
        leave-to-class="opacity-0 -translate-y-2"
      >
        <div
          v-for="toast in toasts"
          :key="toast.id"
          :class="[
            'pointer-events-auto flex w-fit max-w-full cursor-pointer items-start gap-2.5 overflow-hidden rounded-[20px] border border-black/5 bg-white/95 px-3 py-3 pr-4 shadow-[0_10px_28px_rgba(0,0,0,0.1)] backdrop-blur-md',
            'dark:border-white/10 dark:bg-dark-800/95'
          ]"
          @click="removeToast(toast.id)"
        >
          <div
            :class="[
              'mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-full',
              getMarkBg(toast.type)
            ]"
          >
            <Icon
              :name="getToastIconName(toast.type)"
              size="sm"
              :class="getIconColor(toast.type)"
              aria-hidden="true"
            />
          </div>
          <div class="min-w-0 flex-1 pt-0.5">
            <p v-if="toast.title" class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ toast.title }}
            </p>
            <p
              :class="[
                'text-[13.5px] font-semibold leading-snug text-gray-800 dark:text-gray-100',
                toast.title ? 'mt-0.5 font-medium text-gray-600 dark:text-gray-300' : ''
              ]"
            >
              {{ toast.message }}
            </p>
          </div>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const toasts = computed(() => appStore.toasts)

const getToastIconName = (type: string): 'checkCircle' | 'xCircle' | 'exclamationTriangle' | 'infoCircle' => {
  switch (type) {
    case 'success':
      return 'checkCircle'
    case 'error':
      return 'xCircle'
    case 'warning':
      return 'exclamationTriangle'
    case 'info':
    default:
      return 'infoCircle'
  }
}

const getIconColor = (type: string): string => {
  const colors: Record<string, string> = {
    success: 'text-green-500',
    error: 'text-red-500',
    warning: 'text-yellow-500',
    info: 'text-blue-500'
  }
  return colors[type] || colors.info
}

const getMarkBg = (type: string): string => {
  const colors: Record<string, string> = {
    success: 'bg-emerald-500/15',
    error: 'bg-red-500/15',
    warning: 'bg-amber-300/40',
    info: 'bg-[#fff3a8]'
  }
  return colors[type] || colors.info
}

const removeToast = (id: string) => {
  appStore.hideToast(id)
}
</script>
