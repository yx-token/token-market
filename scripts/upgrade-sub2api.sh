#!/usr/bin/env bash
# 受控升级 sub2api：先检查兼容性，冲突则拒绝升级；通过后执行升级命令，并等待 token-market 运行时自检恢复正常。
#
# 前提（docs/06-兼容性工具.md）：目标版本已在 token-market 的 contract.yaml 中登记，且新版 token-market 已经发布。
#
# 必填环境变量：
#   SUB2API_REPO         sub2api 本地 git 仓库（已 fetch 目标 tag）
#   TARGET_REF           目标版本的 git ref，例如 v0.2.15
#   TARGET_VERSION       目标版本号，例如 0.2.15（与 sub2api /admin/system/version 返回一致）
#   SUB2API_REPLICA_DSN  生产 sub2api 只读库 DSN（读取实际已执行的迁移）
#   MARKET_DSN           token-market 数据库 DSN（读取运行时自检状态）
#   UPGRADE_CMD          执行升级的命令，例如 "docker compose -f /srv/sub2api/compose.yml pull sub2api && docker compose -f /srv/sub2api/compose.yml up -d sub2api"
# 可选：
#   SCRATCH_DSN          空的临时库：在其上执行目标版本迁移并对契约查询做 EXPLAIN
#   STAGING_URL          已升级到目标版本的预发 sub2api 地址：探测契约中的接口是否仍然存在
#   WAIT_MINUTES         等待自检恢复正常的分钟数，默认 20
#   YES=1                跳过人工确认
set -euo pipefail

: "${SUB2API_REPO:?}" "${TARGET_REF:?}" "${TARGET_VERSION:?}" "${SUB2API_REPLICA_DSN:?}" "${MARKET_DSN:?}" "${UPGRADE_CMD:?}"
WAIT_MINUTES="${WAIT_MINUTES:-20}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPORT_DIR="${REPORT_DIR:-$ROOT/data/compat-reports}"
mkdir -p "$REPORT_DIR"
stamp="$(date +%Y%m%d-%H%M%S)"
compat=(go run "$ROOT/cmd/s2a-compat")

echo "==> 1/4 静态兼容检查：生产已执行迁移 → $TARGET_REF"
check_args=(check --sub2api-repo "$SUB2API_REPO" --target "$TARGET_REF" --replica-dsn "$SUB2API_REPLICA_DSN"
  --target-version "$TARGET_VERSION" --out "$REPORT_DIR/check-$stamp.md")
if [[ -n "${SCRATCH_DSN:-}" ]]; then
  check_args+=(--scratch-dsn "$SCRATCH_DSN")
fi
set +e
"${compat[@]}" "${check_args[@]}"
rc=$?
set -e
if [[ $rc -ne 0 ]]; then
  echo "!! 兼容检查未通过（退出码 $rc），拒绝升级。报告：$REPORT_DIR/check-$stamp.md" >&2
  echo "   处理方式：先修改 token-market（契约、查询、接入代码）并发布兼容新旧两个版本的版本，再重新执行本脚本。" >&2
  exit 1
fi
echo "    通过，报告：$REPORT_DIR/check-$stamp.md"

if [[ -n "${STAGING_URL:-}" ]]; then
  echo "==> 2/4 预发环境接口探测：$STAGING_URL"
  set +e
  "${compat[@]}" routes --base-url "$STAGING_URL" --out "$REPORT_DIR/routes-$stamp.md"
  rc=$?
  set -e
  if [[ $rc -ne 0 ]]; then
    echo "!! 接口探测未通过（退出码 $rc），拒绝升级。报告：$REPORT_DIR/routes-$stamp.md" >&2
    exit 1
  fi
  echo "    通过，报告：$REPORT_DIR/routes-$stamp.md"
else
  echo "==> 2/4 未设置 STAGING_URL，跳过接口探测（建议先在预发环境升级并探测）"
fi

echo "==> 3/4 执行升级：$UPGRADE_CMD"
if [[ "${YES:-}" != "1" ]]; then
  read -r -p "确认升级生产 sub2api 到 $TARGET_REF？输入 yes 继续：" answer
  [[ "$answer" == "yes" ]] || { echo "已取消"; exit 1; }
fi
bash -c "$UPGRADE_CMD"

echo "==> 4/4 等待 token-market 运行时自检恢复正常（最多 $WAIT_MINUTES 分钟）"
deadline=$(( $(date +%s) + WAIT_MINUTES * 60 ))
while true; do
  row="$(psql "$MARKET_DSN" -Atc "SELECT guard_state || '|' || COALESCE(sub2api_version, '') || '|' || COALESCE(reason, '') FROM compat_state WHERE id = 1" || true)"
  IFS='|' read -r state version reason <<< "$row"
  if [[ "$state" == "normal" && "$version" == "$TARGET_VERSION" ]]; then
    echo "    自检正常：sub2api $version，写入已恢复"
    exit 0
  fi
  if (( $(date +%s) > deadline )); then
    echo "!! 超时：自检状态 $state，版本 '$version'，原因：$reason" >&2
    echo "   写入 sub2api 的操作保持暂停（出站队列会在恢复后继续发送），请排查或回滚 sub2api。" >&2
    exit 2
  fi
  echo "    当前：$state ${version:+(版本 $version)} ${reason:+- $reason}，30 秒后重试"
  sleep 30
done
