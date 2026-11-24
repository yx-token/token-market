<script setup lang="ts">
/**
 * 首页「一键使用」：探测 → 未装自动下载 → 短文案步骤条引导。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { useRoute } from 'vue-router'
import {
  detectOneClickOS,
  ensureHelperRunning,
  markHelperInstalled,
  preferHelperSite,
  probeHelper,
  syncHelperSession,
  type OneClickOS,
} from '@/utils/oneClickUse'

type HelperPhase =
  | 'checking'
  | 'download'
  | 'guide'
  | 'online'
  | 'need-login'
  | 'network-error'
  | 'unsupported'

type GuideStepState = 'pending' | 'active' | 'done'

const open = defineModel<boolean>('open', { default: false })

const auth = useAuth()
const loginModal = useLoginModal()
const route = useRoute()

const phase = ref<HelperPhase>('checking')
const os = ref<OneClickOS>(detectOneClickOS())
const statusLine = ref('')
const progress = ref(0)
const downloadedThisSession = ref(false)
const userConfirmedInstall = ref(false)
let cancelled = false
let running = false
let pollTimer: number | undefined

const osLabel = computed(() => {
  if (os.value === 'windows') return 'Windows'
  if (os.value === 'linux') return 'Linux'
  return 'macOS'
})

/** macOS 门禁会把未公证包装成「已损坏」；先解压 zip，再清隔离（免 sudo） */
const MAC_QUARANTINE_CMD = 'xattr -cr ~/Downloads/闲猫助手.app'
const showMacGatekeeperTip = computed(
  () => os.value === 'macos' && (phase.value === 'guide' || phase.value === 'download'),
)
const macCmdCopied = ref(false)
let macCopyTimer: number | undefined

async function copyMacQuarantineCmd() {
  try {
    await navigator.clipboard.writeText(MAC_QUARANTINE_CMD)
    macCmdCopied.value = true
    if (macCopyTimer) window.clearTimeout(macCopyTimer)
    macCopyTimer = window.setTimeout(() => {
      macCmdCopied.value = false
    }, 2000)
  } catch {
    macCmdCopied.value = false
  }
}

/** 水平步骤条：短标签 */
const guideSteps = computed(() => {
  const online = phase.value === 'online'
  const guiding = phase.value === 'guide'
  const downloading = phase.value === 'download'
  const step2Done = userConfirmedInstall.value || online
  const step3Done = online

  const items: { id: string; label: string; state: GuideStepState }[] = [
    {
      id: 'download',
      label: '下载助手',
      state:
        downloadedThisSession.value || online
          ? 'done'
          : downloading
            ? 'active'
            : 'pending',
    },
    {
      id: 'install',
      label: '安装打开',
      state: step2Done ? 'done' : guiding ? 'active' : 'pending',
    },
    {
      id: 'login',
      label: '登录闲猫',
      state: step3Done ? 'done' : guiding && step2Done ? 'active' : 'pending',
    },
    {
      id: 'use',
      label: '一键使用',
      state: online ? 'done' : 'pending',
    },
  ]
  return items
})

const showStepper = computed(
  () =>
    phase.value === 'guide' ||
    phase.value === 'online' ||
    phase.value === 'download' ||
    phase.value === 'checking',
)

const leadText = computed(() => {
  if (phase.value === 'need-login') return '请先登录'
  if (phase.value === 'checking') return '正在检测…'
  if (phase.value === 'download') return '正在下载…'
  if (phase.value === 'guide') return '按步骤完成'
  if (phase.value === 'online') return '助手已就绪'
  if (phase.value === 'network-error') return '网络异常'
  if (phase.value === 'unsupported') return '暂不支持'
  return '助手帮你一键配置'
})

function stopPoll() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

async function pushSessionIfLoggedIn() {
  const apiBase = typeof location !== 'undefined' ? location.origin : ''
  void preferHelperSite(apiBase)
  if (!auth.user) return
  void syncHelperSession({
    userId: auth.user.id,
    account: auth.user.email || '',
    apiBase,
    retries: 2,
  })
}

