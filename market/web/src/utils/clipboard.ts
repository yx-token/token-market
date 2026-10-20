// 复制到剪贴板：优先用异步 Clipboard API，非 HTTPS（局域网 IP、内网域名）或权限被拒时退回 execCommand。
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 落到下面的兜底方案
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;padding:0;border:0;opacity:0'
  document.body.appendChild(ta)
  try {
    ta.select()
    ta.setSelectionRange(0, ta.value.length)
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    document.body.removeChild(ta)
  }
}
