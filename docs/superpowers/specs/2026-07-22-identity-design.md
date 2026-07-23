# Identity 模块设计

**日期：** 2026-07-22
**状态：** 已确认
**所属系统：** Agri Price Crawler v2 后端

## 1. 目标

Identity 是模块化单体中的账户与认证边界，为微信小程序、响应式 Web 和未来独立 App 提供统一身份模型。本阶段交付以下能力：

- 微信小程序临时凭证登录；
- 邮箱验证码登录，首次验证成功时自动创建账户；
- 微信身份与邮箱身份绑定到同一账户；
- 已分别创建账户的身份在双重验证后安全合并；
- Web、小程序和未来 App 的多设备会话；
- Access Token 签发与 Refresh Token 轮换、撤销和重用检测；
- Gateway 认证中间件、统一错误响应和 OpenAPI 契约；
- SMTP、微信、PostgreSQL 和 Redis 的明确适配器边界。

本阶段不迁移旧 MySQL 用户，不兼容旧认证 API，也不复用旧 Gin 认证中间件或全局邮件实例。

## 2. 非目标

本阶段不实现：

- 密码登录、找回密码或多因素认证；
- 头像、昵称等用户资料编辑；
- 管理员账户和后台用户管理；
- 微信以外的社交登录；
- 邮件营销、订阅通知或群发能力；
- 用户自行解绑最后一种登录身份；
- 独立身份微服务或外部身份平台。

## 3. 模块边界

Identity 采用模块化纵向切片，公开应用接口，隐藏存储与供应商细节。建议目录如下：

```text
internal/identity/
  service.go              # 公开应用服务与用例
  domain.go               # 用户、身份、会话和合并规则
  ports.go                # 仓储、验证码、微信、邮件和时钟端口
  postgres/               # PostgreSQL 仓储和模块迁移
  redisotp/               # 验证码与限流
  wechat/                 # 微信 jscode2session 适配器
  smtp/                   # 单封验证码邮件适配器
  httpapi/                # /api/v1/auth 与 /api/v1/me
```

依赖方向固定为：HTTP 适配器调用 Identity 应用服务，应用服务依赖端口，PostgreSQL、Redis、微信和 SMTP 实现这些端口。Gateway 只负责挂载路由、通用请求追踪、CORS 和认证上下文，不包含账户业务规则。

Identity 可以依赖 Platform 提供的配置、迁移执行器、日志和 PostgreSQL 事务抽象；Platform 不得反向依赖 Identity。Bootstrap 负责组装具体适配器。

## 4. 持久化模型

PostgreSQL 是账户、身份和会话的唯一事实来源。所有 ID 使用应用侧生成的 UUID，时间使用 `TIMESTAMPTZ`。

### 4.1 `identity_users`

- `id`：主键；
- `status`：`active`、`merged` 或 `disabled`；
- `merged_into_user_id`：被合并账户指向的主账户，仅 `merged` 状态允许设置；
- `created_at`、`updated_at`。

数据库约束必须保证活跃账户没有 `merged_into_user_id`，被合并账户必须指向另一个账户，禁止自引用。

### 4.2 `identity_identities`

- `id`：主键；
- `user_id`：所属账户；
- `kind`：`email` 或 `wechat_mini`；
- `issuer`：邮箱固定为 `email`，微信为小程序 AppID；
- `subject`：邮箱为规范化地址，微信为 OpenID；
- `union_id`：微信返回时保存，可为空；
- `verified_at`、`created_at`。

`(kind, issuer, subject)` 必须唯一。邮箱去除首尾空格后转为小写。微信身份以 `AppID + OpenID` 唯一识别；UnionID 仅作为未来同一微信开放平台下跨应用识别的依据，本阶段不凭 UnionID 自动合并账户。

### 4.3 `identity_sessions`

- `id`：主键，也是 JWT 的会话标识；
- `user_id`：所属账户；
- `client_kind`：`web`、`wechat_mini`，预留 `app`；
- `created_at`、`last_seen_at`、`expires_at`；
- `revoked_at`、`revoked_reason`。

同一账户可以同时拥有多个有效会话。每个设备或客户端登录创建独立会话。刷新时将会话空闲有效期滚动至 30 天后。

### 4.4 `identity_refresh_tokens`

- `id`：主键；
- `session_id`：所属会话；
- `token_hash`：Refresh Token 的 SHA-256 哈希，唯一；
- `created_at`、`expires_at`；
- `consumed_at`：成功轮换的时间；
- `replacement_token_id`：轮换后的令牌；
- `revoked_at`。

数据库绝不保存 Refresh Token 原文。已消费令牌保留到其自然过期，以支持重用检测和审计。

### 4.5 `identity_account_merges`

