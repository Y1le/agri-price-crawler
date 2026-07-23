# Pricing 与 Ingestion 实施计划

> **执行方式：** 按任务顺序以测试驱动开发完成；每个任务先写失败测试，再实现最小代码，完成后运行列出的验证并提交。不要开始后续任务，直到当前任务的规格审查与代码质量审查通过。

**目标：** 在模块化单体中完成可重复的惠农网价格采集、原子发布、公开价格查询与趋势 API；失败采集永不覆盖上一批有效数据。

**前置规格：** `docs/superpowers/specs/2026-07-23-pricing-ingestion-design.md`

**技术约束：** Go 1.24.3、`net/http`、pgx v5、PostgreSQL 16、已有 `platform/jobs`、已有 `httpx.Problem`。不读取或改造旧 `internal/craw`、MySQL、Gin 或 GORM 代码；它们只可用于字段调研。

## 全局约束

- 新业务表只由 Pricing/Ingestion 模块拥有；其他业务模块仅走公开应用接口。
- 迁移版本为全局唯一，接在当前 `000003_identity.up.sql` 之后。
- 测试只能使用独立 PostgreSQL schema 与独立 Redis 前缀/DB，禁止 `TRUNCATE` 开发公共表。
- 任何金额比较或持久化不得使用 `float64`；HTTP 金额字段必须编码为 JSON string。
- 所有业务日期使用 `Asia/Shanghai`，持久化瞬间使用 UTC `TIMESTAMPTZ`。
- 真实数据源访问只通过显式配置；开发和 CI 只使用 fixture/httptest Source。
- 每项完成后运行 `git diff --check`；日志、错误、fixture 和测试输出不得包含 Source Secret、签名或完整原始响应。

---

## Task 1：扩展配置与业务日期基础类型

**文件：**

- 修改：`internal/platform/config/config.go`
- 修改：`internal/platform/config/config_test.go`
- 新建：`internal/platform/config/ingestion.go`
- 新建：`internal/platform/config/ingestion_test.go`
- 新建：`internal/ingestion/model.go`
- 新建：`internal/ingestion/model_test.go`

**接口：**

```go
type Ingestion struct {
    Huinong Huinong
    RejectRatioMaxBasisPoints uint16
    RawRetention time.Duration
    ScheduleTimezone string
}
type Huinong struct {
    Enabled bool
    BaseURL string
    Timeout time.Duration
    DeviceID string
    Secret string
}
```

### Step 1：先写配置与业务日期失败测试

- 开发环境默认 `INGESTION_HUINONG_ENABLED=false`、`INGESTION_HUINONG_TIMEOUT=15s`、`INGESTION_REJECT_RATIO_MAX=0.05`、`INGESTION_RAW_RETENTION=720h`、时区 `Asia/Shanghai`。
- 验证 reject ratio 为 `[0,0.20]`；将十进制文本精确解析为基点（`0.05 = 500 bp`），不经过 `float64`；保留期至少 24 小时，超时为正且不超过 30 秒。
- 开启惠农网时要求 Base URL、Device ID 与 Secret；生产环境要求 HTTPS 且显式开启。
- 验证 `ingestion.BusinessDate` 只接受并稳定输出 `YYYY-MM-DD`，按 `Asia/Shanghai` 解释，拒绝零值和非规范日期。

运行：`go test ./internal/platform/config ./internal/ingestion -run 'Ingestion|BusinessDate' -count=1`

预期：FAIL，因为新配置和业务日期类型尚不存在。

### Step 2：实现安全配置与业务日期

- 解析环境变量时不得打印 Secret。
- 拒绝率将小数文本解析为范围 `[0,2000]` 的基点；校验时用整数交叉相乘，避免浮点误差。
- 定义 `BusinessDate`，由采集幂等键、批次日期和调度 payload 共用，禁止不同模块各自格式化 `time.Time`。

### Step 3：验证并提交

```bash
go test -race ./internal/platform/config ./internal/ingestion
git diff --check
git add internal/platform/config internal/ingestion/model.go internal/ingestion/model_test.go
git commit -m "feat(config): configure price ingestion runtime"
```

