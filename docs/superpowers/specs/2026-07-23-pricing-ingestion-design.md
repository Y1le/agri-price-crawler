# Pricing 与 Ingestion 设计规格

日期：2026-07-23  
状态：已确认，待实施

## 1. 目标与范围

本阶段在已完成的 Identity 与 Platform 基础上，建设“价格可查询、采集可发布”的第一个业务闭环，为微信小程序、响应式 Web 和后续独立 App 提供统一的只读价格 API。

本阶段交付：

- 标准品类、标准农产品与行政地区目录；
- 惠农网（Huinong）首个受控数据源适配器；
- 采集批次、原始记录、校验、去重、标准化与原子发布；
- 当前价格快照与日级趋势汇总；
- `/api/v1/products`、`/api/v1/regions`、`/api/v1/prices/current`、`/api/v1/prices/trends`；
- Worker 每日调度、幂等任务、失败重试与原始记录清理；
- 真实 PostgreSQL 的事务、并发与端到端测试。

本阶段不交付：收藏、订阅、菜谱、通知、后台人工维护界面、多数据源、实时采集、旧 MySQL 数据迁移或旧 API 兼容。

旧 `internal/craw` 的 MySQL/Gin/GORM 实现只作为字段调研参考；新模块不得依赖它，也不得读取、迁移或保留其中的业务数据。

## 2. 设计原则

1. PostgreSQL 是价格业务唯一事实来源；Redis 不参与采集发布、价格查询或趋势计算的正确性。
2. 原始数据先落库，再标准化；失败或未发布批次绝不改变公开查询结果。
3. 一个数据源、一个业务日期最多只有一个已发布批次；重跑是幂等的。
4. 金额一律使用 PostgreSQL `NUMERIC` 与 Go 定点/字符串扫描，不使用 `float64` 持久化或比较。
5. 日期语义统一采用 `Asia/Shanghai` 业务日期；数据库时间使用 `TIMESTAMPTZ`（UTC）。
6. 所有分页使用稳定、不可依赖内部结构的 base64url 游标，禁止 offset 分页。
7. 价格读取为公开接口；后续收藏、订阅和个性化功能才使用 Identity Principal。
8. 只接入有权访问、允许自动化读取的数据源。适配器不得规避访问控制、验证码、频率限制或网站条款；凭据仅由环境配置提供，绝不写入日志、原始记录或响应。

## 3. 模块边界

```
Gateway HTTP -> pricing.Application (只读查询)
Worker jobs  -> ingestion.Application -> pricing.Application (发布)
                    |                         |
              Source adapter                 PostgreSQL
```

### 3.1 Pricing

Pricing 拥有标准目录、公开快照与趋势读取模型。它不发起网络请求，也不知道惠农网的 JSON 字段。

公开应用接口：

```go
type Catalog interface {
    ListProducts(context.Context, ProductQuery) (ProductPage, error)
    ListRegions(context.Context, RegionQuery) (RegionPage, error)
}

type Reader interface {
    CurrentPrices(context.Context, CurrentPriceQuery) (CurrentPricePage, error)
    PriceTrend(context.Context, TrendQuery) (TrendSeries, error)
}

type Publisher interface {
    Publish(context.Context, PublishTx, PublishedBatch) error
}

// PublishTx 由 Ingestion 的批次事务提供；Pricing 只使用它写自己的表。
type PublishTx interface {
    SQL() platformpostgres.Tx
}
```

`Publisher` 是 Ingestion 对 Pricing 的唯一写入入口。除迁移和预置目录种子外，其他模块不得直接写 Pricing 表。

### 3.2 Ingestion

Ingestion 拥有数据源适配、批次生命周期、原始记录、字段校验、标准化映射、去重与发布编排。它依赖 `pricing.Publisher`，但不依赖 HTTP、Identity 或未来订阅模块。

```go
type Source interface {
    Name() string
    Fetch(context.Context, FetchRequest, RecordSink) (FetchResult, error)
}

type RecordSink interface {
    Append(context.Context, SourceRecord) error
}
```

