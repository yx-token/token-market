#!/usr/bin/env bash
# token-market 与 sub2api 的运维入口（说明见同目录 README.md）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"

sub2api() { docker compose -f "$ROOT/sub2api/docker-compose.yml" --env-file "$ROOT/sub2api/.env" "$@"; }
market() { docker compose -f "$ROOT/market-src/deploy/docker-compose.yml" -f "$ROOT/market.override.yml" --env-file "$ROOT/market.env" "$@"; }

# 用 market-src 的源码构建 token-market 镜像，并把 market.env 的 TM_IMAGE 指向它
build() {
  local version commit
  version="$(cat "$ROOT/market-src/VERSION")"
  commit="$(cat "$ROOT/market-src/COMMIT")"
  docker build --build-arg VERSION="$version" --build-arg COMMIT="$commit" -t "token-market:$version" "$ROOT/market-src"
  sed -i "s#^TM_IMAGE=.*#TM_IMAGE=token-market:$version#" "$ROOT/market.env"
  echo "镜像 token-market:$version 已构建，market.env 已指向它；执行 $0 market up -d 生效"
}

case "${1:-}" in
  sub2api) shift; sub2api "$@" ;;
  market) shift; market "$@" ;;
  build) build ;;
  status) sub2api ps; echo; market ps ;;
  *)
    cat <<USAGE
用法：
  $0 status                 两套服务的运行状态
  $0 sub2api <compose 命令>  例如：$0 sub2api logs -f sub2api
  $0 market <compose 命令>   例如：$0 market up -d、$0 market logs -f api worker edge
  $0 build                  用 market-src 构建 token-market 镜像
USAGE
    ;;
esac
