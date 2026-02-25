include $(dir $(lastword $(MAKEFILE_LIST)))/common.mk

# 需要安装的工具列表
TOOLS := \
	google.golang.org/protobuf/cmd/protoc-gen-go \
	google.golang.org/grpc/cmd/protoc-gen-go-grpc \
	github.com/golang/mock/mockgen \
	github.com/golangci/golangci-lint/cmd/golangci-lint

# 安装系统依赖（protoc）
.PHONY: tools.install.system
tools.install.system:
	@echo "===========> Installing system dependencies <==========="
	@if ! command -v protoc &> /dev/null; then \
		sudo apt update && sudo apt install -y protobuf-compiler; \
	fi

# 安装Go工具
.PHONY: tools.install.go
tools.install.go:
	@echo "===========> Installing Go tools <==========="
	@for tool in $(TOOLS); do \
		$(GO) install $$tool@latest; \
	done

# 验证工具是否安装
.PHONY: tools.verify
tools.verify:
	@echo "===========> Verifying tools <==========="
	@command -v protoc >/dev/null 2>&1 || { echo "ERROR: protoc not installed"; exit 1; }
	@command -v $(TOOLS_BIN_DIR)/protoc-gen-go >/dev/null 2>&1 || { echo "ERROR: protoc-gen-go not installed"; exit 1; }
	@command -v $(TOOLS_BIN_DIR)/mockgen >/dev/null 2>&1 || { echo "ERROR: mockgen not installed"; exit 1; }

# 一键安装所有工具
.PHONY: tools.install
tools.install: tools.install.system tools.install.go tools.verify

# 将工具目录加入PATH
.PHONY: tools.env
tools.env:
	@echo "export PATH=$(TOOLS_BIN_DIR):\$$PATH" >> $GITHUB_PATH