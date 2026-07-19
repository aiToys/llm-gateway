.PHONY: help dev build fmt test test-integration lint tidy migrate-up migrate-down seed up down run-web-user run-web-admin build-web all docs clean test-e2e

GATEWAY_ADDR ?= :8080
# 默认走官方代理;国内开发者可在命令行覆盖: make build GOPROXY=https://goproxy.cn,direct
GOPROXY ?= https://proxy.golang.org,direct
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/aitoys/llm-gateway/internal/version.Version=$(VERSION)

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

tidy: ## 整理依赖
	GOPROXY=$(GOPROXY) go mod tidy

build: ## 编译后端
	GOPROXY=$(GOPROXY) go build -ldflags="$(LDFLAGS)" -o bin/gateway ./cmd/gateway
	GOPROXY=$(GOPROXY) go build -ldflags="$(LDFLAGS)" -o bin/edge ./cmd/edge

fmt: ## 格式化(gofmt;goimports 若已安装则一并)
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || echo "(goimports 未安装,跳过)"

dev: ## 本地开发(需先 docker-compose up -d postgres redis)
	GOPROXY=$(GOPROXY) go run ./cmd/gateway -config config.local.yaml

test: ## 运行测试
	GOPROXY=$(GOPROXY) go test ./... -count=1

test-integration: ## 集成测试(需 postgres+redis)
	GOPROXY=$(GOPROXY) go test ./... -tags=integration -count=1

lint: ## 静态检查(golangci-lint 优先,否则 go vet)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || GOPROXY=$(GOPROXY) go vet ./...

migrate-up: ## 执行迁移
	@GOPROXY=$(GOPROXY) go run ./cmd/gateway -config config.local.yaml -migrate up

migrate-down: ## 回滚迁移
	@GOPROXY=$(GOPROXY) go run ./cmd/gateway -config config.local.yaml -migrate down

seed: ## 灌入 mock 数据
	@GOPROXY=$(GOPROXY) go run ./cmd/gateway -config config.local.yaml -seed

up: ## 启动依赖(postgres/redis)
	docker-compose up -d postgres redis

down: ## 停止依赖
	docker-compose down

run-web-user: ## 启动用户端前端
	cd web/user && npm install && npm run dev

run-web-admin: ## 启动管理端前端
	cd web/admin && npm install && npm run dev

build-web: ## 构建两端前端产物(供单二进制嵌入)
	cd web/user && npm install && npm run build
	cd web/admin && npm install && npm run build

all: build-web build ## 一键构建前端 + 后端

docs: ## 构建文档站
	cd docs-site && npm install && npm run docs:build

clean: ## 清理构建产物
	rm -rf bin web/user/dist web/admin/dist docs-site/.vitepress/dist tests/e2e/test-results tests/e2e/playwright-report

test-e2e: ## 端到端测试(需先起网关并 seed)
	cd tests/e2e && npm install && npx playwright install chromium && npx playwright test