Source 只输出本模块定义的中性 `SourceRecord`；惠农网的分页、签名、字段拼写和响应结构被封装在 `internal/ingestion/huinong` 中。

### 3.3 Platform jobs

本阶段新增并注册以下任务：

| kind | business_key | 作用 | 最大尝试 |
|---|---|---|---:|
| `ingestion.huinong.fetch` | `huinong:YYYY-MM-DD` | 拉取、校验、标准化并发布该业务日期 | 3 |
| `ingestion.raw.prune` | `before:YYYY-MM-DD` | 删除已过 30 天保留期的原始记录 | 3 |

Jobs 表已有 `(kind, business_key)` 唯一约束。调度器和人工重跑都使用同一业务键；重复入队返回同一任务，而不是创建重复采集。

## 4. 领域模型与数据规则

### 4.1 目录

`pricing_categories`

- `id UUID`；
- `parent_id UUID NULL`，形成有限层级树；
- `name TEXT`、`slug TEXT UNIQUE`、`sort_order INT`；
- `active BOOLEAN`、`created_at`、`updated_at`。

`pricing_products`

- `id UUID`；
- `category_id UUID NOT NULL`；
- `name TEXT`、`slug TEXT UNIQUE`、`aliases TEXT[]`；
- `canonical_unit TEXT NOT NULL DEFAULT 'CNY/kg'`；
- `active BOOLEAN`、`created_at`、`updated_at`。

产品表示可被消费者理解和订阅的标准品种，例如“西红柿”“黄瓜”；源站分类和品种 ID 只属于 Ingestion 映射，不暴露为产品 ID。

`pricing_regions`

- `id UUID`；
- `parent_id UUID NULL`；
- `level TEXT CHECK ('province','city','district')`；
- `national_code TEXT UNIQUE`（行政区划代码）；
- `name TEXT`、`full_name TEXT`、`pinyin TEXT`、`active BOOLEAN`；
- `created_at`、`updated_at`。

目录在首个迁移中预置本期支持的产品和地区。没有人工管理 API；新映射或目录调整通过受审查的种子迁移完成。

### 4.2 数据源映射

`ingestion_product_mappings` 将 `(source_name, source_category_id, source_breed_id)` 唯一映射到 `pricing_products.id`；保存最后观察到的源名称，仅作审计。

`ingestion_region_mappings` 将 `(source_name, source_province_id, source_city_id, source_district_id)` 唯一映射到 `pricing_regions.id`；源站没有可靠区县 ID 时，映射键必须包含规范化后的地区文本与父级代码。

映射不可由采集器自动创建。未知产品或地区记录会保留为 `rejected` 原始记录；其是否阻断发布由第 5.4 节的完整性门槛决定。

### 4.3 采集批次与原始记录

`ingestion_batches`

- `id UUID PRIMARY KEY`；
- `source_name TEXT`、`business_date DATE`；
- `state TEXT CHECK ('running','failed','validated','published')`；
- `started_at`、`finished_at`、`published_at`；
- `source_expected_count INT NULL`、`fetched_count INT`、`accepted_count INT`、`rejected_count INT`；
- `failure_code TEXT NULL`、`failure_detail TEXT NULL`（仅安全分类，不保存凭据或响应正文）；
- `published_by_job_id BIGINT NULL`；
- 唯一约束 `(source_name, business_date)`。

`ingestion_raw_records`

- `id BIGSERIAL`、`batch_id UUID`；
- `source_record_key TEXT`；
- `payload JSONB`（经凭据字段清洗后）；
- `payload_hash BYTEA`；
- `observed_at TIMESTAMPTZ`、`validation_state TEXT CHECK ('pending','accepted','rejected')`；
- `reject_code TEXT NULL`、`expires_at TIMESTAMPTZ NOT NULL`；
- 唯一约束 `(batch_id, source_record_key)`，并为 `(expires_at)` 建索引。

原始记录保留 30 个自然日。清理任务只删除 `expires_at < now()` 的记录；绝不删除已发布的标准化价格历史。

### 4.4 标准化价格、快照与趋势