- `id`：主键；
- `primary_user_id`：创建时间更早的主账户；
- `secondary_user_id`：被合并账户，唯一；
- `initiated_by_session_id`：发起合并的已验证会话；
- `created_at`。

合并记录不可删除或改写。业务日志引用合并记录 ID，不记录完整邮箱、OpenID 或 Token。

## 5. Redis 数据

Redis 仅保存可重建、短生命周期的认证数据：

- 邮箱验证码 HMAC 摘要；
- 剩余尝试次数；
- 60 秒发送冷却；
- 邮箱和来源 IP 的小时级限流计数；
- 微信登录与绑定入口的来源 IP 限流计数。

验证码为使用密码学安全随机源生成的 6 位数字，有效期 10 分钟，最多尝试 5 次。Redis Key 使用规范化邮箱的摘要，不暴露邮箱原文。验证码 Key 还包含用途：登录验证码不能用于绑定，绑定验证码必须绑定到发起请求的当前用户。

发送、验证、递减次数和消费验证码使用原子 Redis 操作。SMTP 发送失败时，只删除与本次请求匹配的验证码和冷却记录；小时级限流计数保留，防止利用失败请求绕过滥用保护。

默认限额为每邮箱每小时 5 次、每来源 IP 每小时 30 次，可通过环境变量调整。只有明确配置的可信反向代理才可以提供客户端 IP。

微信登录与绑定使用独立、可配置的来源 IP 限额，避免攻击者借 Gateway 放大对微信接口的调用。需要 Redis 限流但无法读取限流状态时，认证入口安全失败并返回 `503`，不绕过限流继续访问上游。

## 6. HTTP API

OpenAPI 是唯一外部接口契约。Identity 增加以下路由：

| 方法 | 路径 | 认证 | 用途 |
|---|---|---|---|
| `POST` | `/api/v1/auth/email/code` | 否 | 发送邮箱登录验证码 |
| `POST` | `/api/v1/auth/email/login` | 否 | 邮箱验证码登录 |
| `POST` | `/api/v1/auth/wechat/login` | 否 | 微信小程序登录 |
| `POST` | `/api/v1/auth/refresh` | Refresh Token | 轮换会话令牌 |
| `POST` | `/api/v1/auth/logout` | Access Token | 退出当前会话 |
| `POST` | `/api/v1/auth/logout-all` | Access Token | 退出当前账户全部会话 |
| `POST` | `/api/v1/auth/bind/email/code` | Access Token | 发送邮箱绑定验证码 |
| `POST` | `/api/v1/auth/bind/email` | Access Token | 验证并绑定邮箱 |
| `POST` | `/api/v1/auth/bind/wechat` | Access Token | 验证并绑定微信 |
| `GET` | `/api/v1/me` | Access Token | 获取当前账户及绑定身份摘要 |

登录请求显式携带 `client_kind`。本阶段接受 `web` 和 `wechat_mini`，尚未发布的 `app` 值返回参数错误。

登录和刷新成功响应包含：

- `access_token`；
- `token_type: Bearer`；
- `expires_in: 900`；
- 当前用户摘要；
- 非 Web 客户端的 `refresh_token`。

Web 的 Refresh Token 不进入 JSON，而是写入 Cookie。小程序的 Refresh Token 通过 JSON 返回，由微信客户端安全存储。刷新接口要求请求只能提供一种 Refresh Token 来源，避免 Cookie 与请求体含义不一致。

邮箱验证码发送成功统一返回 `202 Accepted`。响应不区分邮箱是否已有账户。首次成功验证邮箱或微信身份时，Identity 在事务中创建用户和身份，并立即创建会话。

`GET /api/v1/me` 只返回账户 ID、状态、创建时间和脱敏后的已绑定身份，不返回 OpenID、UnionID、完整邮箱或任何供应商凭据。

## 7. 邮箱登录与 SMTP

Identity 定义单封验证码邮件发送端口。生产 SMTP 必须使用 `implicit` 或 `starttls` TLS 模式，并提供用户名和密码完成认证。开发环境可使用内存捕获适配器，也可将 TLS 模式显式配置为 `none` 连接 Mailpit 等 Docker 邮件捕获器；`none` 模式必须跳过 SMTP 认证，允许用户名和密码为空，并且不得在明文连接上传输任何凭据。TLS 模式仍必须提供用户名和密码。

请求流程为：

1. 规范化并校验邮箱；
2. 原子检查冷却和限流；
3. 生成验证码，在 Redis 写入 HMAC 摘要与尝试次数；
4. 使用短超时同步发送邮件；
5. 发送成功后返回 `202`；发送失败则清理本次验证码并返回可重试的 `503`。

SMTP 密码、验证码、完整收件地址和邮件认证响应不得写入日志。旧 `internal/craw/mailer` 不被新模块引用。