async function finishOnline() {
  stopPoll()
  markHelperInstalled()
  phase.value = 'online'
  progress.value = 100
  statusLine.value = '去助手里选客户端'
  await pushSessionIfLoggedIn()
}

function startGuidePoll() {
  stopPoll()
  pollTimer = window.setInterval(() => {
    void (async () => {
      if (cancelled || phase.value !== 'guide') {
        stopPoll()
        return
      }
      if (await probeHelper(900)) await finishOnline()
    })()
  }, 2000)
}

async function onConfirmInstalled() {
  userConfirmedInstall.value = true
  statusLine.value = '正在确认…'
  if (await probeHelper(2000, 4)) {
    await finishOnline()
    return
  }
  statusLine.value = '若已打开可关闭'
  startGuidePoll()
}

async function runAutoBootstrap(opts?: { forceDownload?: boolean }) {
  if (running) return
  running = true
  cancelled = false
  stopPoll()
  os.value = detectOneClickOS()
  phase.value = 'checking'
  progress.value = 0
  userConfirmedInstall.value = false
  if (opts?.forceDownload) downloadedThisSession.value = false
  statusLine.value = '检测助手…'

  if (!auth.loggedIn) {
    phase.value = 'need-login'
    statusLine.value = '请先登录'
    loginModal.show({ redirect: route.fullPath })
    running = false
    return
  }

  try {
    if (!opts?.forceDownload && (await probeHelper(1800, 4))) {
      if (cancelled) return
      await finishOnline()
      return
    }

    await ensureHelperRunning({
      os: os.value,
      forceDownload: !!opts?.forceDownload,
      onProgress: (p, info) => {
        if (cancelled) return
        if (p === 'download') {
          downloadedThisSession.value = true
          phase.value = 'download'
          progress.value = info.percent
          statusLine.value = `下载中 ${info.percent}%`
          return
        }
        if (p === 'install') {
          downloadedThisSession.value = true
          progress.value = 100
          statusLine.value = '下载完成'
          return
        }
        phase.value = 'checking'
        statusLine.value = '正在打开…'
      },
    })
    if (cancelled) return
    await finishOnline()
  } catch (err) {
    if (cancelled) return
    if (await probeHelper(2000, 3)) {
      await finishOnline()
      return
    }
    const msg = err instanceof Error ? err.message : String(err)
    progress.value = 0
    if (msg === 'HELPER_OS_UNSUPPORTED') {
      phase.value = 'unsupported'
      statusLine.value = `${osLabel.value}包不可用`
      return
    }
    if (msg === 'INSTALL_GUIDE' || msg === 'INSTALL_PENDING') {
      downloadedThisSession.value = true
      markHelperInstalled()
      phase.value = 'guide'
      progress.value = 100
      statusLine.value = '请安装并打开'
      startGuidePoll()
      return
    }
    phase.value = 'network-error'
    statusLine.value = '请检查网络'
  } finally {
    running = false
  }
}

watch(open, async (v) => {
  if (!v) {
    cancelled = true
    stopPoll()
    return
  }
  downloadedThisSession.value = false
  userConfirmedInstall.value = false
  await runAutoBootstrap()
})

watch(
  () => auth.loggedIn,
  async (loggedIn) => {
    if (!open.value || !loggedIn) return
    await runAutoBootstrap()
  },
)

onBeforeUnmount(() => {
  cancelled = true
  stopPoll()
  if (macCopyTimer) window.clearTimeout(macCopyTimer)
})
</script>