---

## Task 2：Pricing 目录、迁移与只读 Repository

**文件：**

- 新建：`internal/pricing/model.go`
- 新建：`internal/pricing/ports.go`
- 新建：`internal/pricing/catalog.go`
- 新建：`internal/pricing/catalog_test.go`
- 新建：`internal/pricing/postgres/repository.go`
- 新建：`internal/pricing/postgres/repository_test.go`
- 新建：`internal/pricing/postgres/migrations.go`
- 新建：`internal/pricing/postgres/migrations/000004_pricing_catalog.up.sql`
- 修改：`internal/bootstrap/migrate.go`
- 修改：`internal/bootstrap/schema_test.go`

**数据模型：** `pricing_categories`、`pricing_products`、`pricing_regions`，以及受迁移管理的初始目录种子。

### Step 1：写目录领域测试

覆盖：

- Product/Region 的 UUID、active、名称、slug、层级和行政代码校验；
- 游标编码必须绑定排序版本和查询条件；
- 目录分页按 `name,id`（产品）及 `level,full_name,id`（地区）稳定排序；
- 不返回 inactive 记录；
- `q` 最多 64 字符，limit 默认 20、最大 50。

运行：`go test ./internal/pricing -run 'Catalog|Cursor' -count=1`

预期：FAIL。

### Step 2：写 PostgreSQL 集成测试

使用独立 schema，运行全部 migration 后断言：

- 目录表、唯一约束和初始种子存在；
- 相同 slug / national code 不能重复；
- 查询游标没有重复、没有跳项；
- parent region 只返回直接子级。

运行：`TEST_DATABASE_URL=... go test -race ./internal/pricing/postgres -run Catalog -count=1`

预期：FAIL。

### Step 3：实现目录和 Repository

- Product、Region、Category 与游标 DTO 不暴露数据库 row 类型。
- 游标使用 base64url JSON，包含 `v`、排序位置、过滤条件哈希；不匹配的 cursor 返回 `ErrInvalidCursor`。
- PostgreSQL 查询只选择公开列，使用 keyset 条件，不使用 `OFFSET`。
- migration 以 `000004` 建表、索引、约束与最小可用种子；所有种子 `ON CONFLICT` 幂等。
- `Pricing.Migrations()` 使用嵌入式模块目录；将其加入 `migrationSources()`，并更新 schema 版本测试，避免空迁移目录导致 `go:embed` 构建失败。

### Step 4：验证并提交

```bash
TEST_DATABASE_URL=... go test -race ./internal/pricing/... -count=1
go test ./internal/bootstrap -run Schema -count=1
git diff --check
git add internal/pricing internal/bootstrap/migrate.go
git commit -m "feat(pricing): add catalog and cursor repository"
```

---

## Task 3：Ingestion batch、原始记录与安全生命周期

**文件：**

- 新建：`internal/ingestion/errors.go`
- 新建：`internal/ingestion/ports.go`
- 新建：`internal/ingestion/batch.go`
- 新建：`internal/ingestion/batch_test.go`
- 新建：`internal/ingestion/postgres/repository.go`
- 新建：`internal/ingestion/postgres/repository_test.go`
- 新建：`internal/ingestion/postgres/migrations.go`
- 新建：`internal/ingestion/postgres/migrations/000005_ingestion_batches.up.sql`
- 修改：`internal/bootstrap/migrate.go`
- 修改：`internal/bootstrap/schema_test.go`

**接口：**

```go
type BatchRepository interface {
    Start(context.Context, source string, date BusinessDate, jobID int64) (Batch, bool, error)
    AppendRaw(context.Context, batchID uuid.UUID, record RawRecord) error
    ListPending(context.Context, batchID uuid.UUID) ([]RawRecord, error)
    MarkRaw(context.Context, rawID int64, result ValidationResult) error
    FinishFetching(context.Context, batchID uuid.UUID, expected *int, fetched int) error
    FinishValidation(context.Context, batchID uuid.UUID, counts BatchCounts) error
    Fail(context.Context, batchID uuid.UUID, code string) error
    PruneExpired(context.Context, now time.Time, limit int) (int64, error)
}

type PublishCoordinator interface {
    WithinPublish(context.Context, batchID uuid.UUID, func(PublishTx) error) error
}

type PublishTx interface {
    SQL() platformpostgres.Tx
    Batch() Batch
    MarkPublished(context.Context, at time.Time) error
}
```

