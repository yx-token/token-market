SUB2API_REPO ?= ..
TEST_DATABASE_DSN ?= postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable
TEST_REDIS_ADDR ?= 127.0.0.1:56379

.PHONY: build test test-integration lint test-db-up test-db-down compat-check web-build web-test dev-up dev-down image

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

# 需要 PostgreSQL 与 Redis（make test-db-up）以及本地 sub2api 仓库；端到端测试会清空 Redis 的第 9 号库
test-integration:
	TM_TEST_DATABASE_DSN="$(TEST_DATABASE_DSN)" TM_SUB2API_REPO="$(SUB2API_REPO)" TM_TEST_REDIS_ADDR="$(TEST_REDIS_ADDR)" \
		go test -race -tags integration -timeout 20m ./...

lint:
	golangci-lint run ./...
	golangci-lint run --build-tags integration,smoke ./...

test-db-up:
	docker run -d --name tm-test-pg -e POSTGRES_USER=tm -e POSTGRES_PASSWORD=tm -e POSTGRES_DB=tm -p 55432:5432 postgres:16-alpine
	docker run -d --name tm-test-redis -p 56379:6379 redis:7-alpine

test-db-down:
	docker rm -f tm-test-pg tm-test-redis

# 示例：make compat-check CURRENT=v0.2.14 TARGET=v0.2.15 TARGET_VERSION=0.2.15
compat-check:
	go run ./cmd/s2a-compat check --sub2api-repo "$(SUB2API_REPO)" --current "$(CURRENT)" --target "$(TARGET)" \
		$(if $(TARGET_VERSION),--target-version "$(TARGET_VERSION)")

# 前端
web-build:
	cd web && pnpm install --frozen-lockfile && pnpm run build

web-test:
	cd web && pnpm test

# 本地开发 / 演示环境（sub2api 替身 + 全部服务，见 scripts/dev.sh）
dev-up: web-build
	scripts/dev.sh up

dev-down:
	scripts/dev.sh down

# 镜像（无法直连 Docker Hub 时：make image REGISTRY=<镜像源>/）
image:
	docker build $(if $(REGISTRY),--build-arg REGISTRY=$(REGISTRY)) \
		--build-arg VERSION="$$(git describe --tags --always --dirty)" --build-arg COMMIT="$$(git rev-parse HEAD)" \
		-t token-market:local .
