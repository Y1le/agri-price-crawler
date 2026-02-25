# 通用变量定义
GO := go
GOPATH := $(shell $(GO) env GOPATH)
TOOLS_BIN_DIR := $(GOPATH)/bin
OUTPUT_DIR := _output
COVERAGE_FILE := $(OUTPUT_DIR)/coverage.out
ROOT_PACKAGE := github.com/Y1le/agri-price-crawler