## test: Run unit test.
.PHONY: test
test:
	go test -v -cover -short ./internal/...

## cover: Run unit test and get test coverage.
.PHONY: cover 
cover:
	@$(MAKE) go.test.cover

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: server
server:
	go run cmd/craw-server/crawserver.go

.PHONY: redis
redis:
	docker run --name redis -p 6379:6379 -d redis:7-alpine