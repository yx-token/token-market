# token-market 镜像：同一镜像包含 market-api（含前端页面）、market-worker、market-edge 与 s2a-compat，
# 用不同的启动命令运行不同组件（见 deploy/docker-compose.yml）。
#
# 无法直连 Docker Hub 时可指定镜像源，例如：docker build --build-arg REGISTRY=docker.m.daocloud.io/ .
ARG REGISTRY=

FROM ${REGISTRY}library/node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM ${REGISTRY}library/golang:1.26-alpine AS go
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/worker ./cmd/edge ./cmd/s2a-compat

FROM ${REGISTRY}library/alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 market
WORKDIR /app
COPY --from=go /out/ /app/bin/
COPY --from=web /src/web/dist /app/web
ENV TM_HTTP_WEB_DIR=/app/web TM_EDGE_SPOOL_DIR=/app/data/edge-spool TZ=Asia/Shanghai
RUN mkdir -p /app/data && chown market /app/data
USER market
EXPOSE 8080 8090 8091 8092
ENTRYPOINT ["/app/bin/api"]
