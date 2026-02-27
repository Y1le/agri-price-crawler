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

# 帮助信息
.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
