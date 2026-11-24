#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/dist/闲猫助手.app"
ELECTRON_SRC="$ROOT/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"
SETTER_SRC="$ROOT/scripts/set-url-handler.m"
SETTER_BIN="$ROOT/scripts/set-url-handler"
USER_DATA="$HOME/Library/Application Support/闲猫助手"

if [[ ! -x "$ELECTRON_SRC" ]]; then
  echo "未找到 Electron，先 npm install" >&2
  exit 1
fi

# 关掉旧进程（源码 Electron + 开发包装 App）
pkill -f "$ROOT/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron" 2>/dev/null || true
pkill -f "$ROOT/dist/闲猫助手.app/Contents/MacOS/Electron" 2>/dev/null || true
sleep 0.4

# 生成真正的闲猫助手.app（含闲猫 icns，程序坞才不会是 Electron 白图标）
bash "$ROOT/scripts/sync-dev-app.sh"

LSREGISTER="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
if [[ -x "$LSREGISTER" ]]; then
  "$LSREGISTER" -f "$APP" >/dev/null 2>&1 || true
fi
if [[ ! -x "$SETTER_BIN" || "$SETTER_SRC" -nt "$SETTER_BIN" ]]; then
  clang -framework CoreServices -framework Foundation -o "$SETTER_BIN" "$SETTER_SRC" 2>/dev/null || true
fi
if [[ -x "$SETTER_BIN" ]]; then
  "$SETTER_BIN" xianmao com.xianmao.helper.dev >/dev/null 2>&1 || true
fi

# RAW=1：直接跑源码树里的 Electron（调试用，程序坞仍可能是默认图标）
if [[ "${RAW:-}" == "1" ]]; then
  exec "$ELECTRON_SRC" "$ROOT" --user-data-dir="$USER_DATA" "$@"
fi

# 用带闲猫图标的 App 启动；把助手源码目录作为第一个参数传给 Electron
open -n -a "$APP" --args "$ROOT" --user-data-dir="$USER_DATA" "$@"