## 8. 微信登录

小程序将 `wx.login` 获得的一次性临时凭证提交给 Gateway。微信适配器使用配置的 AppID 与 AppSecret 调用 `jscode2session`，校验 HTTP 状态、微信错误码、OpenID 和响应格式。

Identity 使用 `AppID + OpenID` 查找身份。不存在时自动创建账户；存在时为所属活跃账户创建新会话。微信返回的 `session_key` 只存在于当前请求内，不写入数据库、不返回客户端、不进入日志。UnionID 存在时更新到身份记录，但不作为本阶段自动合并不同账户的依据。

微信接口使用明确连接和响应超时。参数或凭证错误不可重试；网络错误、限流和微信 5xx 映射为可重试的上游不可用错误。

## 9. 身份绑定与账户合并

绑定必须同时证明：

1. 当前账户的有效 Access Token；
2. 待绑定邮箱的有效绑定验证码，或待绑定微信身份的有效临时凭证。

发放绑定验证码、完成绑定、账户合并和退出全部设备时，应用服务必须在 PostgreSQL 中再次确认 JWT 对应会话仍然有效。仅通过签名但会话已经撤销的 Access Token 不得执行这些敏感操作。

若身份未被占用，在事务中绑定到当前账户。若已属于当前账户，操作幂等成功。若已属于另一活跃账户，则执行账户合并：

1. 对两个用户按稳定顺序加行锁，防止交叉合并死锁；
2. 以 `created_at` 更早者为主账户，相同时按 UUID 排序；
3. 依次调用已注册模块的合并处理器，由各模块只处理自己的表；
4. 将所有身份归入主账户；
5. 将次账户标记为 `merged` 并指向主账户；
6. 写入不可变合并审计记录；
7. 撤销两个账户的全部旧会话；
8. 为主账户创建一个属于当前客户端的新会话；
9. 提交事务后签发新的 Access Token，并交付新的 Refresh Token。

当前只有 Identity 数据时，合并处理器集合可以为空。未来 Subscription 等模块加入后，Bootstrap 注册其合并处理器。任一处理器失败会回滚整个事务，避免部分模块已经合并而其他模块尚未合并。

系统绝不根据相似邮箱、客户端声明或未验证的 UnionID 自动合并账户。

## 10. Token 与会话安全

### 10.1 Access Token

Access Token 使用 Ed25519 签名的 JWT，有效期 15 分钟，至少包含：

- `sub`：用户 ID；
- `sid`：会话 ID；
- `iss`、`aud`、`iat`、`exp`；
- `jti`：令牌唯一 ID；
- `kid`：签名密钥 ID，位于 JWT Header。

认证中间件只接受明确配置的算法、签发方、受众和公钥。Ed25519 公钥可供未来拆分后的服务独立验证，私钥只属于签发 Identity 会话的进程配置。

退出或账户合并后，已签发 Access Token 最多继续有效到 15 分钟自然过期；本阶段不为每个 API 请求查询会话表或维护 JWT 黑名单。

普通已认证读取依赖 JWT 自包含声明，不查询会话表。绑定、账户合并、发放绑定验证码和退出全部设备等敏感操作必须额外检查 PostgreSQL 会话状态，以缩短被撤销令牌对账户安全操作的影响窗口。

### 10.2 Refresh Token

Refresh Token 使用 32 字节安全随机数并编码为不透明字符串，有效期滚动 30 天。刷新操作在 PostgreSQL 事务中锁定当前令牌和会话，消费旧令牌、创建新令牌并延长会话有效期。

正常并发重试可能让旧令牌在极短时间内重复出现。系统在可配置的短宽限期内拒绝该请求但不撤销会话；宽限期后再次使用已消费令牌视为令牌泄露，撤销该设备会话。服务不保存替换令牌原文，因此并发失败请求不能重新取得已成功签发的令牌，只能使用最先成功响应中的新令牌或重新登录。

退出当前设备撤销当前会话及其全部 Refresh Token；退出全部设备撤销该用户全部会话。

### 10.3 Web Cookie 与跨域

Web Refresh Token Cookie 使用：

- `HttpOnly`；
- 生产环境 `Secure`；
- `SameSite=Lax`；
- 路径限制为 `/api/v1/auth`；
- 与 Refresh Token 一致的过期时间。

使用 Cookie 的写请求校验 `Origin`。只有配置允许的 Web Origin 可以跨域携带凭据。生产环境启动时如果 Cookie 未启用 `Secure`、允许通配凭据来源或密钥配置无效，配置校验必须失败。

## 11. 错误模型

所有错误使用 `application/problem+json`，包含：

