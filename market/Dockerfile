# token-market 镜像：同一镜像包含 market-api（含 C 端与管理端页面）、market-worker、market-edge 与 s2a-compat，
# 用不同的启动命令运行不同组件（见 deploy/docker-compose.yml）。
#
# 构建上下文是仓库根目录（管理端来自 frontend/）：docker build -f market/Dockerfile .
# 无法直连 Docker Hub 时可指定镜像源，例如：docker build --build-arg REGISTRY=docker.m.daocloud.io/ -f market/Dockerfile .
# 多架构：前端与 Go 都在构建机的架构上编译（Go 交叉编译），只有最后一层按目标架构组装，
# 所以 docker buildx build --platform linux/amd64,linux/arm64 不需要在模拟器里跑编译。
ARG REGISTRY=

FROM --platform=$BUILDPLATFORM ${REGISTRY}library/node:22-alpine AS web
WORKDIR /src/market/web
RUN corepack enable
COPY market/web/package.json market/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY market/web/ ./
RUN pnpm run build

# 管理端（frontend/）按同端口模式构建：路由 /admin/*、静态资源 /admin-assets，接口走 market-api 反代的 /api/v1。
# 输出到 ../market/engine-dist（见 frontend/vite.config.ts），随 C 端打包为 web/admin-spa。
FROM --platform=$BUILDPLATFORM ${REGISTRY}library/node:24-alpine AS admin-web
WORKDIR /src/frontend
RUN corepack enable && corepack prepare pnpm@9 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
# 合规确认页在构建期引用 ../docs/legal/*.md
COPY docs/legal/ /src/docs/legal/
RUN VITE_ENGINE_HOST=1 VITE_API_BASE_URL=/api/v1 pnpm run build

FROM --platform=$BUILDPLATFORM ${REGISTRY}library/golang:1.27-alpine AS go
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOWORK=off
COPY market/go.mod market/go.sum ./
RUN go mod download
COPY market/ ./
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X github.com/yx-token/token-market/market/internal/platform/buildinfo.Version=${VERSION} -X github.com/yx-token/token-market/market/internal/platform/buildinfo.Commit=${COMMIT}" \
      -o /out/ ./cmd/api ./cmd/worker ./cmd/edge ./cmd/s2a-compat ./cmd/market-runner
RUN printf '{"schema":1,"version":"%s","commit":"%s","os":"%s","arch":"%s"}\n' \
      "$VERSION" "$COMMIT" "$TARGETOS" "$TARGETARCH" > /manifest.json

# Release 更新包直接导出此阶段，复用镜像里的二进制和前端，不运行目标架构程序。
FROM scratch AS release
COPY --from=go /out/ /bin/
COPY --from=web /src/market/web/dist /web/
COPY --from=admin-web /src/market/engine-dist /web/admin-spa/
# 管理端按根路径引用 frontend/public 的图标（/logo.svg、/xianmao-logo.svg），C 端没有同名文件
COPY --from=admin-web /src/market/engine-dist/logo.svg /src/market/engine-dist/xianmao-logo.svg /web/
COPY --from=go /manifest.json /manifest.json

FROM ${REGISTRY}library/alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 market
WORKDIR /app
COPY --from=release / /app/
ENV TM_HTTP_WEB_DIR=/app/web TM_EDGE_SPOOL_DIR=/app/data/edge-spool TM_UPDATE_RUNTIME_DIR=/app/runtime TZ=Asia/Shanghai
RUN mkdir -p /app/data /app/runtime && chown market /app/data /app/runtime
USER market
EXPOSE 8080 8090 8091 8092
ENTRYPOINT ["/app/bin/market-runner"]
CMD ["api"]
