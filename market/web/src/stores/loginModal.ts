import { defineStore } from 'pinia'
import { ref } from 'vue'

/** 闲猫进站登录弹窗（对齐原型 market-home.html） */
export const useLoginModal = defineStore('loginModal', () => {
  const open = ref(false)
  const redirect = ref('/')
  const mode = ref<'login' | 'register'>('login')

  function show(opts?: { redirect?: string; mode?: 'login' | 'register' }) {
    redirect.value = opts?.redirect || '/'
    mode.value = opts?.mode || 'login'
    open.value = true
  }

  function hide() {
    open.value = false
  }

  return { open, redirect, mode, show, hide }
})
