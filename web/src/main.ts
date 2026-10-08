import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { setUnauthorizedHandler } from './api/client'
import { useAuth } from './stores/auth'
import './style.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)

// 登录态失效（令牌过期或账号被冻结）时回到登录页
setUnauthorizedHandler(() => {
  useAuth().logout()
  if (router.currentRoute.value.name !== 'login') {
    router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
  }
})

app.mount('#app')
