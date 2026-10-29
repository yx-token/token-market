<script setup lang="ts">
import { useToast } from '@/stores/toast'

const toast = useToast()

const kindClass: Record<string, string> = {
  success: 'is-ok',
  error: 'is-err',
  info: 'is-info',
}
</script>

<template>
  <!-- 唯一提示出口：Teleport 到 body，顶部水平居中，避免被布局挤到底部 -->
  <Teleport to="body">
    <div class="xm-toast-host" aria-live="polite" aria-atomic="false">
      <TransitionGroup name="xm-toast">
        <div
          v-for="t in toast.items"
          :key="t.id"
          class="xm-toast"
          :class="kindClass[t.kind]"
          role="status"
          @click="toast.dismiss(t.id)"
        >
          <span class="xm-toast__mark" aria-hidden="true">
            <svg v-if="t.kind === 'success'" viewBox="0 0 20 20" fill="none">
              <path d="M5 10.5 8.2 13.5 15 6.5" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <svg v-else-if="t.kind === 'error'" viewBox="0 0 20 20" fill="none">
              <path d="M6.5 6.5 13.5 13.5M13.5 6.5 6.5 13.5" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
            </svg>
            <svg v-else viewBox="0 0 20 20" fill="none">
              <circle cx="10" cy="10" r="7" stroke="currentColor" stroke-width="2" />
              <path d="M10 8.2v5.2M10 6.2v.2" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
            </svg>
          </span>
          <p class="xm-toast__text">{{ t.text }}</p>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style>
/* 全局样式：不依赖页面壳，所有入口共用同一视觉 */
.xm-toast-host {
  pointer-events: none;
  position: fixed;
  /* 中间上方：顶栏下方，水平居中 */
  top: max(64px, env(safe-area-inset-top, 0px) + 56px);
  left: 50%;
  transform: translateX(-50%);
  z-index: 10000;
  display: flex;
  width: min(440px, calc(100vw - 24px));
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.xm-toast {
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: fit-content;
  max-width: 100%;
  margin: 0 auto;
  padding: 12px 18px 12px 12px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow:
    0 10px 28px rgba(0, 0, 0, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(16px) saturate(1.4);
  -webkit-backdrop-filter: blur(16px) saturate(1.4);
  color: #3a3a3a;
  font-family: "DM Sans", "PingFang SC", "Hiragino Sans GB", sans-serif;
  cursor: pointer;
}
.xm-toast__mark {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 999px;
  background: #fff3a8;
  color: #3a3a3a;
}
.xm-toast__mark svg {
  width: 16px;
  height: 16px;
}
.xm-toast.is-ok .xm-toast__mark {
  background: rgba(52, 179, 106, 0.16);
  color: #1f8f4e;
}
.xm-toast.is-err .xm-toast__mark {
  background: rgba(240, 113, 113, 0.16);
  color: #d45353;
}
.xm-toast.is-info .xm-toast__mark {
  background: #fff3a8;
  color: #3a3a3a;
}
.xm-toast__text {
  margin: 0;
  padding-top: 3px;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.45;
  letter-spacing: 0.01em;
  word-break: break-word;
}
.xm-toast-enter-active,
.xm-toast-leave-active {
  transition: opacity 0.22s ease, transform 0.22s ease;
}
.xm-toast-enter-from,
.xm-toast-leave-to {
  opacity: 0;
  transform: translateY(-8px) scale(0.98);
}
.xm-toast-move {
  transition: transform 0.22s ease;
}

/* 控制台深色工作台：略提高对比，位置不变 */
html.is-console .xm-toast {
  background: rgba(255, 255, 255, 0.98);
  border-color: rgba(5, 85, 206, 0.12);
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.14);
}
</style>
