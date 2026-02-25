include $(dir $(lastword $(MAKEFILE_LIST)))/common.mk

# 生成mock文件
.PHONY: gen.mock
gen.mock:
	@echo "===========> Generating mock files <==========="
	$(GO) generate ./internal/ai/...

# 生成proto文件
.PHONY: gen.proto
gen.proto:
	@echo "===========> Generating protobuf files <==========="
	protoc --proto_path=proto/v1 \
		--go_out=pb --go_opt=paths=source_relative \
		--go-grpc_out=pb --go-grpc_opt=paths=source_relative \
		proto/v1/*.proto

# 一键生成所有文件
.PHONY: gen.run
gen.run: gen.mock gen.proto