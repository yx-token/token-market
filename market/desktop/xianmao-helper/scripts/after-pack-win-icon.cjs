/**
 * Windows 打包时若 signAndEditExecutable=false（避免 wine 签名卡住），
 * electron-builder 不会把图标写进 exe，快捷方式会变成 Electron 默认原子图标。
 * 这里用纯 JS 的 resedit 直接改 PE 资源，不依赖 wine。
 */
const fs = require('fs')
const path = require('path')

exports.default = async function afterPack(context) {
  if (context.electronPlatformName !== 'win32') return

  const ResEdit = require('resedit')
  const exeName = `${context.packager.appInfo.productFilename}.exe`
  const candidates = [
    path.join(context.appOutDir, 'XianmaoHelper.exe'),
    path.join(context.appOutDir, exeName),
  ]
  const exePath = candidates.find((p) => fs.existsSync(p))
  if (!exePath) {
    console.warn('[after-pack-win-icon] exe not found in', context.appOutDir)
    return
  }

  const icoPath = path.join(__dirname, '..', 'build', 'icon.ico')
  if (!fs.existsSync(icoPath)) {
    console.warn('[after-pack-win-icon] missing', icoPath)
    return
  }

  const buf = fs.readFileSync(exePath)
  const executable = ResEdit.NtExecutable.from(buf, { ignoreCert: true })
  const resource = ResEdit.NtExecutableResource.from(executable)
  const groups = ResEdit.Resource.IconGroupEntry.fromEntries(resource.entries)
  if (!groups.length) {
    console.warn('[after-pack-win-icon] no icon group in exe')
    return
  }
  const iconFile = ResEdit.Data.IconFile.from(fs.readFileSync(icoPath))
  const icons = iconFile.icons.map((item) => item.data)
  for (const group of groups) {
    ResEdit.Resource.IconGroupEntry.replaceIconsForResource(resource.entries, group.id, group.lang, icons)
  }

  // 浏览器唤起协议时显示「闲猫助手」，而不是默认的「Electron」
  try {
    const versionInfo = ResEdit.Resource.VersionInfo.fromEntries(resource.entries)
    if (versionInfo.length >= 1) {
      const vi = versionInfo[0]
      const langs = vi.getAllLanguagesForStringValues()
      const lang = langs[0] || { lang: 1033, codepage: 1200 }
      // 浏览器「要打开 xxx 吗？」主要看 FileDescription / ProductName；不要留 Electron
      for (const [key, value] of [
        ['ProductName', '闲猫'],
        ['FileDescription', '闲猫'],
        ['InternalName', 'XianmaoHelper'],
        ['OriginalFilename', 'XianmaoHelper.exe'],
        ['CompanyName', '闲猫'],
      ]) {
        vi.setStringValues(lang, { [key]: value })
      }
      vi.outputToResourceEntries(resource.entries)
    }
  } catch (err) {
    console.warn('[after-pack-win-icon] version info skip', err instanceof Error ? err.message : err)
  }

  resource.outputResource(executable)
  fs.writeFileSync(exePath, Buffer.from(executable.generate()))

  // 额外放一份 ico，方便排查 / 部分工具读 resources
  try {
    const resDir = path.join(context.appOutDir, 'resources')
    fs.mkdirSync(resDir, { recursive: true })
    fs.copyFileSync(icoPath, path.join(resDir, 'icon.ico'))
  } catch {
    /* ignore */
  }
  console.log(
    '[after-pack-win-icon] embedded',
    path.basename(icoPath),
    '->',
    path.basename(exePath),
    `(${groups.length} group(s), ${icons.length} sizes)`,
  )
}
