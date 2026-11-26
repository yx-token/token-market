const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('xianmaoHelper', {
  onProgress(cb) {
    const handler = (_event, payload) => cb(payload)
    ipcRenderer.on('one-click:progress', handler)
    // 告诉主进程：监听已挂好，可以冲刷缓冲进度
    void ipcRenderer.invoke('one-click:ui-ready')
    return () => ipcRenderer.removeListener('one-click:progress', handler)
  },
  openExternal(url) {
    return ipcRenderer.invoke('one-click:open-external', url)
  },
  openApp(which) {
    return ipcRenderer.invoke('one-click:open-app', which)
  },
  openCli(which, bin) {
    return ipcRenderer.invoke('one-click:open-cli', which, bin)
  },
  openAgent(clientId) {
    return ipcRenderer.invoke('one-click:open-agent', clientId)
  },
  preflight(clientId) {
    return ipcRenderer.invoke('one-click:preflight', clientId)
  },
  setupAgent(clientId, opts) {
    return ipcRenderer.invoke('one-click:setup-agent', clientId, opts || {})
  },
  uninstallAgent(clientId) {
    return ipcRenderer.invoke('one-click:uninstall-agent', clientId)
  },
  vendors(clientId) {
    return ipcRenderer.invoke('one-click:vendors', clientId)
  },
  agents() {
    return ipcRenderer.invoke('one-click:agents')
  },
  installed() {
    return ipcRenderer.invoke('one-click:installed')
  },
  account() {
    return ipcRenderer.invoke('one-click:account')
  },
  site() {
    return ipcRenderer.invoke('one-click:site')
  },
  setSite(api) {
    return ipcRenderer.invoke('one-click:set-site', api)
  },

  wallet() {
    return ipcRenderer.invoke('one-click:wallet')
  },
  openRecharge() {
    return ipcRenderer.invoke('one-click:open-recharge')
  },
  rechargeOptions() {
    return ipcRenderer.invoke('one-click:recharge-options')
  },
  createRecharge(amount, channel) {
    return ipcRenderer.invoke('one-click:create-recharge', amount, channel)
  },
  getRecharge(id) {
    return ipcRenderer.invoke('one-click:get-recharge', id)
  },
  openPayURL(url) {
    return ipcRenderer.invoke('one-click:open-external', url)
  },
  logoutAccount() {
    return ipcRenderer.invoke('one-click:logout-account')
  },
  loginAccount() {
    return ipcRenderer.invoke('one-click:login-account')
  },
  cancelLogin() {
    return ipcRenderer.invoke('one-click:cancel-login')
  },
  cancel() {
    return ipcRenderer.invoke('one-click:cancel')
  },
})
