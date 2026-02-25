## test: Run unit test.
.PHONY: test
test: mock proto
	go test -v -cover -short ./internal/...

## cover: Run unit test and get test coverage.
.PHONY: cover 
cover: mock proto
	@$(MAKE) go.test.cover

.PHONY: go.test.cover
go.test.cover:
	go test -v -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: server
server:
	go run cmd/craw-server/crawserver.go

.PHONY: redis
redis:
	docker run --name redis -p 6379:6379 -d redis:7-alpine

.PHONY: mock
mock:
	go generate ./internal/ai/...


.PHONY: proto
proto:
	protoc --proto_path=proto/v1 --go_out=pb --go_opt=paths=source_relative --go-grpc_out=pb --go-grpc_opt=paths=source_relative proto/v1/*.proto