### Step 1：写领域状态机测试

- 同一 `(source,business_date)` 并发 Start 返回同一 batch，只有一个创建者；
- running 才能 append，validated/published/failed 不能再追加；
- raw key 相同且 hash 相同为幂等，hash 不同为永久 `duplicate_conflict`；
- failure code 只能是稳定分类，不能存入原始 Source error；
- `expires_at` 从配置 retention 计算；
- prune 只选过期 raw record，绝不选择 batch、observation 或 snapshot。

### Step 2：写 PostgreSQL 并发与约束测试

- 运行两 goroutine Start/Append 相同业务日期；
- 验证 batch 唯一约束、raw 唯一约束、计数和 indexes；
- 使用独立 schema；不清空公共表。

### Step 3：实现迁移和 Repository

- 建 `ingestion_batches`、`ingestion_raw_records`、映射表的结构与索引。
- `Ingestion.Migrations()` 使用嵌入式模块目录；将其加入 `migrationSources()`，并更新 schema 版本测试。
- `payload` 进入数据库前经过字段白名单/敏感字段剥离；调用方不能写 Secret、Authorization、Cookie、签名字段。
- 错误映射区分 permanent 与 retryable，供 `jobs.Permanent` 使用。

### Step 4：验证并提交

```bash
TEST_DATABASE_URL=... go test -race ./internal/ingestion/... -count=1
go test ./internal/bootstrap -run Schema -count=1
git diff --check
git add internal/ingestion internal/bootstrap/migrate.go
git commit -m "feat(ingestion): persist batches and raw records"
```

---

## Task 4：单位换算、映射、去重与规范化

**文件：**

- 新建：`internal/ingestion/normalize.go`
- 新建：`internal/ingestion/normalize_test.go`
- 新建：`internal/ingestion/mapping.go`
- 新建：`internal/ingestion/mapping_test.go`

### Step 1：先写确定性规范化测试

覆盖：

- `元/公斤`、`元/kg`、`元/斤` 到 `CNY/kg` 的精确换算；
- 正确拒绝未知单位、负价、`min > max`、越界 average、缺少观测时间；
- source category/breed/region 映射缺失各自形成稳定 reject code；
- 同一 source key 相同 payload 只保留一次，不同 payload 为 batch 失败；
- 带正 sample_count 的记录使用加权平均；任一缺失/非正时回退简单平均；
- reject ratio 使用基点和整数交叉相乘，边界 `5%` 可发布、超过才失败。

运行：`go test -race ./internal/ingestion -run 'Normalize|Mapping|Summary' -count=1`

预期：FAIL。

### Step 2：实现纯领域服务

- 所有金额使用固定四位小数的定点值/字符串解析类型；源 JSON 数字使用 `json.Number` 或字符串接收，转换后的金额保留四位小数，禁止经由 `float64`。
- `Normalize` 接收中性 SourceRecord 和只读 Mapping lookup，输出 `AcceptedObservation` 或 `RejectedRecord`，不触及 HTTP/SQL。
- `BuildSummary` 按 `(product,region,business_date)` 输出完全确定的 summary。
- 错误消息可含内部诊断但 Handler 只记录分类；对外不传递。

### Step 3：验证并提交

```bash
go test -race ./internal/ingestion -count=1
git diff --check
git add internal/ingestion
git commit -m "feat(ingestion): normalize price source records"
```

---

## Task 5：惠农网 Source adapter 与 fixture 契约

**文件：**

- 新建：`internal/ingestion/huinong/client.go`
- 新建：`internal/ingestion/huinong/client_test.go`
- 新建：`internal/ingestion/huinong/testdata/*.json`

### Step 1：写 adapter 契约测试