`pricing_price_observations` 按 `business_date` 做月度 RANGE 分区。

- `id BIGSERIAL`；
- `batch_id UUID`、`raw_record_id BIGINT`；
- `business_date DATE`、`observed_at TIMESTAMPTZ`；
- `product_id UUID`、`region_id UUID`；
- `market_name TEXT NOT NULL`（源地址规范化后的市场/区域标签）；
- `min_price NUMERIC(14,4)`、`max_price NUMERIC(14,4)`、`average_price NUMERIC(14,4)`；
- `sample_count INT NULL`；
- `unit TEXT NOT NULL DEFAULT 'CNY/kg'`；
- `source_name TEXT`、`source_record_key TEXT`；
- 唯一约束 `(batch_id, source_record_key)`；
- 查询索引 `(product_id, region_id, business_date DESC)`。

所有源单位必须能明确换算为 `CNY/kg`，例如“元/斤”乘以 2；无法换算、价格为负、最小价大于最大价、平均价不在合理范围内或必要维度缺失的记录均拒绝。`NUMERIC` 的精度至少保留四位小数；API 以字符串金额返回，避免客户端浮点误差。

`pricing_daily_summaries`

- 主键 `(product_id, region_id, business_date)`；
- `min_price`、`max_price`、`average_price NUMERIC(14,4)`；
- `observation_count INT`、`weighted_sample_count BIGINT`；
- `source_batch_id UUID`、`computed_at TIMESTAMPTZ`。

若该产品地区的所有有效 observation 都带正 `sample_count`，平均价按 `sample_count` 加权；否则使用 observation 的简单算术平均。最小、最大值始终取全部有效 observation 的边界。该规则是确定性的，重跑同一批次生成相同汇总。

`pricing_current_snapshots`

- 主键 `(product_id, region_id)`；
- 保存与 `pricing_daily_summaries` 相同的价格字段；
- `business_date`、`source_batch_id`、`published_at`。

公开“当前”数据永远读取快照；趋势永远读取日级汇总，不扫描原始 observation 分区。

## 5. 采集与发布流程

### 5.1 每日调度

Worker 使用 `Asia/Shanghai` 时区。每天 04:00 创建 `ingestion.huinong.fetch`，业务键为该日 `huinong:YYYY-MM-DD`。Worker 启动时若当天已过 04:00，也会尝试补入当天任务；唯一键保证多实例和重启不会重复执行。

每日 01:00 创建 `ingestion.raw.prune`。调度器不需要 Redis；调度逻辑和 Job Repository 共同保证至少一次触发、业务写入幂等。

### 5.2 获取

适配器必须：

- 使用显式超时、请求上下文和受控分页；
- 校验 HTTP 状态、响应大小、JSON 结构与分页总数；
- 将每条源记录写入当前 running batch，再继续下一页；
- 对 429、网络超时和 5xx 返回可重试错误；
- 对认证失败、契约结构变化、分页矛盾和无效必填字段返回不可重试错误；
- 不记录请求签名、设备标识、Secret、完整响应正文或可识别凭据。

惠农网适配器可复用旧实现调研到的业务字段：源分类/品种 ID、名称、最小/最大/平均价、加权均价、单位、地区 ID、地址文本、统计样本数和源时间。但新的适配器必须通过中性 Source 接口输出，且不得将旧 HTTP/Gin/MySQL 包带入新架构。

### 5.3 校验、去重和标准化

获取结束后，在同一批次中依序进行：

1. 校验 `fetched_count` 与源声明总数（若源提供）一致；
2. 对 `(source_record_key)` 去重；同键 payload hash 不同视为源数据矛盾并失败；
3. 按映射解析产品、地区与市场名称；
4. 统一单位、金额和观测时间；
5. 为每条记录标记 accepted/rejected，并构造规范化 observation；
6. 计算发布门槛与日级汇总。

单条拒绝原因使用稳定代码：`unknown_product_mapping`、`unknown_region_mapping`、`invalid_unit`、`invalid_price`、`invalid_timestamp`、`duplicate_conflict`。对外 API 不暴露原始拒绝详情。

