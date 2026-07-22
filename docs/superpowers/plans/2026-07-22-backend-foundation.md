# Backend Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runnable Go Gateway/Worker foundation with validated configuration, PostgreSQL migrations, a durable job queue, health endpoints, and an isolated Docker development stack.

**Architecture:** Add the new modular-monolith runtime beside the existing server so the repository stays buildable. Gateway and Worker are separate programs sharing platform packages; PostgreSQL is durable state and Redis is optional short-lived state. This is plan 1 of 5; later plans cover Identity, Pricing/Ingestion, Subscription/Recipe/Notification, and final cutover.

**Tech Stack:** Go 1.24.3, standard `net/http`, `log/slog`, pgx v5, go-redis v9, PostgreSQL 16, Redis 7, Docker Compose, OpenAPI 3.1.

## Global Constraints

- Keep module path `github.com/Y1le/agri-price-crawler`.
- Build `gateway` and `worker` as separate stateless programs from one repository.
- PostgreSQL is the only durable business store; Redis contains only replaceable or short-lived state.
- Use constructor injection; do not add package-level mutable clients.
- Gateway and Worker check the schema version but never apply migrations automatically.
- Preserve `cmd/craw-server` until the final cutover plan.
- Add infrastructure in `docker-compose.v2.yaml`; do not replace the legacy Compose file yet.
- Do not migrate, read, or retain old MySQL business data.
- Use TDD and commit after every task.
- Run packages that mutate the shared integration database with `go test -p 1`.

## File Map

- `internal/platform/config`: environment configuration.
- `internal/platform/postgres`: PostgreSQL pool construction.
- `internal/platform/rediscache`: Redis client construction.
- `internal/platform/migrate`: embedded forward-only migrations.
- `internal/platform/jobs`: durable job repository and runner.
- `internal/gateway`: HTTP server and health routes.
- `internal/bootstrap`: process dependency assembly.
- `cmd/{gateway,worker,migrate}`: process entry points.
- `api/openapi.yaml`: Gateway contract.
- `Dockerfile.v2`, `docker-compose.v2.yaml`: new isolated runtime.

---

### Task 1: Validated Runtime Configuration

**Files:**
- Create: `internal/platform/config/config.go`
- Test: `internal/platform/config/config_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: environment variables.
- Produces: `config.Load() (config.Config, error)` and `Config{Environment, Gateway, Postgres, Redis, Worker}`.

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/jackc/pgx/v5 github.com/redis/go-redis/v9
```

Expected: both modules become direct requirements.

- [ ] **Step 2: Write failing tests**

Create tests for these exact cases:

```go
func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://agri:agri@localhost:5432/agri?sslmode=disable")
	got, err := config.Load()
	if err != nil { t.Fatal(err) }
	if got.Environment != "development" || got.Gateway.Addr != ":8080" { t.Fatalf("got %+v", got) }
	if got.Redis.Addr != "localhost:6379" { t.Fatalf("got %+v", got.Redis) }
	if got.Worker.PollInterval != time.Second || got.Worker.BatchSize != 4 { t.Fatalf("got %+v", got.Worker) }
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := config.Load(); err == nil { t.Fatal("want error") }
}

func TestLoadRejectsInvalidWorkerValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("WORKER_POLL_INTERVAL", "bad")
	if _, err := config.Load(); err == nil { t.Fatal("want duration error") }
	t.Setenv("WORKER_POLL_INTERVAL", "1s")
	t.Setenv("WORKER_BATCH_SIZE", "0")
	if _, err := config.Load(); err == nil { t.Fatal("want batch error") }
}
```

- [ ] **Step 3: Verify failure**

Run: `go test ./internal/platform/config -v`

Expected: FAIL because the package is absent.

- [ ] **Step 4: Implement the configuration types and loader**

Use these exact types and defaults:

```go
type Config struct { Environment string; Gateway Gateway; Postgres Postgres; Redis Redis; Worker Worker }
type Gateway struct { Addr string }
type Postgres struct { URL string }
type Redis struct { Addr, Username, Password string; DB int }
type Worker struct { PollInterval time.Duration; BatchSize int }
```