使用 `httptest.Server` 和脱敏 fixture，覆盖：

- 首页得到总页数，顺序拉取每页且刚好一次；
- 请求上下文取消与 15 秒可配置超时；
- 最大响应体、非 2xx、429、5xx、非法/重复/尾随 JSON、分页总数不一致；
- 必填源字段解析为中性 `SourceRecord`；
- 只有显式配置才可构造真实 adapter；
- 日志、错误和 record payload 不含 device ID、secret、签名或完整请求 URL。

运行：`go test -race ./internal/ingestion/huinong -count=1`

预期：FAIL。

### Step 2：实现受控 adapter

- 适配器封装现有字段调研到的 endpoint/分页请求，但不 import `internal/craw`。
- HTTP Client 限制响应大小，禁用跨域重定向，处理 `context.Canceled`。
- Source 对外只发 `SourceRecord` 给 sink；批次存储由 Ingestion service 管理。
- 数据源不启用或未配凭据时，Worker 注册 fixture/no-op Source，不触网。

### Step 3：验证并提交

```bash
go test -race ./internal/ingestion/huinong -count=1
go vet ./internal/ingestion/huinong
git diff --check
git add internal/ingestion/huinong
git commit -m "feat(ingestion): fetch Huinong price records safely"
```

---

## Task 6：分区、原子发布、快照与趋势 Repository

**文件：**

- 新建：`internal/pricing/publish.go`
- 新建：`internal/pricing/publish_test.go`
- 新建：`internal/pricing/read.go`
- 新建：`internal/pricing/read_test.go`
- 新建：`internal/pricing/postgres/publisher.go`
- 新建：`internal/pricing/postgres/publisher_test.go`
- 新建：`internal/pricing/postgres/migrations/000006_pricing_prices.up.sql`

### Step 1：写发布事务失败测试

- validated batch 发布后写 observation、summary、snapshot，并标记 published；
- 同 batch 第二次发布是幂等成功，不产生重复行；
- 任一 observation/summary/snapshot 写入失败时整个事务回滚，旧快照逐字不变；
- 月边界自动确保正确 RANGE 分区，分区名称来自已验证 `business_date`；
- 两个并发 publish 尝试只能得到一个物理发布；
- snapshot 查询永不读取未发布 batch。

### Step 2：写读取语义测试

- Current 按 product、可选 region descendants、稳定 cursor 查询 snapshot；
- Trend 只查询 daily summaries，按日期升序，最大 366 天；
- 金额扫描和 JSON 序列化均为字符串；
- `is_stale` 用注入的 Asia/Shanghai clock 计算；
- 无快照返回领域 `ErrPriceDataNotFound`。

### Step 3：实现迁移、Publisher 与 Reader

- `000006` 创建月分区父表、初始分区、daily summaries、current snapshots、索引和外键。
- Ingestion 的 `PublishCoordinator` 发起并拥有一个 PostgreSQL 事务，先锁 batch，再将同一 `platformpostgres.Tx` 交给 Pricing Publisher；Pricing 只写 Pricing 表，最后由 Ingestion 在该事务中标记 batch published。锁顺序固定为 batch -> observation partition -> summary -> snapshot。
- 以 `INSERT ... ON CONFLICT` 实现同 batch 幂等，不使用删除/重建当前公开数据的策略。
- Reader 使用 keyset pagination；region descendant 查询用递归 CTE 或受索引支持的路径字段，二者在实现前选择其一并测试查询计划。

### Step 4：验证并提交

```bash
TEST_DATABASE_URL=... go test -race ./internal/pricing/... -count=1
git diff --check
git add internal/pricing internal/bootstrap/migrate.go
git commit -m "feat(pricing): publish snapshots and daily trends"
```

---

## Task 7：Ingestion application、Job handler 与清理任务

**文件：**

- 新建：`internal/ingestion/service.go`
- 新建：`internal/ingestion/service_test.go`
- 新建：`internal/ingestion/jobs.go`
- 新建：`internal/ingestion/jobs_test.go`

### Step 1：写流程测试