### 5.4 发布门槛和原子性

只有同时满足以下条件的 batch 可发布：

- 状态为 `validated`；
- 至少有 1 条 accepted record；
- `rejected_count / fetched_count <= 5%`；
- 源提供总数时，`fetched_count == source_expected_count`；
- 不存在重复键 payload 冲突；
- 所有将被公开的价格均已换算为 `CNY/kg`。

发布在一个 PostgreSQL 事务中完成：

1. 锁定 `(source_name, business_date)` batch；
2. 确认它尚未 published；
3. 写入 observation 分区和对应 daily summaries；
4. upsert current snapshots；
5. 标记 batch 为 `published` 并记录 `published_at`。

任一步骤失败必须回滚。失败 batch 标记为 `failed`，保留上一个已发布快照。已发布 batch 不允许覆盖或再次发布；若必须修正数据，使用新的显式人工业务日期修复流程，该流程不在本期范围内。

## 6. Gateway API

所有响应为 `application/json`；错误继续使用已有 `application/problem+json`、中文安全 `detail` 和 `trace_id`。本期四个价格 API 均不需要登录。

### 6.1 `GET /api/v1/products`

查询参数：

- `q`：可选，按名称、拼音、alias 前缀检索，1–64 字符；
- `category_id`：可选 UUID；
- `cursor`：可选不透明游标；
- `limit`：可选，默认 20，范围 1–50。

只返回 active 产品，按 `name ASC, id ASC` 稳定排序。响应含 `items` 与 `next_cursor`。

### 6.2 `GET /api/v1/regions`

查询参数：`q`、`parent_id`、`cursor`、`limit`。`q` 为名称/拼音前缀；`parent_id` 限制子地区。排序为 `level, full_name, id`。只返回 active 地区。

### 6.3 `GET /api/v1/prices/current`

参数：

- `product_id`：必填 UUID；
- `region_id`：可选 UUID。指定父地区时包含其活动后代地区；
- `cursor`、`limit`（默认 20、最大 50）。

每项包含 `product`、`region`、`min_price`、`max_price`、`average_price`、`unit`、`observation_count`、`business_date`、`published_at`、`is_stale`。

`is_stale` 在该快照的 `business_date` 早于当前 `Asia/Shanghai` 日期时为 true。响应顶层还返回 `latest_published_at`；若没有任何可用快照，返回 `404 price_data_not_found`，而不是空数组伪装为最新数据。

### 6.4 `GET /api/v1/prices/trends`

参数：

- `product_id`：必填 UUID；
- `region_id`：必填 UUID；
- `from`、`to`：必填 `YYYY-MM-DD`，包含边界；
- 最大跨度 366 天，且 `from <= to`。

响应返回按 `business_date ASC` 排序的每日点，每点字段为 `business_date`、`min_price`、`max_price`、`average_price`、`unit`、`observation_count`、`is_stale`。没有数据的日期不虚构零值；客户端根据缺失点展示空档。

### 6.5 游标与错误码

游标是 base64url 编码、版本化的 JSON 排序位置；服务端必须验证版本、字段和过滤条件一致性。无效、跨查询复用或过期格式返回 `400 invalid_cursor`。

本模块新增稳定错误码：

- `invalid_cursor`；
- `invalid_date_range`；
- `price_data_not_found`；
- `price_data_delayed`（仅在未来需要把“数据滞后”提升为非 200 策略时使用；本期通过 `is_stale` 表达）；
- `price_service_unavailable`。

OpenAPI 是唯一外部契约；实施时必须同时扩展 `api/openapi.yaml` 和 Gateway 契约测试。

## 7. 配置与安全

新增配置只允许由 Gateway/Worker Bootstrap 构造后传入模块，业务包不得自行读环境变量：

- `INGESTION_HUINONG_ENABLED`（开发默认 `false`，生产显式开启）；
- `INGESTION_HUINONG_BASE_URL`；
- `INGESTION_HUINONG_TIMEOUT`（默认 15s，上限 30s）；
- `INGESTION_HUINONG_DEVICE_ID`、`INGESTION_HUINONG_SECRET`；
- `INGESTION_REJECT_RATIO_MAX`（默认 `0.05`，范围 `[0,0.20]`）；
- `INGESTION_RAW_RETENTION`（默认 `720h`，最小 `24h`）；
- `INGESTION_SCHEDULE_TIMEZONE`（固定允许 `Asia/Shanghai`）。

