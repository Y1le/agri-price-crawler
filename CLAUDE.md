# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

agri-price-crawler is an agricultural product price monitoring and diet recommendation system. It scrapes real-time prices from cnhnb.com (惠农网), manages user subscriptions, sends daily email reports, and uses AI (Doubao/豆包) to generate healthy meal recommendations based on low-price seasonal ingredients.

## Architecture

```
cmd/craw-server/         -- Entry point (main.go)
internal/craw/           -- Core application logic
├── app.go               -- App factory and run function
├── server.go            -- Main server setup (HTTP + gRPC)
├── cron.go              -- Scheduled tasks (crawl + email)
├── crawler/             -- Price data scraper (cnhnb.com API)
├── controller/          -- HTTP handlers (user, price, subscribe)
├── service/v1/          -- Business logic layer
├── store/               -- Data access layer (interface + implementations)
├── mailer/              -- Email sending (SMTP)
├── auth.go              -- JWT authentication setup
├── config/              -- Configuration loading
└── options/             -- Command-line flags + config options
internal/ai/             -- AI recipe generation (Doubao API)
internal/pkg/            -- Shared utilities (server, middleware, code)
pkg/                     -- Infrastructure (log, db, util, app framework)
pb/                      -- Generated gRPC/protobuf code
```

## Key Patterns

- **Configuration**: Uses viper + command-line flags. Main config struct is `internal/craw/options.Options` -> `internal/craw/config.Config`
- **DI Container**: Global `store.Client()` for store factory, `ai.Client()` for AI generator
- **HTTP Server**: Gin framework with JWT authentication middleware
- **gRPC Server**: Secure (mTLS) with reflection enabled
- **Dependency Flow**: `controller -> service -> store` (interface-based)

## Common Commands

| Command | Description |
|---------|-------------|
| `make build` | Build the server binary to `_output/platforms/craw-server` |
| `make docker-build` | Build Linux binary for Docker |
| `make lint` | Run golangci-lint with auto-fix |
| `make cover` | Run tests with coverage report |
| `make tidy` | Run `go mod tidy` and `go mod verify` |
| `make gen` | Generate code (mocks + protobuf) |
| `make tools` | Install dev tools (protoc-gen-go, mockgen, golangci-lint) |

## Essential Notes

1. **Crawler Authentication**: The scraper requires `device-id` and `secret` from cnhnb.com (configured in `config.yaml`). These are used to generate签名 (SHA384 + custom trace ID algorithm).

2. **Test database**: MySQL database name is `craw`. SQL schema is in `configs/craw.sql`.

3. **Environment**: Go 1.24+, MySQL 8.x, Redis 7.x. Use `docker-compose up -d mysql redis` for local dev.

4. **Code Generation**:
   - Mocks: `go generate ./internal/ai/...`
   - Protobuf: `protoc proto/v1/*.proto --go_out=pb --go-grpc_out=pb`

5. **Entry Point**: `cmd/craw-server/crawserver.go` -> `craw.NewApp("craw").Run()` -> `config.CreateConfigFromOptions()` -> `Run(cfg)` which initializes both HTTP server and cron jobs.

6. **HTTP Routes**: All v1 routes are under `/v1/`. User/subscribe endpoints require JWT auth. Price list is accessible at `GET /v1/prices`.