用 fake Source、BatchRepository、Mapping lookup 和 Publisher 验证：

- fetch -> raw sink -> normalize -> validate -> publish 的顺序；
- source 429/5xx、网络超时返回 retryable error；
- 契约坏 JSON、总数不一致、duplicate conflict 和 reject ratio 超限返回 `jobs.Permanent`；
- 可重试 Source 失败将 batch 标记为可恢复失败；下一次相同 job 重试可安全重开同一 batch，且不重复 raw record；达到 Job 最大尝试后仍保留失败批次和旧 snapshot；
- 永久校验/发布失败不可自动重开，旧 snapshot 仍可读；
- job payload 只能是 `{"business_date":"YYYY-MM-DD"}`；
- raw prune 使用固定 now，最多删除 limit 条且不会触及有效数据。

### Step 2：实现服务和 Handler

- `FetchAndPublish(ctx,date,jobID)` 是 Worker 唯一采集入口。
- batch 已 published 时直接返回成功；只有失败码明确为 retryable 的 batch 能由同一业务键重开为 running，永久失败不可自动复用；重开前必须确认不存在已发布记录，且 raw append 仍受 `(batch_id, source_record_key)` 幂等约束保护。
- Handler 将错误分类转换为 `jobs.Permanent` 或普通 error。
- `RegisterJobs` 将 `ingestion.huinong.fetch` 和 `ingestion.raw.prune` 注册到 Runner，不在 Handler 内部自行启动 goroutine。

### Step 3：验证并提交

```bash
go test -race ./internal/ingestion -run 'Service|Job' -count=1
git diff --check
git add internal/ingestion
git commit -m "feat(ingestion): run idempotent price publish jobs"
```

---

## Task 8：每日调度、Worker Bootstrap 与运行时装配

**文件：**

- 新建：`internal/ingestion/schedule.go`
- 新建：`internal/ingestion/schedule_test.go`
- 修改：`internal/bootstrap/worker.go`
- 修改：`internal/bootstrap/bootstrap_test.go`
- 新建：`internal/bootstrap/pricing_ingestion_integration_test.go`
- 修改：`.env.v2.example`
- 修改：`docker-compose.v2.yaml`

### Step 1：写 scheduler 测试

- `Asia/Shanghai` 04:00 产生当日 fetch key；01:00 产生 prune key；
- Worker 启动后错过时间窗口仍尝试 enqueue 当日 key；
- 多次 tick 和多实例共享 Repository 时只创建一个 Job；
- UTC 日界线和夏令时无关（时区固定中国标准时间）。

### Step 2：写真实依赖集成测试

独立 PostgreSQL schema + 独立 Redis prefix，注入 fixture Source：

1. 迁移并启动 Worker handler；
2. enqueue fetch job 并运行一次；
3. 经 Pricing Reader 查询 current 与 trend；
4. 先发布前一业务日数据，再将注入时钟推进到当前业务日并让下一次 Source 失败，确认旧快照仍返回且 `is_stale=true`；
5. 插入过期 raw record 并运行 prune，确认只删除该记录；
6. 验证无残余 test schema、无共享表清理。

### Step 3：实现 Bootstrap

- Gateway 构造 Pricing Catalog/Reader 依赖（HTTP API 在 Task 9 挂载）；Worker 构造 Ingestion Repository、Publisher、Source、Service、Scheduler、Runner 注册。
- 开发 Compose 默认禁用真实惠农网；测试使用 fixture Source；生产因配置缺失而失败启动，不静默访问真实服务。
- Worker 和 Gateway 都只检查 migration version，不执行迁移。

### Step 4：验证并提交

```bash
TEST_DATABASE_URL=... TEST_REDIS_ADDR=... go test -race ./internal/bootstrap ./internal/ingestion/... ./internal/pricing/... -count=1
git diff --check
git add internal/bootstrap internal/ingestion .env.v2.example docker-compose.v2.yaml
git commit -m "feat(worker): schedule and assemble price ingestion"
```

---

## Task 9：公开 HTTP API 与 OpenAPI 契约

**文件：**

