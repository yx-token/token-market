#!/usr/bin/env bash
# 打包闲猫助手（macOS + Windows）。产物放到 dist/，下载 API 按固定文件名查找。
# 需要：Node 18+；打 Windows 包可在 macOS 上交叉编译 zip（NSIS 需 wine，可选）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 国内网络拉 GitHub Electron 常失败/下残包，默认走 npmmirror
export ELECTRON_MIRROR="${ELECTRON_MIRROR:-https://npmmirror.com/mirrors/electron/}"
export ELECTRON_BUILDER_BINARIES_MIRROR="${ELECTRON_BUILDER_BINARIES_MIRROR:-https://npmmirror.com/mirrors/electron-builder-binaries/}"

if [[ ! -d node_modules/electron ]]; then
  echo "[pack] npm install…"
  npm install
fi

OS_LIST="${1:-mac,win}"
mkdir -p dist

pack_mac() {
  echo "[pack] macOS zip/dmg…"
  npx electron-builder --mac zip dmg --x64 --arm64
  # 稳定别名：下载接口优先认这个名字
  local zip
  zip="$(ls -1t dist/xianmao-helper-mac-*.zip 2>/dev/null | head -1 || true)"
  if [[ -n "$zip" ]]; then
    cp -f "$zip" dist/xianmao-helper-macos.zip
    echo "[pack] -> dist/xianmao-helper-macos.zip"
  fi
}

pack_win() {
  echo "[pack] Windows zip (x64)…"
  # 跳过 winCodeSign/wine；zip 可在 macOS 交叉产出
  export CSC_IDENTITY_AUTO_DISCOVERY=false
  if ! npx electron-builder --win zip --x64; then
    echo "[pack] electron-builder zip 失败，尝试打包已有 win-unpacked…" >&2
  fi
  local zip
  zip="$(ls -1t dist/xianmao-helper-win-*.zip 2>/dev/null | head -1 || true)"
  if [[ -z "$zip" && -x dist/win-unpacked/XianmaoHelper.exe ]]; then
    echo "[pack] 用 zip 压缩 win-unpacked…"
    rm -f dist/xianmao-helper-windows-x64.zip
    (cd dist/win-unpacked && zip -qry ../xianmao-helper-windows-x64.zip . -x "*.DS_Store" -x "__MACOSX/*")
    zip="dist/xianmao-helper-windows-x64.zip"
  fi
  if [[ -n "$zip" ]]; then
    cp -f "$zip" dist/xianmao-helper-windows-x64.zip
    echo "[pack] -> dist/xianmao-helper-windows-x64.zip"
  else
    echo "[pack] Windows zip 失败" >&2
    return 1
  fi
  if command -v wine64 >/dev/null 2>&1 || command -v wine >/dev/null 2>&1; then
    echo "[pack] Windows NSIS setup…"
    npx electron-builder --win nsis --x64 || echo "[pack] NSIS 跳过（wine 打包失败）"
    local setup
    setup="$(ls -1t dist/xianmao-helper-windows-setup-*.exe 2>/dev/null | head -1 || true)"
    if [[ -n "$setup" ]]; then
      cp -f "$setup" dist/xianmao-helper-windows-setup.exe
      echo "[pack] -> dist/xianmao-helper-windows-setup.exe"
    fi
  else
    echo "[pack] 未安装 wine，跳过 NSIS（仍可提供 zip 便携包）"
  fi
}

pack_linux() {
  echo "[pack] Linux AppImage…"
  npx electron-builder --linux AppImage --x64 || return 1
  local img
  img="$(ls -1t dist/xianmao-helper-linux-*.AppImage 2>/dev/null | head -1 || true)"
  if [[ -n "$img" ]]; then
    cp -f "$img" dist/xianmao-helper-linux-x64.AppImage
    echo "[pack] -> dist/xianmao-helper-linux-x64.AppImage"
  fi
}

IFS=',' read -r -a items <<<"$OS_LIST"
for item in "${items[@]}"; do
  case "$(echo "$item" | tr '[:upper:]' '[:lower:]' | tr -d ' ')" in
    mac|macos|darwin) pack_mac ;;
    win|windows) pack_win ;;
    linux) pack_linux ;;
    all)
      pack_mac
      pack_win
      pack_linux
      ;;
    *)
      echo "未知目标: $item（mac|win|linux|all）" >&2
      exit 1
      ;;
  esac
done

echo "[pack] done"
ls -lah dist/xianmao-helper-* 2>/dev/null || true