`Load` must require `DATABASE_URL`; default `APP_ENV=development`, `GATEWAY_ADDR=:8080`, `REDIS_ADDR=localhost:6379`, `REDIS_DB=0`, `WORKER_POLL_INTERVAL=1s`, and `WORKER_BATCH_SIZE=4`. Parse duration with `time.ParseDuration`, integers with `strconv.Atoi`, require batch size 1–100 and Redis DB at least zero, and name the invalid variable in every error.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/platform/config -v
go mod tidy
git diff --check
git add go.mod go.sum internal/platform/config
git commit -m "feat(platform): add validated runtime configuration"
```

Expected: tests PASS and commit succeeds.

---

### Task 2: PostgreSQL, Redis, and Migrations

**Files:**
- Create: `docker-compose.v2.yaml`
- Create: `internal/platform/postgres/postgres.go`
- Create: `internal/platform/rediscache/redis.go`
- Create: `internal/platform/migrate/migrate.go`
- Create: `internal/platform/migrate/migrate_test.go`
- Create: `internal/platform/migrate/sql/000001_platform.up.sql`

**Interfaces:**
- Consumes: Task 1 configuration.
- Produces: `postgres.Open(ctx, url) (*pgxpool.Pool, error)`, `rediscache.Open(config.Redis) *redis.Client`, `migrate.Up(ctx, pool) error`, and `migrate.CurrentVersion(ctx, pool) (int64, error)`.

- [ ] **Step 1: Create dependency-only Compose services**

Use PostgreSQL `postgres:16-alpine` with database/user `agri`, development password `agri_dev`, port 5432 and `pg_isready` health check. Use `redis:7-alpine`, port 6379 and `redis-cli ping`. Name the volume `postgres_v2_data` and the Compose project `agri-price-crawler-v2`.

Run:

```bash
docker compose -f docker-compose.v2.yaml up -d postgres redis
docker compose -f docker-compose.v2.yaml ps
```

Expected: both services are healthy.

- [ ] **Step 2: Write the failing migration test**

```go
func TestUpIsIdempotent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" { t.Fatal("TEST_DATABASE_URL is required") }
	ctx := context.Background()
	pool, err := platformpg.Open(ctx, url)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS platform_outbox, platform_jobs, platform_schema_migrations"); err != nil { t.Fatal(err) }
	if err := migrate.Up(ctx, pool); err != nil { t.Fatal(err) }
	if err := migrate.Up(ctx, pool); err != nil { t.Fatal(err) }
	version, err := migrate.CurrentVersion(ctx, pool)
	if err != nil || version != 1 { t.Fatalf("version=%d err=%v", version, err) }
}
```

Run with `TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable'`; expect FAIL because packages are absent.

- [ ] **Step 3: Implement connection constructors**

`postgres.Open` must call `pgxpool.New`, then `Ping`, closing the pool on ping failure. `rediscache.Open` must map all four Redis fields directly into `redis.Options`; it must not ping or mutate global state.

- [ ] **Step 4: Create the platform migration**

Create `platform_jobs` with `BIGSERIAL id`, `(kind,business_key)` unique, JSONB payload, states `pending/running/succeeded/dead`, run time, attempts, maximum attempts default 3, lock owner/time, last error, timestamps, and a partial `(run_at,id)` pending index. Create `platform_outbox` with unique `(topic,business_key)`, JSONB payload, creation/publication timestamps, and an unpublished partial index.

- [ ] **Step 5: Implement forward-only migrations**

Embed `sql/*.up.sql`. `Up` must begin a transaction, acquire `pg_advisory_xact_lock(71422026)`, create `platform_schema_migrations(version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ)`, sort files, parse the numeric prefix, skip recorded versions, execute each file, record its version, and commit. `CurrentVersion` returns `COALESCE(MAX(version),0)` and wraps database errors with operation context.

- [ ] **Step 6: Verify and commit**

```bash
TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable' go test -p 1 -race ./internal/platform/migrate -v
git diff --check
git add docker-compose.v2.yaml internal/platform/postgres internal/platform/rediscache internal/platform/migrate
git commit -m "feat(platform): add PostgreSQL migrations and Redis client"
```

Expected: idempotency test PASS.

---

### Task 3: Durable Job Repository

**Files:**
- Create: `internal/platform/jobs/types.go`
- Create: `internal/platform/jobs/postgres.go`
- Test: `internal/platform/jobs/postgres_test.go`

**Interfaces:**
- Consumes: migrated `platform_jobs`.
- Produces: `Repository` with `Enqueue`, `Claim`, `Complete`, `Retry`, and `Dead` methods.

- [ ] **Step 1: Define the test contract**

```go
type Job struct { ID int64; Kind, BusinessKey string; Payload json.RawMessage; RunAt time.Time; Attempts, MaxAttempts int }
type NewJob struct { Kind, BusinessKey string; Payload json.RawMessage; RunAt time.Time; MaxAttempts int }
type Repository interface {
	Enqueue(context.Context, NewJob) (int64, bool, error)
	Claim(context.Context, string, time.Time, int) ([]Job, error)
	Complete(context.Context, int64) error
	Retry(context.Context, int64, time.Time, string) error
	Dead(context.Context, int64, string) error
}
```

Write an integration test that enqueues `(platform.test,2026-07-22)` twice and receives the same ID with `created=true` then `false`; claims it with attempt 1; retries it one minute later; claims with attempt 2; and completes it.

