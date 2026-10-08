SUB2API_REPO ?= ../github-fork/sub2api
TEST_DATABASE_DSN ?= postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable

.PHONY: build test test-integration lint test-db-up test-db-down compat-check

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

# 需要 PostgreSQL（make test-db-up）与本地 sub2api 仓库
test-integration:
	TM_TEST_DATABASE_DSN="$(TEST_DATABASE_DSN)" TM_SUB2API_REPO="$(SUB2API_REPO)" go test -tags integration ./...

lint:
	golangci-lint run ./...
	golangci-lint run --build-tags integration,smoke ./...

test-db-up:
	docker run -d --name tm-test-pg -e POSTGRES_USER=tm -e POSTGRES_PASSWORD=tm -e POSTGRES_DB=tm -p 55432:5432 postgres:16-alpine

test-db-down:
	docker rm -f tm-test-pg

# 示例：make compat-check CURRENT=v0.2.14 TARGET=v0.2.15
compat-check:
	go run ./cmd/s2a-compat check --sub2api-repo "$(SUB2API_REPO)" --current "$(CURRENT)" --target "$(TARGET)"