<template>
  <ModalDialog v-model:open="open" title="闲猫助手">
    <div class="oc">
      <p class="oc-purpose">零基础一键装客户端、连闲猫</p>
      <p class="oc-lead">{{ leadText }}</p>

      <!-- 水平步骤条 -->
      <ol v-if="showStepper" class="oc-bar" aria-label="步骤">
        <li
          v-for="(s, idx) in guideSteps"
          :key="s.id"
          class="oc-bar-item"
          :class="{ 'is-done': s.state === 'done', 'is-active': s.state === 'active' }"
        >
          <div class="oc-bar-node">
            <span class="oc-bar-dot" aria-hidden="true">
              {{ s.state === 'done' ? '✓' : idx + 1 }}
            </span>
            <span v-if="idx < guideSteps.length - 1" class="oc-bar-line" aria-hidden="true" />
          </div>
          <span class="oc-bar-label">{{ s.label }}</span>
        </li>
      </ol>

      <div
        v-if="phase === 'download' || phase === 'checking'"
        class="oc-progress"
        role="progressbar"
        :aria-valuenow="progress"
        aria-valuemin="0"
        aria-valuemax="100"
      >
        <div class="oc-progress-bar" :style="{ width: `${Math.max(progress, phase === 'checking' ? 12 : 0)}%` }" />
      </div>

      <p
        class="oc-status"
        :class="{ 'is-error': phase === 'network-error' || phase === 'unsupported' }"
      >{{ statusLine }}</p>

      <div v-if="showMacGatekeeperTip" class="oc-mac-tip" role="note">
        <p class="oc-mac-tip-title">Mac 若提示「已损坏，无法打开」</p>
        <p class="oc-mac-tip-desc">
          先解压下载的 <code>xianmao-helper-macos.zip</code>，再打开「终端」粘贴下面命令回车，然后双击
          <code>闲猫助手.app</code>：
        </p>
        <div class="oc-mac-cmd-row">
          <code class="oc-mac-cmd">{{ MAC_QUARANTINE_CMD }}</code>
          <button type="button" class="xm-btn xm-btn-ghost oc-mac-copy" @click="copyMacQuarantineCmd">
            {{ macCmdCopied ? '已复制' : '复制' }}
          </button>
        </div>
        <p class="oc-mac-tip-desc oc-mac-tip-muted">
          若已拖到「应用程序」，把路径改成
          <code>xattr -cr "/Applications/闲猫助手.app"</code>
        </p>
      </div>

      <div v-if="phase === 'need-login'" class="oc-actions">
        <button type="button" class="xm-btn xm-btn-primary" @click="loginModal.show({ redirect: route.fullPath })">
          去登录
        </button>
      </div>
      <div v-else-if="phase === 'guide'" class="oc-actions">
        <button type="button" class="xm-btn xm-btn-primary" @click="onConfirmInstalled">我已打开</button>
        <button type="button" class="xm-btn xm-btn-ghost" @click="() => finishOnline()">已在运行</button>
        <button type="button" class="xm-btn xm-btn-ghost" @click="runAutoBootstrap({ forceDownload: true })">
          重下
        </button>
      </div>
      <div v-else-if="phase === 'online'" class="oc-actions">
        <button type="button" class="xm-btn xm-btn-primary" @click="open = false">去助手使用</button>
      </div>
      <div v-else-if="phase === 'network-error' || phase === 'unsupported'" class="oc-actions">
        <button type="button" class="xm-btn xm-btn-primary" @click="runAutoBootstrap()">重试</button>
        <button type="button" class="xm-btn xm-btn-ghost" @click="runAutoBootstrap({ forceDownload: true })">
          下载
        </button>
      </div>
      <div v-else-if="phase === 'checking' || phase === 'download'" class="oc-actions">
        <button type="button" class="xm-btn xm-btn-ghost" @click="open = false">取消</button>
      </div>
    </div>
  </ModalDialog>
</template>

