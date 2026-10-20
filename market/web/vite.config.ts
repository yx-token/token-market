import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// 开发时把 /api 代理到本地 market-api（默认 :8080，可用 TM_API_URL 覆盖）
export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    port: 5173,
    proxy: { '/api': { target: process.env.TM_API_URL ?? 'http://127.0.0.1:8080', changeOrigin: false } },
  },
  test: { environment: 'jsdom', globals: true },
})