生产环境必须要求 HTTPS 数据源地址、非空数据源凭据和明确的启用值。开发环境可使用 `httptest`/fixture Source，但不能在未配置凭据时意外访问真实数据源。

日志仅记录 source、batch ID、业务日期、页号、记录计数、错误分类和 trace/job ID。禁止记录 Source Secret、签名、完整原始 payload、完整 URL query 或原始请求/响应头。

## 8. 迁移与依赖注册

Pricing 与 Ingestion 各自拥有迁移目录，使用全局唯一版本号（紧随当前 `000003_identity.up.sql`）。`internal/bootstrap/migrate.go` 将两个 `migrate.Source` 注册进 catalog；Gateway 与 Worker 的 schema version 检查因此覆盖所有业务模块。

分区创建采用受控的月度维护函数或 migration helper：发布前确保目标 `business_date` 对应分区存在。不得使用来自 HTTP 或 Source 的未验证 SQL 标识符拼接分区名称。

## 9. 可观测性

至少提供以下结构化日志和 Prometheus 指标：

- batch 状态、耗时、fetched/accepted/rejected 计数；
- 最近一次已发布批次的业务日期和时间；
- Source 请求总数、失败数、限流数与耗时；
- 当前价格查询和趋势查询耗时/错误数；
- Job 积压、重试和 dead 数量（复用 Platform jobs 指标）。

Gateway `/readyz` 继续以 PostgreSQL 为主要就绪依据；数据源或 Redis 故障不能使已发布价格查询、已有会话或 Refresh Token 不可用。

## 10. 测试与验收

### 10.1 单元与契约测试

- 金额/单位换算、地区与产品映射、拒绝比例、汇总加权规则、游标编解码；
- 惠农网适配器使用脱敏 fixture，覆盖分页、429/5xx、超大响应、坏 JSON、总数不一致和字段缺失；
- Source 不得访问真实网络的测试；
- HTTP 参数、Problem、游标、价格字符串精度、`is_stale` 与 OpenAPI 覆盖。

### 10.2 PostgreSQL 集成测试

- 迁移幂等、月分区创建与分区查询计划；
- 同一 source/date 并发入队和发布仅得到一个 published batch；
- failed batch 不改变之前快照；
- 相同 batch 重跑不重复 observation/summary；
- 独立测试 schema，绝不 TRUNCATE 开发公共表。

### 10.3 Worker 端到端测试

使用受控 Source 运行 `ingestion.huinong.fetch`：拉取 fixture -> 写原始记录 -> 标准化 -> 发布 -> Gateway 查询 current/trends。关闭 Source 后再次执行，断言旧快照仍可读且标记 stale；验证原始记录保留期清理只删除过期原始数据。

### 10.4 阶段验收

1. 空 PostgreSQL 可执行完整迁移并预置目录；
2. 同日重复调度不会产生重复任务或公开价格；
3. 已发布批次能通过四个公开 API 查询；
4. 失败、拒绝比例超限或分页不完整的批次绝不覆盖旧快照；
5. 价格金额以字符串精确返回，趋势使用日级汇总；
6. 真实 PostgreSQL 集成、竞态测试、OpenAPI 契约、Worker 测试、Gateway/Worker/Docker 构建全部通过；
7. 不读取、迁移或依赖旧 MySQL 表、旧 Gin 路由或旧爬虫运行入口。

## 11. 后续阶段接口

Subscription 将通过 Pricing 的 `Reader` 查询标准产品、地区与已发布快照；不得访问 Ingestion 的原始记录。Recipe 和 Notification 只消费已发布的 daily summary/snapshot 及后续 Outbox 事件。账户合并时，未来模块作为 Identity merge participant 合并各自拥有的收藏、订阅和偏好数据。
