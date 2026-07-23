.DEFAULT_GOAL := all

# 引入子 Makefile
include scripts/make-rules/common.mk
include scripts/make-rules/tools.mk
include scripts/make-rules/gen.mk

# 全量目标
all: tidy gen lint cover build

# 依赖整理
.PHONY: tidy
tidy:
	@echo "===========> Tidying go modules <==========="
	$(GO) mod tidy
	$(GO) mod verify

# 代码生成（入口）
.PHONY: gen
gen: gen.run

# 代码lint
.PHONY: lint
lint: tools.verify
	@echo "===========> Running golangci-lint <==========="
	$(TOOLS_BIN_DIR)/golangci-lint run ./... --timeout 5m --fix

# 测试覆盖率
.PHONY: cover
cover: 
	@echo "===========> Running tests with coverage <==========="
	@mkdir -p $(OUTPUT_DIR)/coverage
	$(GO) test -v -coverprofile=$(COVERAGE_FILE) ./internal/...
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(OUTPUT_DIR)/coverage/coverage.html

# 构建
.PHONY: build
build: 
	@echo "===========> Building craw-server binary <==========="
	@mkdir -p $(OUTPUT_DIR)/platforms
	$(GO) build -o $(OUTPUT_DIR)/platforms/craw-server $(ROOT_PACKAGE)/cmd/craw-server

# 工具安装（入口）
.PHONY: tools
tools: tools.install tools.env


.PHONY: docker-build
docker-build: tidy gen
	@echo "===========> Building craw-server for docker <==========="
	@mkdir -p $(OUTPUT_DIR)/platforms
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w" -o $(OUTPUT_DIR)/platforms/craw-server $(ROOT_PACKAGE)/cmd/craw-server

# Backend v2 tests
.PHONY: v2-test
v2-test:
	@echo "===========> Running backend v2 tests <==========="
	$(GO) test -p 1 -race ./internal/platform/... ./internal/platform/httpx/... ./internal/identity/... ./internal/gateway/... ./internal/bootstrap/...

# Backend v2 binaries
.PHONY: v2-build
v2-build:
	@echo "===========> Building backend v2 binaries <==========="
	@mkdir -p $(OUTPUT_DIR)/platforms
	$(GO) build -o $(OUTPUT_DIR)/platforms/gateway $(ROOT_PACKAGE)/cmd/gateway
	$(GO) build -o $(OUTPUT_DIR)/platforms/worker $(ROOT_PACKAGE)/cmd/worker
	$(GO) build -o $(OUTPUT_DIR)/platforms/migrate $(ROOT_PACKAGE)/cmd/migrate

# Backend v2 live smoke test
.PHONY: v2-smoke
v2-smoke:
	@set -eu; \
		IDENTITY_JWT_PRIVATE_KEY_BASE64="$$(openssl rand -base64 32)"; \
		IDENTITY_OTP_PEPPER_BASE64="$$(openssl rand -base64 32)"; \
		export IDENTITY_JWT_PRIVATE_KEY_BASE64 IDENTITY_OTP_PEPPER_BASE64; \
		docker compose -f docker-compose.v2.yaml up --build --wait
	curl --fail --silent --show-error http://localhost:8080/livez
	@echo
	curl --fail --silent --show-error http://localhost:8080/readyz
	@echo

# Backend v2 Identity integration test
.PHONY: v2-identity-integration
v2-identity-integration:
	@set -eu; \
		trap 'docker compose -f docker-compose.v2.yaml stop postgres redis' EXIT INT TERM; \
		docker compose -f docker-compose.v2.yaml up -d --wait postgres redis; \
		docker compose -f docker-compose.v2.yaml run --build --rm migrate; \
		TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable' \
		TEST_REDIS_ADDR='localhost:6379' \
		$(GO) test -count=1 ./internal/bootstrap -run IdentityIntegration

# 帮助信息
.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
