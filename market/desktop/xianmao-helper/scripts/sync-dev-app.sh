#!/usr/bin/env bash
# 把 Electron.app 复制成真正的「闲猫助手.app」，替换图标与 bundle id。
# 壳脚本 exec Electron 时，程序坞仍显示 Electron 默认图标；必须跑这份拷贝的二进制。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/node_modules/electron/dist/Electron.app"
APP="$ROOT/dist/闲猫助手.app"
ICON_ICNS="$ROOT/build/icon.icns"
STAMP_FILE="$APP/Contents/Resources/.xianmao-dev-stamp"
ELECTRON_VER="$(/usr/bin/node -p "require('$ROOT/node_modules/electron/package.json').version" 2>/dev/null || echo unknown)"
WANT_STAMP="electron=${ELECTRON_VER};icon=$(/sbin/md5 -q "$ICON_ICNS" 2>/dev/null || echo noicon)"

if [[ ! -d "$SRC" ]]; then
  echo "未找到 Electron.app，先 npm install" >&2
  exit 1
fi
if [[ ! -f "$ICON_ICNS" ]]; then
  echo "缺少 $ICON_ICNS，请先生成闲猫 icon.icns" >&2
  exit 1
fi

NEED_SYNC=1
if [[ -x "$APP/Contents/MacOS/Electron" && -f "$STAMP_FILE" ]]; then
  if [[ "$(cat "$STAMP_FILE")" == "$WANT_STAMP" ]]; then
    NEED_SYNC=0
  fi
fi

if [[ "$NEED_SYNC" == "1" ]]; then
  echo "[xianmao-helper] 同步开发 App（闲猫图标）…"
  rm -rf "$APP"
  mkdir -p "$ROOT/dist"
  /usr/bin/ditto "$SRC" "$APP"

  # 闲猫图标：替换 electron.icns，并额外放一份 icon.icns
  cp "$ICON_ICNS" "$APP/Contents/Resources/electron.icns"
  cp "$ICON_ICNS" "$APP/Contents/Resources/icon.icns"

  PLIST="$APP/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier com.xianmao.helper.dev" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleName 闲猫助手" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName 闲猫助手" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleIconFile electron.icns" "$PLIST" 2>/dev/null \
    || /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string electron.icns" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString 0.1.0" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion 0.1.0" "$PLIST"

  # xianmao:// URL scheme
  /usr/libexec/PlistBuddy -c "Delete :CFBundleURLTypes" "$PLIST" 2>/dev/null || true
  /usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes array" "$PLIST"
  /usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0 dict" "$PLIST"
  /usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLName string Xianmao Helper" "$PLIST"
  /usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes array" "$PLIST"
  /usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string xianmao" "$PLIST"

  printf '%s' "$WANT_STAMP" > "$STAMP_FILE"
fi

# 关键：协议 / 双击启动时系统不会带上「源码目录」参数。
# 若没有 Resources/app，Electron 会加载 default_app.asar → 欢迎页（用户看到的白底 Electron 说明）。
# 把开发目录挂成 app，打开 闲猫助手.app 或 xianmao:// 即跑真助手。
APP_LINK="$APP/Contents/Resources/app"
if [[ -L "$APP_LINK" || -e "$APP_LINK" ]]; then
  rm -rf "$APP_LINK"
fi
ln -sfn "$ROOT" "$APP_LINK"