- [ ] **Step 2: Verify failure**

Run the jobs test with `TEST_DATABASE_URL`; expect FAIL because the repository is absent.

- [ ] **Step 3: Implement SQL lifecycle**

- `Enqueue`: `INSERT ... ON CONFLICT DO NOTHING RETURNING id`, then select the existing ID on no row.
- `Claim`: one transaction using `FOR UPDATE SKIP LOCKED`, ordered by `(run_at,id)`, updating picked rows to running and incrementing attempts.
- `Complete`: running to succeeded.
- `Retry`: running to pending with new `run_at` and error.
- `Dead`: running to dead with error.

Validate kind, business key, nonempty JSON, max attempts 1–10, and claim limit 1–100. Every lifecycle update must require exactly one affected row.

- [ ] **Step 4: Verify and commit**

```bash
TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable' go test -race ./internal/platform/jobs -run TestPostgresRepository -v
git diff --check
git add internal/platform/jobs
git commit -m "feat(platform): add durable PostgreSQL job repository"
```

Expected: idempotency and lifecycle tests PASS.

---

### Task 4: Worker Runner and Retry Policy

**Files:**
- Create: `internal/platform/jobs/runner.go`
- Test: `internal/platform/jobs/runner_test.go`

**Interfaces:**
- Consumes: Task 3 `Repository`.
- Produces: `Handler func(context.Context,json.RawMessage) error`, `NewRunner`, `Register`, `RunOnce`, and `Run`.

- [ ] **Step 1: Write failing fake-repository tests**

Test these exact outcomes: successful handler calls `Complete`; attempt 1 failure calls `Retry` for `now+1m`; attempt 2 failure retries at `now+2m`; final attempt calls `Dead`; unknown kind calls `Dead`; duplicate handler registration returns an error.

- [ ] **Step 2: Verify failure**

Run: `go test ./internal/platform/jobs -run TestRunner -v`

Expected: FAIL with undefined runner symbols.

- [ ] **Step 3: Implement the runner**

`NewRunner(repository, workerID, batchSize, logger)` stores dependencies and an empty handler map. `RunOnce(ctx,now)` claims `batchSize` jobs and handles them concurrently. Retry delay is `1m << (attempts-1)`, capped at one hour. Lifecycle update failures are logged with job ID and action. `Run(ctx,pollInterval)` executes immediately, then on each ticker interval, and returns nil after context cancellation.

- [ ] **Step 4: Verify and commit**

```bash
go test -race ./internal/platform/jobs -run TestRunner -v
git diff --check
git add internal/platform/jobs/runner.go internal/platform/jobs/runner_test.go
git commit -m "feat(worker): dispatch durable jobs with bounded retries"
```

Expected: all runner cases PASS without races.

---

### Task 5: Gateway Health Contract

**Files:**
- Create: `internal/gateway/server.go`
- Test: `internal/gateway/server_test.go`
- Create: `api/openapi.yaml`

**Interfaces:**
- Consumes: `Ready func(context.Context) error`.
- Produces: `gateway.New(Config) *Server`, `Handler() http.Handler`, and `Run(context.Context) error`.

- [ ] **Step 1: Write failing HTTP tests**

Use `httptest` to assert `GET /livez` returns 200 and `{"status":"ok"}`. Assert `/readyz` returns the same when Ready is nil, and 503 Problem Details containing `"code":"service_not_ready"` when Ready returns an error.

- [ ] **Step 2: Verify failure**

Run: `go test ./internal/gateway -v`

Expected: FAIL because Gateway is absent.

- [ ] **Step 3: Implement Gateway**

Define `Config{Addr string; Ready func(context.Context) error; Logger *slog.Logger}`. Use `http.ServeMux`, JSON content type, and exact routes `/livez` and `/readyz`. Configure `ReadHeaderTimeout=5s`. On context cancellation, call `Shutdown` with a fresh 10-second timeout. Treat `http.ErrServerClosed` as success.

- [ ] **Step 4: Add OpenAPI 3.1**

Define `Health{status:string}` and `Problem{type,title,status,code}` schemas and both probe operations. State that probes are root infrastructure routes while future product APIs use `/api/v1`.

- [ ] **Step 5: Verify and commit**

```bash
go test -race ./internal/gateway -v
git diff --check
git add api/openapi.yaml internal/gateway
git commit -m "feat(gateway): add health server and OpenAPI foundation"
```

Expected: all health tests PASS.

---

### Task 6: Bootstrap and Process Commands

**Files:**
- Create: `internal/bootstrap/gateway.go`
- Create: `internal/bootstrap/worker.go`
- Create: `internal/bootstrap/migrate.go`
- Test: `internal/bootstrap/bootstrap_test.go`
- Create: `cmd/gateway/main.go`
- Create: `cmd/worker/main.go`
- Create: `cmd/migrate/main.go`

