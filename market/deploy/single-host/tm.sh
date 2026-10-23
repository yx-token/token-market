#!/usr/bin/env bash
# 闲猫宿主（sub2api）与 token-market 的运维入口（说明见同目录 README.md）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"

sub2api() { docker compose -f "$ROOT/sub2api/docker-compose.yml" --env-file "$ROOT/sub2api/.env" "$@"; }
market() { docker compose -f "$ROOT/docker-compose.market.yml" -f "$ROOT/market.override.yml" --env-file "$ROOT/market.env" "$@"; }

case "${1:-}" in
  sub2api) shift; sub2api "$@" ;;
  market) shift; market "$@" ;;
  status) sub2api ps; echo; market ps ;;
  *)
    cat <<USAGE
用法：
  $0 status                 两套服务的运行状态
  $0 sub2api <compose 命令>  例如：$0 sub2api logs -f sub2api
  $0 market <compose 命令>   例如：$0 market up -d、$0 market logs -f edge
USAGE
    ;;
esac