<style scoped>
.oc {
  display: grid;
  gap: 14px;
  padding: 4px 2px 8px;
}
.oc-purpose {
  margin: 0;
  font-size: 13px;
  line-height: 1.4;
  color: #555;
  padding: 8px 12px;
  border-radius: 10px;
  background: #f5f5f5;
  border: 1px solid rgba(0, 0, 0, 0.06);
}
.oc-lead {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
  color: #1f1f1f;
}
.oc-status {
  margin: 0;
  font-size: 13px;
  line-height: 1.4;
  color: #666;
}
.oc-status.is-error {
  color: #b42318;
  font-weight: 600;
}

/* 水平步骤条：圆点 + 连线 */
.oc-bar {
  list-style: none;
  margin: 4px 0 0;
  padding: 8px 4px 0;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0;
}
.oc-bar-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: #999;
}
.oc-bar-node {
  position: relative;
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}
.oc-bar-dot {
  width: 28px;
  height: 28px;
  border-radius: 999px;
  display: grid;
  place-items: center;
  font-size: 12px;
  font-weight: 800;
  background: #eee;
  color: #888;
  border: 2px solid #ddd;
  position: relative;
  z-index: 1;
  flex: 0 0 auto;
}
.oc-bar-line {
  position: absolute;
  left: calc(50% + 14px);
  right: calc(-50% + 14px);
  top: 50%;
  height: 2px;
  background: #e0e0e0;
  transform: translateY(-50%);
  z-index: 0;
}
.oc-bar-label {
  font-size: 12px;
  font-weight: 600;
  text-align: center;
  line-height: 1.2;
  white-space: nowrap;
}
.oc-bar-item.is-active {
  color: #1f1f1f;
}
.oc-bar-item.is-active .oc-bar-dot {
  background: #1f1f1f;
  border-color: #1f1f1f;
  color: #fff;
}
.oc-bar-item.is-done {
  color: #1f5c3a;
}
.oc-bar-item.is-done .oc-bar-dot {
  background: #2f9e62;
  border-color: #2f9e62;
  color: #fff;
}
.oc-bar-item.is-done .oc-bar-line {
  background: #2f9e62;
}

.oc-progress {
  height: 6px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.06);
  overflow: hidden;
}
.oc-progress-bar {
  height: 100%;
  border-radius: inherit;
  background: #1f1f1f;
  transition: width 0.25s ease;
}
.oc-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

/* 弹窗可能不在 .xm-market 下，全局 xm-btn 吃不到；这里自带按钮外形 */
.oc-actions :deep(.xm-btn),
.oc-mac-tip :deep(.xm-btn),
.oc .xm-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  margin: 0;
  min-height: 40px;
  padding: 10px 16px;
  border-radius: 12px;
  border: 1px solid transparent;
  font: inherit;
  font-size: 14px;
  font-weight: 700;
  line-height: 1.2;
  cursor: pointer;
  text-decoration: none;
  color: #1f1f1f;
  background: #fff;
}
.oc .xm-btn-primary {
  background: #ffd400;
  border-color: #e6c000;
  color: #1f1f1f;
}
.oc .xm-btn-primary:hover {
  background: #f0c200;
}
.oc .xm-btn-ghost {
  background: #fff;
  border-color: rgba(0, 0, 0, 0.16);
  color: #1f1f1f;
}
.oc .xm-btn-ghost:hover {
  background: #f7f7f7;
}

.oc-mac-tip {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 10px;
  background: #fff8e8;
  border: 1px solid #f0d9a0;
}
.oc-mac-tip-title {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  color: #7a4b00;
}
.oc-mac-tip-desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.45;
  color: #5c4a2a;
}
.oc-mac-tip-muted {
  color: #8a7350;
}
.oc-mac-cmd-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.oc-mac-cmd {
  flex: 1 1 180px;
  margin: 0;
  padding: 8px 10px;
  border-radius: 8px;
  background: #1f1f1f;
  color: #f5f5f5;
  font-size: 11px;
  line-height: 1.35;
  word-break: break-all;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
.oc-mac-copy {
  flex: 0 0 auto;
  font-size: 12px;
}
.oc-mac-tip code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
}
</style>
