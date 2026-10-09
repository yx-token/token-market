# token-market 镜像：同一镜像包含 market-api（含前端页面）、market-worker、market-edge 与 s2a-compat，
# 用不同的启动命令运行不同组件（见 deploy/docker-compose.yml）。
#
# 无法直连 Docker Hub 时可指定镜像源，例如：docker build --build-arg REGISTRY=docker.m.daocloud.io/ .
# 多架构：前端与 Go 都在构建机的架构上编译（Go 交叉编译），只有最后一层按目标架构组装，
# 所以 docker buildx build --platform linux/amd64,linux/arm64 不需要在模拟器里跑编译。
ARG REGISTRY=

FROM --platform=$BUILDPLATFORM ${REGISTRY}library/node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM --platform=$BUILDPLATFORM ${REGISTRY}library/golang:1.26-alpine AS go
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X github.com/DueGin/token-market/internal/platform/buildinfo.Version=${VERSION} -X github.com/DueGin/token-market/internal/platform/buildinfo.Commit=${COMMIT}" \
      -o /out/ ./cmd/api ./cmd/worker ./cmd/edge ./cmd/s2a-compat ./cmd/market-runner
RUN printf '{"schema":1,"version":"%s","commit":"%s","os":"%s","arch":"%s"}\n' \
      "$VERSION" "$COMMIT" "$TARGETOS" "$TARGETARCH" > /manifest.json

# Release 更新包直接导出此阶段，复用镜像里的二进制和前端，不运行目标架构程序。
FROM scratch AS release
COPY --from=go /out/ /bin/
COPY --from=web /src/web/dist /web/
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