- 新建：`internal/pricing/httpapi/handler.go`
- 新建：`internal/pricing/httpapi/handler_test.go`
- 修改：`internal/bootstrap/gateway.go`
- 修改：`internal/gateway/openapi_test.go`
- 修改：`api/openapi.yaml`

### Step 1：写 HTTP 契约测试

逐项覆盖四条 GET 路由：

- 参数校验、limit、UUID、日期范围、游标绑定；
- 公开访问不要求 Bearer；
- 200 响应金额为 string，`next_cursor`、`latest_published_at`、`is_stale` 正确；
- 无数据 `404 price_data_not_found`；
- 无效游标 `400 invalid_cursor`，范围错误 `400 invalid_date_range`；
- Repository state 失败 `503 price_service_unavailable`；
- Problem 包含已有 request trace ID 与安全中文 detail。

运行：`go test ./internal/pricing/httpapi ./internal/gateway -count=1`

预期：FAIL。

### Step 2：实现 Handler 与 Gateway 挂载

- 使用私有 `http.ServeMux`，严格 query 参数解析，不读环境变量。
- 复用 `httpx` 的 Problem/request-id/CORS；不复制 Identity 的认证逻辑。
- Bootstrap 创建一个私有组合 mux：将 `/api/v1/auth/` 委派给既有 Identity handler，将 products/regions/prices 的精确路径委派给 Pricing handler，再把组合 mux 作为 Gateway 的唯一 `/api/v1/` API。不得将两个 handler 都注册为 `/api/v1/`，不得改变 health bytes。
- OpenAPI 补齐四个路径、查询 DTO、`Money` string schema、cursor、Problem 和所有可达状态。

### Step 3：验证并提交

```bash
go test -race ./internal/pricing/httpapi ./internal/gateway -count=1
go test ./internal/gateway -run OpenAPI -count=1
git diff --check
git add internal/pricing/httpapi internal/bootstrap/gateway.go internal/gateway api/openapi.yaml
git commit -m "feat(gateway): expose public price APIs"
```

---

## Task 10：最终验收、CI 与 Docker

**文件：**

- 修改：`Makefile`
- 修改：`.github/workflows/ci.yml`
- 修改：`docker-compose.v2.yaml`

### Step 1：扩展可复现验证目标

- `v2-test` 包含 Pricing、Ingestion、HTTP API、Bootstrap 的竞态测试；
- 新增 `v2-pricing-ingestion-integration`，只创建/删除 test-owned schema 与临时 Redis 前缀，不删除开发 named volume；
- CI 启动 PostgreSQL/Redis，使用 fixture，不配置真实惠农网凭据；
- Docker Compose Gateway/Worker 在默认开发配置下不触发外部采集。

### Step 2：运行完整验收

```bash
docker compose -f docker-compose.v2.yaml up -d --wait postgres redis
TEST_DATABASE_URL=... TEST_REDIS_ADDR=... GOTOOLCHAIN=go1.24.3 go test -count=1 -race ./internal/pricing/... ./internal/ingestion/... ./internal/platform/... ./internal/gateway/... ./internal/bootstrap/...
GOTOOLCHAIN=go1.24.3 go test ./...
GOTOOLCHAIN=go1.24.3 make v2-build
docker build -f Dockerfile.v2 -t agri-price-crawler-v2:pricing-ingestion .
docker compose -f docker-compose.v2.yaml config --quiet
```

记录全仓 `go vet ./...` 的既有、与本阶段无关告警；新增或涉及包的 vet 必须通过。

### Step 3：提交

```bash
git diff --check
git add Makefile .github/workflows/ci.yml docker-compose.v2.yaml
git commit -m "build(ci): verify price ingestion pipeline"
```

## 审查关卡

每一项提交后执行以下顺序：

1. 规格审查：实现是否逐条满足本计划和确认的 Spec；
2. 代码质量与安全审查：事务、锁、分区、金额精度、数据源脱敏、测试隔离、错误分类；
3. 有问题先修复并补回归测试，再由两类审查复核；
4. 只有两个审查都通过，才开始下一个任务。
