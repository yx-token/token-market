#!/usr/bin/env bash
# 与镜像共用构建阶段，输出三进程和前端的完整更新包。
set -euo pipefail
release_tag="${RELEASE_TAG:?RELEASE_TAG is required}"
release_commit="${RELEASE_COMMIT:?RELEASE_COMMIT is required}"
if [[ ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "无效的发布版本号" >&2
  exit 1
fi
mkdir -p release-assets
staging_root="$(mktemp -d)"
trap 'rm -rf "$staging_root"' EXIT
for arch in amd64 arm64; do
  bundle="$staging_root/$arch"
  docker buildx build --platform "linux/$arch" --target release \
    --file market/Dockerfile \
    --output "type=local,dest=$bundle" \
    --build-arg VERSION="$release_tag" --build-arg COMMIT="$release_commit" \
    market

  for binary in api worker edge s2a-compat; do test -s "$bundle/bin/$binary"; done
  test -s "$bundle/web/index.html"
  tar -C "$bundle" -czf "release-assets/token-market_linux_$arch.tar.gz" \
    manifest.json bin/api bin/worker bin/edge bin/s2a-compat web
done
cd release-assets
sha256sum token-market_linux_amd64.tar.gz token-market_linux_arm64.tar.gz > checksums.txt