- `type`；
- `title`；
- `status`；
- 稳定的 `code`；
- `trace_id`；
- 可安全展示的中文 `detail`。

至少定义以下稳定错误码：

- `invalid_request`；
- `invalid_email_code`；
- `email_code_expired`；
- `rate_limited`；
- `invalid_wechat_code`；
- `invalid_access_token`；
- `invalid_refresh_token`；
- `refresh_token_reused`；
- `identity_conflict`；
- `account_disabled`；
- `email_delivery_unavailable`；
- `wechat_unavailable`；
- `service_not_ready`。

日志使用内部错误与 Trace ID 排障，客户端响应不暴露数据库、Redis、SMTP 或微信原始错误。

## 12. 故障行为

| 故障 | 行为 |
|---|---|
| PostgreSQL 不可用 | Gateway readiness 失败；Identity 写入与 Refresh Token 操作不可用 |
| Redis 不可用 | 邮箱验证码及依赖限流的新登录、绑定入口返回 `503` |
| SMTP 不可用 | 清理本次验证码，返回 `email_delivery_unavailable` |
| 微信不可用 | 不创建账户或绑定，返回 `wechat_unavailable` |
| JWT 配置无效 | 进程启动失败，不使用临时随机密钥兜底 |
| 合并处理器失败 | 整个账户合并事务回滚，旧账户与会话保持原状 |

已有有效 JWT 的普通鉴权不依赖 Redis。Refresh Token、退出和会话撤销只依赖 PostgreSQL，因此 Redis 故障不会迫使已登录用户立即退出。

## 13. 配置

新增配置组至少覆盖：

- JWT 私钥、密钥 ID、签发方、受众和时长；
- Refresh Token 时长与重用宽限期；
- OTP HMAC Pepper、有效期、尝试次数、冷却和小时限额；
- 微信 AppID、AppSecret、接口地址、超时和入口 IP 限额；
- SMTP 主机、端口、发件地址、TLS 与超时，以及 TLS 认证模式所需的用户名和密码；
- Web Cookie 名称、安全属性和允许的 Origin；
- 可信反向代理列表。

生产环境必需密钥缺失或安全属性不满足要求时启动失败。开发环境允许使用显式配置的内存邮件适配器，或显式选择不认证且不传输凭据的 `none` SMTP 邮件捕获器模式；不得静默退化或把验证码输出到普通日志。

## 14. 模块迁移注册

模块 SQL 位于 `internal/identity/postgres/migrations/*.up.sql` 并由 Identity 包嵌入。Platform 迁移包提供通用 `Source`/注册表和事务执行能力，Bootstrap 将 Platform 与 Identity 的迁移源传给迁移命令。

迁移版本仍使用全局单调版本，注册表在执行前检查重复版本和非法文件名；冲突立即失败。Gateway 启动时检查已应用版本是否达到当前已注册迁移集合的最大版本。这样 SQL 由模块拥有，同时 Platform 不需要导入任何业务模块。

本阶段只做前向迁移，不提供旧 MySQL 数据导入或回滚脚本。

## 15. 测试策略

测试分为四层：

1. **领域单元测试**：首次登录、重复登录、身份绑定、账户主次选择、幂等合并、会话撤销和令牌重用规则；
2. **适配器测试**：使用真实 PostgreSQL/Redis 验证唯一约束、事务、原子验证码次数、限流和轮换竞争；
3. **HTTP 契约测试**：逐条验证 OpenAPI 路由、状态码、Problem Details、Cookie 属性和认证中间件；
4. **Bootstrap 测试**：验证配置失败、迁移注册、Gateway 装配以及外部服务替身注入。

默认测试不访问真实微信或 SMTP。微信使用 `httptest` 服务器，邮件使用内存捕获适配器。竞态敏感的会话和验证码测试必须运行 Go race detector；完整仓库测试与 v2 二进制构建必须继续通过。

## 16. 验收标准

满足以下条件后 Identity 阶段完成：

1. 邮箱与微信首次验证都能自动创建账户并登录；
2. 同一身份并发首次登录只创建一个用户；
3. Web、小程序可同时保持独立会话；
4. Web 不在 JSON 暴露 Refresh Token，小程序可以安全取得它；
5. Refresh Token 每次使用后轮换，过期、撤销和宽限期外重用均按设计处理；
6. 邮箱和微信身份可绑定到同一账户；
7. 两个已有账户经双重验证后原子合并，旧会话全部撤销；
8. Redis、SMTP、微信或 PostgreSQL 故障时没有半完成账户、绑定或合并；
9. 敏感凭据、验证码、Token 和完整身份标识不进入日志或错误响应；
10. OpenAPI、实现、测试和环境配置保持一致；
11. `go test ./...`、Identity 竞态测试和 v2 Docker 构建通过。