**Interfaces:**
- Consumes: Tasks 1–5.
- Produces: `RunGateway`, `RunWorker`, and `RunMigrate`, each accepting context, config, and logger.

- [ ] **Step 1: Write failing bootstrap tests**

Test that each function wraps PostgreSQL connection failure with the word `PostgreSQL`. Against `TEST_DATABASE_URL`, drop only `platform_outbox`, `platform_jobs`, and `platform_schema_migrations`, call `RunMigrate`, and verify schema version 1.

- [ ] **Step 2: Implement assembly**

- `RunGateway`: open PostgreSQL, require schema version 1, construct Redis, warn but continue on Redis ping failure, construct Gateway with `pool.Ping`, and run it.
- `RunWorker`: open PostgreSQL, require version 1, construct repository and runner, use hostname plus PID as worker ID, and run until cancellation.
- `RunMigrate`: open PostgreSQL, call `migrate.Up`, and close the pool.

- [ ] **Step 3: Add signal-aware commands**

Each main loads config, creates a JSON `slog` logger, derives context with `signal.NotifyContext(SIGINT,SIGTERM)`, calls its bootstrap function, logs errors, and exits 1 on failure. No command imports legacy `internal/craw`.

- [ ] **Step 4: Verify and commit**

```bash
TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable' go test -p 1 -race ./internal/platform/... ./internal/gateway/... ./internal/bootstrap/...
go build ./cmd/gateway ./cmd/worker ./cmd/migrate ./cmd/craw-server
git diff --check
git add internal/bootstrap cmd/gateway cmd/worker cmd/migrate
git commit -m "feat: add Gateway Worker and migration commands"
```

Expected: all tests pass and all four commands build.

---

### Task 7: Docker, Make, and CI Delivery

**Files:**
- Create: `Dockerfile.v2`
- Modify: `docker-compose.v2.yaml`
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: runnable commands from Task 6.
- Produces: `make v2-test`, `make v2-build`, and `make v2-smoke`.

- [ ] **Step 1: Add Make acceptance targets**

`v2-test` runs the new platform, Gateway, and bootstrap tests with `-p 1 -race`. `v2-build` writes Gateway, Worker, and migrate binaries to `_output/platforms`. `v2-smoke` starts `docker-compose.v2.yaml` with build/wait and curls `/livez` plus `/readyz`. Run smoke now and expect failure because app services are absent.

- [ ] **Step 2: Create the image**

Use `golang:1.24-alpine` to build all three binaries with `CGO_ENABLED=0`, then copy them into `alpine:3.21`. Install CA certificates and run as numeric user/group 65532. Default entrypoint is `/app/gateway`; Compose overrides it for Worker and migrate.

- [ ] **Step 3: Complete Compose**

Add a one-shot migrate service that waits for PostgreSQL, Gateway that waits for successful migrate and healthy Redis, and Worker that waits for migrate. Set `DATABASE_URL=postgres://agri:agri_dev@postgres:5432/agri?sslmode=disable`; expose Gateway 8080; add both Gateway health probes. Do not mount source or `.env`.

- [ ] **Step 4: Add independent CI verification**

Add `backend_v2` to `.github/workflows/ci.yml`: install Go 1.24.3, start PostgreSQL/Redis, export the test database URL, run `make v2-test`, `make v2-build`, and build `Dockerfile.v2`. Always finish with `docker compose -f docker-compose.v2.yaml down -v`.

- [ ] **Step 5: Run the completion gate**

```bash
export TEST_DATABASE_URL='postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable'
make v2-test
make v2-build
make v2-smoke
go build ./cmd/craw-server
git diff --check
```

Expected: all tests pass; three new binaries exist; Gateway and Worker run; both probes return `{"status":"ok"}`; the legacy command still builds.

- [ ] **Step 6: Stop and commit**

```bash
docker compose -f docker-compose.v2.yaml down
git add Dockerfile.v2 docker-compose.v2.yaml Makefile .github/workflows/ci.yml
git commit -m "build: add backend v2 development and CI workflow"
```

Expected: containers stop without deleting `postgres_v2_data`, and the worktree is clean.

## Completion Gate

Before writing or executing the Identity plan, verify:

```bash
git status --short
go test -p 1 ./internal/platform/... ./internal/gateway/... ./internal/bootstrap/...
go build ./cmd/gateway ./cmd/worker ./cmd/migrate ./cmd/craw-server
```

Expected: clean worktree, passing tests, and buildable new plus legacy commands. No Identity, Pricing, Ingestion, Subscription, Recipe, or Notification logic belongs in this foundation slice.
