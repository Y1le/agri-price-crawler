# Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the v2 Identity module for passwordless email and WeChat mini-program login, safe account binding and merging, and revocable multi-device sessions behind the Gateway REST API.

**Architecture:** Identity is a vertical module with application and domain rules in `internal/identity`, provider adapters in child packages, and HTTP transport in `internal/identity/httpapi`. PostgreSQL owns durable users, identities, sessions and token history; Redis owns only OTP and rate-limit state; Bootstrap injects all adapters into the unified Gateway.

**Tech Stack:** Go 1.24.3, `net/http`, PostgreSQL 16 with `pgx/v5`, Redis 7 with `go-redis/v9`, Ed25519 JWT with `golang-jwt/jwt/v4`, OpenAPI 3.1, Docker Compose.

## Global Constraints

- Preserve the modular monolith: Gateway is the single public entry point and Identity must not import legacy `internal/craw` code.
- PostgreSQL is the only durable source of truth; Redis stores OTPs, rate limits and other rebuildable short-lived state only.
- Email OTPs are six digits, valid for 10 minutes, allow at most 5 attempts, and cannot be resent to the same email within 60 seconds.
- Default send limits are 5 per email per hour and 30 per source IP per hour; all limits remain configurable.
- Access Tokens are Ed25519 JWTs valid for 15 minutes.
- Refresh Tokens are 32 random bytes, stored only as SHA-256 hashes, valid on a rolling 30-day window, and rotated on every use.
- Web receives the Refresh Token only in an `HttpOnly`, production-`Secure`, `SameSite=Lax` Cookie; mini-program clients receive it in JSON.
- First successful email or WeChat verification automatically creates an account.
- Multiple Web and mini-program sessions may coexist; logout-current and logout-all are separate operations.
- Binding requires a current active session plus proof of the new identity; merging chooses the older account, revokes both accounts' sessions, and is atomic.
- UnionID may be stored but must never auto-merge accounts in this phase.
- Ordinary authenticated reads validate the self-contained JWT without a database lookup; binding, merging, binding-code issuance and logout-all recheck the durable session.
- OpenAPI is the only public API contract; errors use `application/problem+json` with stable codes, a trace ID and safe Chinese detail.
- Never log OTPs, Token plaintext, SMTP/WeChat secrets, full email addresses, OpenID or UnionID.
- Do not migrate old MySQL users or preserve old auth API compatibility.
- Do not add password login, MFA, profile editing, administrator accounts, other social providers, notification mail, or identity-unbind endpoints.
- Use TDD for every task and commit only after that task's focused tests pass.

---

## File Structure Map

### Platform and Gateway

- `internal/platform/migrate/source.go`: migration source catalog, global-version validation and latest-version calculation.
- `internal/platform/migrate/migrate.go`: execute an explicit migration catalog while preserving the default Platform source.
- `internal/platform/postgres/postgres.go`: shared transaction executor interface used by merge participants.
- `internal/platform/config/config.go`: validated Identity, SMTP, WeChat, JWT, Cookie and CORS settings.
- `internal/platform/httpx/problem.go`: Problem Details model and writer.
- `internal/platform/httpx/requestid.go`: request Trace ID generation and context access.
- `internal/platform/httpx/cors.go`: exact-origin CORS and Cookie-origin protection.
- `internal/platform/httpx/clientip.go`: trusted-proxy-aware source IP resolution.
- `internal/gateway/server.go`: health routes plus an injected `/api/v1/` handler.

### Identity module

- `internal/identity/model.go`: domain values and returned models.
- `internal/identity/errors.go`: stable sentinel errors and classifications.
- `internal/identity/ports.go`: repository, OTP, email, WeChat and merge contracts.
- `internal/identity/service.go`: dependency validation and shared session creation helpers.
- `internal/identity/token.go`: Ed25519 Access Token and opaque Refresh Token logic.
- `internal/identity/login.go`: email and WeChat code/login use cases.
- `internal/identity/session.go`: refresh, logout, logout-all and current-user use cases.
- `internal/identity/bind.go`: binding and atomic account merge orchestration.
- `internal/identity/postgres/migrations/000003_identity.up.sql`: Identity schema.
- `internal/identity/postgres/migrations.go`: embedded Identity migration source.
- `internal/identity/postgres/repository.go`: transactional Identity repository.
- `internal/identity/redisotp/store.go`: atomic OTP and rate-limit scripts.
- `internal/identity/smtp/sender.go`: timeout-bounded SMTP verification-code sender.
- `internal/identity/wechat/client.go`: `jscode2session` adapter.
- `internal/identity/httpapi/handler.go`: route registration and request/response DTOs.
- `internal/identity/httpapi/auth.go`: Bearer parsing and authenticated context.
- `internal/identity/httpapi/cookie.go`: Web Refresh Token Cookie policy.

### Assembly, contract and docs

- `internal/bootstrap/migrate.go`: register Platform and Identity migration sources.
- `internal/bootstrap/gateway.go`: construct the complete Identity graph and inject it into Gateway.
- `api/openapi.yaml`: all Identity routes, schemas and security schemes.
- `docker-compose.v2.yaml`: local Identity configuration without real credentials.
- `.github/workflows/ci.yml`: PostgreSQL/Redis integration-test variables and ephemeral test secrets.
- `Makefile`: include Identity in v2 race tests and add an Identity integration target.
- `.env.v2.example`: documented non-secret environment variable template.

---

### Task 1: Make migrations module-owned and catalog-driven

**Files:**
- Create: `internal/platform/migrate/source.go`
- Modify: `internal/platform/migrate/migrate.go`
- Modify: `internal/platform/migrate/migrate_test.go`
- Modify: `internal/bootstrap/migrate.go`
- Modify: `internal/bootstrap/schema_test.go`

**Interfaces:**
- Produces: `migrate.Source`, `migrate.NewSource(name string, files fs.FS, dir string) Source`, `migrate.PlatformSource() Source`, `migrate.LatestVersion(sources ...Source) (int64, error)`.
- Changes: `migrate.Up(ctx context.Context, pool *pgxpool.Pool, sources ...Source) error`; no sources means `PlatformSource()` for backward compatibility.

- [ ] **Step 1: Write catalog validation tests**

Add tests using `fstest.MapFS` that require deterministic sorting and reject duplicate global versions:

```go
func TestLatestVersionRejectsDuplicateGlobalVersion(t *testing.T) {
	a := migrate.NewSource("a", fstest.MapFS{"001_a.up.sql": {Data: []byte("SELECT 1")}}, ".")
	b := migrate.NewSource("b", fstest.MapFS{"001_b.up.sql": {Data: []byte("SELECT 2")}}, ".")
	_, err := migrate.LatestVersion(a, b)
	if err == nil || !strings.Contains(err.Error(), "duplicate migration version 1") {
		t.Fatalf("error = %v, want duplicate version", err)
	}
}
```

- [ ] **Step 2: Run the focused test and observe the missing API**

Run: `go test ./internal/platform/migrate -run 'TestLatestVersionRejectsDuplicateGlobalVersion'`

Expected: FAIL because `NewSource` and `LatestVersion` do not exist.

- [ ] **Step 3: Add the source catalog and feed it to the existing transaction loop**

Use these public types and keep the current advisory lock and migration ledger behavior:

```go
type Source struct {
	name  string
	files fs.FS
	dir   string
}

func NewSource(name string, files fs.FS, dir string) Source {
	return Source{name: name, files: files, dir: dir}
}

func PlatformSource() Source {
	return NewSource("platform", migrationFiles, "sql")
}

func LatestVersion(sources ...Source) (int64, error) {
	migrations, err := loadSources(defaultSources(sources))
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}
	return migrations[len(migrations)-1].version, nil
}
```

`loadSources` must read every source directory, parse the existing global numeric prefix, attach the source name to errors, sort by version, and return `duplicate migration version N` before opening a database transaction. Change `Up` to call `loadSources(defaultSources(sources))` and retain `CurrentVersion` unchanged.

- [ ] **Step 4: Update Bootstrap to pass a catalog and derive readiness from it**

Add a package helper used by migrate, Gateway and Worker:

```go
func migrationSources() []migrate.Source {
	return []migrate.Source{migrate.PlatformSource()}
}

func requiredSchemaVersion() (int64, error) {
	return migrate.LatestVersion(migrationSources()...)
}
```

Replace the `requiredSchemaVersion` constant and call `migrate.Up(ctx, pool, migrationSources()...)` from `RunMigrate` and schema tests.

- [ ] **Step 5: Run migration and Bootstrap tests**

Run: `go test ./internal/platform/migrate ./internal/bootstrap`

Expected: PASS, with PostgreSQL tests skipped when `TEST_DATABASE_URL` is absent.

- [ ] **Step 6: Commit the migration catalog**

```bash
git add internal/platform/migrate internal/bootstrap/migrate.go internal/bootstrap/schema_test.go
git commit -m "refactor(migrate): register module migration sources"
```

### Task 2: Add the Identity PostgreSQL schema

**Files:**
- Create: `internal/identity/postgres/migrations/000003_identity.up.sql`
- Create: `internal/identity/postgres/migrations.go`
- Create: `internal/identity/postgres/migrations_test.go`
- Modify: `internal/bootstrap/migrate.go`
- Modify: `internal/bootstrap/bootstrap_test.go`

**Interfaces:**
- Consumes: `migrate.NewSource` from Task 1.
- Produces: `identitypostgres.Migrations() migrate.Source` and schema version 3.

- [ ] **Step 1: Write an integration test for tables and constraints**

The test must run the full Bootstrap migration catalog and assert all five tables exist, then reject an active user with `merged_into_user_id`:

```go
var tableCount int
err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name = ANY($1)`,
	[]string{"identity_users", "identity_identities", "identity_sessions", "identity_refresh_tokens", "identity_account_merges"}).Scan(&tableCount)
if err != nil || tableCount != 5 {
	t.Fatalf("identity table count=%d err=%v", tableCount, err)
}
```

- [ ] **Step 2: Run the migration test and observe missing tables**

Run: `TEST_DATABASE_URL=postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable go test ./internal/identity/postgres -run TestMigrationsCreateIdentitySchema`

Expected: FAIL because the Identity migration source and tables do not exist.

- [ ] **Step 3: Add the complete schema migration**

Use the following table and constraint structure:

```sql
CREATE TABLE identity_users (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('active', 'merged', 'disabled')),
    merged_into_user_id UUID REFERENCES identity_users(id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((status = 'merged') = (merged_into_user_id IS NOT NULL)),
    CHECK (merged_into_user_id IS NULL OR merged_into_user_id <> id)
);

CREATE TABLE identity_identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity_users(id),
    kind TEXT NOT NULL CHECK (kind IN ('email', 'wechat_mini')),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    union_id TEXT,
    verified_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (kind, issuer, subject)
);
CREATE INDEX identity_identities_user_id_idx ON identity_identities(user_id);

CREATE TABLE identity_sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity_users(id),
    client_kind TEXT NOT NULL CHECK (client_kind IN ('web', 'wechat_mini', 'app')),
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT
);
CREATE INDEX identity_sessions_active_user_idx ON identity_sessions(user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE identity_refresh_tokens (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES identity_sessions(id),
    token_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    replacement_token_id UUID REFERENCES identity_refresh_tokens(id),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX identity_refresh_tokens_session_idx ON identity_refresh_tokens(session_id);

CREATE TABLE identity_account_merges (
    id UUID PRIMARY KEY,
    primary_user_id UUID NOT NULL REFERENCES identity_users(id),
    secondary_user_id UUID NOT NULL UNIQUE REFERENCES identity_users(id),
    initiated_by_session_id UUID NOT NULL REFERENCES identity_sessions(id),
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (primary_user_id <> secondary_user_id)
);
```

- [ ] **Step 4: Embed and register the source**

```go
//go:embed migrations/*.up.sql
var files embed.FS

func Migrations() migrate.Source {
	return migrate.NewSource("identity", files, "migrations")
}
```

Append `identitypostgres.Migrations()` to `migrationSources()` and update the Bootstrap integration assertion from version 2 to version 3.

- [ ] **Step 5: Run migration integration tests twice for idempotence**

Run: `TEST_DATABASE_URL=postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable go test ./internal/platform/migrate ./internal/identity/postgres ./internal/bootstrap`

Expected: PASS and current schema version equals 3.

- [ ] **Step 6: Commit the Identity schema**

```bash
git add internal/identity/postgres internal/bootstrap
git commit -m "feat(identity): add PostgreSQL identity schema"
```

### Task 3: Add validated Identity runtime configuration

**Files:**
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Create: `internal/platform/config/identity.go`

**Interfaces:**
- Produces: `config.Identity`, `config.IdentityJWT`, `config.IdentityOTP`, `config.WeChat`, `config.SMTP`, and `config.WebSecurity` fields on `config.Config`, plus `Config.ValidateGateway() error`.

- [ ] **Step 1: Add table-driven validation tests**

Create a helper that sets a valid 32-byte Ed25519 seed and OTP Pepper, then test Gateway validation failures:

```go
func setValidIdentityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("IDENTITY_JWT_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	t.Setenv("IDENTITY_OTP_PEPPER_BASE64", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	t.Setenv("IDENTITY_WECHAT_APP_ID", "wx-test")
	t.Setenv("IDENTITY_WECHAT_APP_SECRET", "secret-test")
	t.Setenv("IDENTITY_EMAIL_DRIVER", "memory")
}
```

All environments must reject missing signing seed, missing OTP Pepper and missing WeChat credentials when `ValidateGateway` is called. Production Gateway cases must also reject `IDENTITY_COOKIE_SECURE=false`, a Cookie name without the `__Secure-` prefix, wildcard credential origins, and `IDENTITY_EMAIL_DRIVER=memory`. Worker and Migrate tests must prove they can load and run without Gateway-only credentials.

- [ ] **Step 2: Run config tests and observe missing fields**

Run: `go test ./internal/platform/config -run Identity`

Expected: FAIL because Identity configuration is not defined.

- [ ] **Step 3: Add exact configuration groups and defaults**

```go
type Identity struct {
	JWT          IdentityJWT
	OTP          IdentityOTP
	RefreshTTL   time.Duration
	ReuseGrace   time.Duration
	WeChat       WeChat
	SMTP         SMTP
	EmailDriver  string
	Web          WebSecurity
}

type IdentityJWT struct {
	PrivateKey ed25519.PrivateKey
	KeyID      string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
}

type IdentityOTP struct {
	Pepper       []byte
	TTL          time.Duration
	Attempts     int
	Cooldown     time.Duration
	EmailPerHour int
	IPPerHour    int
}

type WeChat struct { AppID, AppSecret, BaseURL string; Timeout time.Duration; IPPerHour int }
type SMTP struct { Host string; Port int; Username, Password, From, TLSMode string; Timeout time.Duration }
type WebSecurity struct { CookieName string; CookieSecure bool; AllowedOrigins, TrustedProxies []string }
```

Use defaults: key ID `identity-v1`, issuer `agri-price-crawler`, audience `agri-clients`, Access TTL `15m`, Refresh TTL `720h`, reuse grace `10s`, OTP TTL `10m`, attempts `5`, cooldown `60s`, email/hour `5`, IP/hour `30`, WeChat IP/hour `60`, WeChat base URL `https://api.weixin.qq.com`, WeChat timeout `5s`, SMTP timeout `5s`, development Cookie name `agri_refresh`, production Cookie name `__Secure-agri_refresh`, Cookie `SameSite=Lax`, and Web origins empty in development.

- [ ] **Step 4: Enforce environment-sensitive validation**

`Load` parses nonempty base64 values and durations but leaves absent Gateway-only credentials empty so Worker and Migrate do not need them. `ValidateGateway` requires exactly 32 seed bytes, at least 32 Pepper bytes, and nonempty WeChat AppID/AppSecret in every environment; no environment may silently generate keys. Permit `IDENTITY_EMAIL_DRIVER=memory` only outside production; require SMTP host, port, from, username and password for `smtp`; accept TLS modes `implicit`, `starttls`, and development-only `none`; reject `app` as a currently enabled client; require production Cookie Secure, the `__Secure-` name prefix and a nonempty exact origin allowlist. Call `ValidateGateway` before constructing Gateway dependencies, not from Worker or Migrate.

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/platform/config`

Expected: PASS.

- [ ] **Step 6: Commit configuration**

```bash
git add internal/platform/config
git commit -m "feat(identity): validate authentication configuration"
```

### Task 4: Define the Identity domain and adapter contracts

**Files:**
- Create: `internal/identity/model.go`
- Create: `internal/identity/errors.go`
- Create: `internal/identity/ports.go`
- Create: `internal/identity/service.go`
- Create: `internal/identity/service_test.go`
- Modify: `internal/platform/postgres/postgres.go`

**Interfaces:**
- Produces all core Identity types consumed by Tasks 5 through 14.

- [ ] **Step 1: Write tests for email normalization, client validation and dependency validation**

```go
func TestNormalizeEmail(t *testing.T) {
	got, err := identity.NormalizeEmail("  Farmer@Example.COM ")
	if err != nil || got != "farmer@example.com" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestClientKindValidateRejectsFutureApp(t *testing.T) {
	if err := identity.ClientApp.Validate(); !errors.Is(err, identity.ErrInvalidRequest) {
		t.Fatalf("error=%v", err)
	}
}
```

- [ ] **Step 2: Run the tests and observe the missing package**

Run: `go test ./internal/identity`

Expected: FAIL because the domain package does not exist.

- [ ] **Step 3: Define stable values and models**

```go
type ClientKind string
const (
	ClientWeb ClientKind = "web"
	ClientWeChatMini ClientKind = "wechat_mini"
	ClientApp ClientKind = "app"
)

type IdentityKind string
const (
	IdentityEmail IdentityKind = "email"
	IdentityWeChatMini IdentityKind = "wechat_mini"
)

type UserStatus string
const (
	UserActive UserStatus = "active"
	UserMerged UserStatus = "merged"
	UserDisabled UserStatus = "disabled"
)

type User struct { ID uuid.UUID; Status UserStatus; MergedInto *uuid.UUID; CreatedAt, UpdatedAt time.Time }
type ExternalIdentity struct { ID, UserID uuid.UUID; Kind IdentityKind; Issuer, Subject, UnionID string; VerifiedAt, CreatedAt time.Time }
type Session struct { ID, UserID uuid.UUID; Client ClientKind; CreatedAt, LastSeenAt, ExpiresAt time.Time; RevokedAt *time.Time }
type RefreshTokenRecord struct { ID, SessionID uuid.UUID; Hash [32]byte; CreatedAt, ExpiresAt time.Time; ConsumedAt, RevokedAt *time.Time; ReplacementID *uuid.UUID }
type Principal struct { UserID, SessionID uuid.UUID }
type LoginResult struct { User User; Client ClientKind; AccessToken, RefreshToken string; AccessExpiresAt, RefreshExpiresAt time.Time }
type IdentitySummary struct { Kind IdentityKind; Display string }
type AccountSummary struct { User User; Identities []IdentitySummary }
type BindResult struct { Account AccountSummary; Session *LoginResult }
type WeChatIdentity struct { AppID, OpenID, UnionID string }
type OTPChallenge struct { EmailKey, Digest, Purpose, Owner, IP string; TTL, Cooldown, Window time.Duration; MaxEmail, MaxIP, Attempts int }
type OTPAttempt struct { EmailKey, Digest, Purpose, Owner string }
type RateLimit struct { Key string; Max int; Window time.Duration }
```

`NormalizeEmail` must parse with `net/mail`, reject names and multiple addresses, trim spaces, lowercase the address, and cap it at 254 bytes. `ClientKind.Validate` accepts only `web` and `wechat_mini` in this release.

- [ ] **Step 4: Define exact ports**

```go
type Repository interface {
	WithinTx(context.Context, func(Tx) error) error
	UserSummary(context.Context, uuid.UUID) (User, []ExternalIdentity, error)
}

type Tx interface {
	SQL() platformpostgres.Tx
	FindIdentity(context.Context, IdentityKind, string, string, bool) (ExternalIdentity, error)
	FindUser(context.Context, uuid.UUID, bool) (User, error)
	ListIdentities(context.Context, uuid.UUID) ([]ExternalIdentity, error)
	InsertUser(context.Context, User) error
	InsertIdentity(context.Context, ExternalIdentity) error
	UpdateIdentityUnionID(context.Context, uuid.UUID, string) error
	ReassignIdentities(context.Context, uuid.UUID, uuid.UUID) error
	MarkUserMerged(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	InsertSession(context.Context, Session) error
	FindSession(context.Context, uuid.UUID, bool) (Session, error)
	ExtendSession(context.Context, uuid.UUID, time.Time, time.Time) error
	RevokeSession(context.Context, uuid.UUID, time.Time, string) error
	RevokeUserSessions(context.Context, uuid.UUID, time.Time, string) error
	InsertRefreshToken(context.Context, RefreshTokenRecord) error
	FindRefreshToken(context.Context, [32]byte, bool) (RefreshTokenRecord, error)
	ConsumeRefreshToken(context.Context, uuid.UUID, time.Time, uuid.UUID) error
	RecordMerge(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error
}

type OTPStore interface {
	Issue(context.Context, OTPChallenge) error
	Verify(context.Context, OTPAttempt) error
	DeleteIfMatch(context.Context, OTPChallenge) error
	Allow(context.Context, RateLimit) error
}
type EmailSender interface { SendCode(context.Context, string, string, time.Duration) error }
type WeChatExchanger interface { Exchange(context.Context, string) (WeChatIdentity, error) }
type MergeParticipant interface { Merge(context.Context, platformpostgres.Tx, uuid.UUID, uuid.UUID) error }
```

Quality-review contract addition: `ListIdentities` lets binding and merging
construct the complete account summary before the transaction commits, avoiding
a post-commit read that can fail after durable state has already changed.

Add the shared transaction contract:

```go
type Tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
```

Define `ErrNotFound`, `ErrConflict`, `ErrInvalidRequest`, `ErrRateLimited`, `ErrCodeInvalid`, `ErrCodeExpired`, `ErrAccountDisabled`, `ErrStateUnavailable`, `ErrUpstreamUnavailable`, `ErrTokenInvalid`, and `ErrTokenReused` using `errors.New`. `ErrStateUnavailable` is reserved for PostgreSQL/Redis state access; `ErrUpstreamUnavailable` is reserved for SMTP/WeChat providers.

- [ ] **Step 5: Add constructor dependency checks**

`NewService` accepts `Dependencies{Repository, OTPStore, EmailSender, WeChatExchanger, TokenManager, Clock, Random, MergeParticipants}` plus `Policy`. It must return an error naming each missing mandatory dependency and default `Clock` to a real UTC clock and `Random` to `crypto/rand.Reader` only when omitted. Lock these public use-case signatures:

```go
func (s *Service) RequestEmailLoginCode(ctx context.Context, email, sourceIP string) error
func (s *Service) LoginEmail(ctx context.Context, email, code string, client ClientKind) (LoginResult, error)
func (s *Service) LoginWeChat(ctx context.Context, code, sourceIP string, client ClientKind) (LoginResult, error)
func (s *Service) Refresh(ctx context.Context, rawRefreshToken string, client ClientKind) (LoginResult, error)
func (s *Service) Logout(ctx context.Context, principal Principal) error
func (s *Service) LogoutAll(ctx context.Context, principal Principal) error
func (s *Service) RequestBindEmailCode(ctx context.Context, principal Principal, email, sourceIP string) error
func (s *Service) BindEmail(ctx context.Context, principal Principal, email, code string) (BindResult, error)
func (s *Service) BindWeChat(ctx context.Context, principal Principal, code, sourceIP string) (BindResult, error)
func (s *Service) Me(ctx context.Context, principal Principal) (AccountSummary, error)
```

- [ ] **Step 6: Run focused tests and commit**

Run: `go test ./internal/identity`

Expected: PASS.

```bash
git add internal/identity internal/platform/postgres/postgres.go
git commit -m "feat(identity): define domain and adapter ports"
```

### Task 5: Implement Access and Refresh Token primitives

**Files:**
- Create: `internal/identity/token.go`
- Create: `internal/identity/token_test.go`

**Interfaces:**
- Consumes: `identity.Principal` from Task 4.
- Produces: `NewTokenManager`, `(*TokenManager).IssueAccess`, `(*TokenManager).ParseAccess`, `NewRefreshToken`, and `HashRefreshToken`.

- [ ] **Step 1: Write deterministic token tests**

Test signature validation, `kid`, issuer, audience, expiry, wrong algorithm, and opaque-token hashing:

```go
func TestTokenManagerRoundTrip(t *testing.T) {
	seed := bytes.Repeat([]byte{1}, ed25519.SeedSize)
	m, err := identity.NewTokenManager(identity.TokenConfig{
		PrivateKey: ed25519.NewKeyFromSeed(seed), KeyID: "k1", Issuer: "agri", Audience: "clients", AccessTTL: 15 * time.Minute,
	})
	if err != nil { t.Fatal(err) }
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	want := identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}
	raw, expires, err := m.IssueAccess(want, now)
	if err != nil || expires != now.Add(15*time.Minute) { t.Fatalf("expires=%v err=%v", expires, err) }
	got, err := m.ParseAccess(raw, now.Add(time.Minute))
	if err != nil || got != want { t.Fatalf("got=%+v err=%v", got, err) }
}
```

- [ ] **Step 2: Run the token tests and observe missing functions**

Run: `go test ./internal/identity -run 'Token|Refresh'`

Expected: FAIL because token primitives do not exist.

- [ ] **Step 3: Add strict Ed25519 JWT handling**

Use custom claims with `sid` and `jwt.RegisteredClaims`. Set Header `kid`, generate `jti` with `uuid.NewString`, parse only `jwt.SigningMethodEdDSA`, use `jwt.Parser{SkipClaimsValidation: true, ValidMethods: []string{"EdDSA"}}`, then validate `iss`, `aud`, `iat`, `exp`, `sub`, `sid` against the injected `now`. Return `ErrTokenInvalid` for every public validation failure.

```go
func (m *TokenManager) IssueAccess(p Principal, now time.Time) (string, time.Time, error)
func (m *TokenManager) ParseAccess(raw string, now time.Time) (Principal, error)
func NewRefreshToken(random io.Reader) (string, [32]byte, error)
func HashRefreshToken(raw string) [32]byte
```

`NewRefreshToken` must fill exactly 32 bytes with `io.ReadFull`, encode with `base64.RawURLEncoding`, and hash the encoded plaintext with SHA-256.

- [ ] **Step 4: Run token tests with the race detector**

Run: `go test -race ./internal/identity -run 'Token|Refresh'`

Expected: PASS without modifying the package-global `jwt.TimeFunc`.

- [ ] **Step 5: Commit token primitives**

```bash
git add internal/identity/token.go internal/identity/token_test.go
git commit -m "feat(identity): issue strict Ed25519 session tokens"
```

### Task 6: Implement the transactional PostgreSQL repository

**Files:**
- Create: `internal/identity/postgres/repository.go`
- Create: `internal/identity/postgres/repository_test.go`

**Interfaces:**
- Consumes: `identity.Repository`, `identity.Tx`, models and sentinel errors from Task 4.
- Produces: `identitypostgres.NewRepository(pool *pgxpool.Pool) identity.Repository`.

- [ ] **Step 1: Write repository lifecycle and constraint tests**

Use real PostgreSQL. Cover user/identity insertion, `FindIdentity(..., true)`, session/token insertion, token consumption, session extension, user-session revocation, identity reassignment, merge recording, summary masking inputs, and unique identity conflict. Reset tables with:

```sql
TRUNCATE identity_account_merges, identity_refresh_tokens, identity_sessions, identity_identities, identity_users CASCADE;
```

- [ ] **Step 2: Run the repository tests and observe the missing constructor**

Run: `TEST_DATABASE_URL=postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable go test ./internal/identity/postgres -run Repository`

Expected: FAIL because `NewRepository` does not exist.

- [ ] **Step 3: Implement transaction and row mapping**

`WithinTx` must begin a `pgx.Tx`, roll back on every returned error or panic, and commit only on success. Implement `FOR UPDATE` only when the boolean flag is true. Map `pgx.ErrNoRows` to `identity.ErrNotFound`, PostgreSQL unique violation `23505` to `identity.ErrConflict`, and connection-class failures, closed pools and database timeouts to wrapped `identity.ErrStateUnavailable`; wrap all other errors with the operation name.

Use explicit column lists. `UpdateIdentityUnionID` may only replace an empty UnionID with a nonempty provider value. `MarkUserMerged` updates only an active secondary account. `ConsumeRefreshToken` must update only rows where `consumed_at IS NULL AND revoked_at IS NULL`; `RevokeSession` and `RevokeUserSessions` must also set `identity_refresh_tokens.revoked_at` for every affected session in the same transaction.

- [ ] **Step 4: Assert affected-row fences**

For `UpdateIdentityUnionID`, `MarkUserMerged`, `ExtendSession`, `ConsumeRefreshToken`, `RevokeSession` and `RecordMerge`, require exactly one affected row where applicable. A stale or already-consumed update returns `identity.ErrConflict`, never silent success. The Logout use case may translate an already-revoked-session conflict into idempotent success.

- [ ] **Step 5: Run repository and package race tests**

Run: `TEST_DATABASE_URL=postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable go test -race ./internal/identity/...`

Expected: PASS.

- [ ] **Step 6: Commit the repository**

```bash
git add internal/identity/postgres
git commit -m "feat(identity): persist accounts and sessions transactionally"
```

### Task 7: Implement atomic Redis OTP and rate limiting

**Files:**
- Create: `internal/identity/redisotp/store.go`
- Create: `internal/identity/redisotp/store_test.go`

**Interfaces:**
- Consumes: `identity.OTPStore`, `OTPChallenge`, `OTPAttempt` and OTP sentinel errors.
- Produces: `redisotp.New(client redis.UniversalClient, prefix string) identity.OTPStore`.

- [ ] **Step 1: Write Redis integration tests**

Cover issue, 60-second cooldown, email/hour and IP/hour limits, wrong-code decrement, fifth-attempt deletion, successful one-time consumption, expiry, purpose isolation, generic `Allow` limits and `DeleteIfMatch` not deleting a newer challenge. Use a unique prefix per test and clean it with `SCAN` plus `DEL`.

- [ ] **Step 2: Run tests and observe the missing store**

Run: `TEST_REDIS_ADDR=localhost:6379 go test ./internal/identity/redisotp`

Expected: FAIL because the Redis store does not exist.

- [ ] **Step 3: Add the atomic issue script**

The Lua script receives challenge, cooldown, email-rate and IP-rate keys. It must check both limits and cooldown before mutation, increment both rate counters with one-hour expiry, set `digest`, `attempts`, `purpose` and `owner` in the challenge hash, apply the 10-minute TTL, and set the 60-second cooldown. Map script result `cooldown` and `rate` to `identity.ErrRateLimited`; map Redis connection, timeout and protocol failures to a wrapped `identity.ErrStateUnavailable` without Redis addresses or key contents in the public error.

- [ ] **Step 4: Add atomic verify and conditional-delete scripts**

Verify returns `expired` when the challenge key is absent, compares digest, purpose and owner, deletes on success, decrements attempts on failure, and deletes after the fifth failure. `DeleteIfMatch` deletes challenge and cooldown only when the stored digest equals the failed SMTP request's digest. Never delete hourly rate counters on delivery failure.

`Allow` uses one atomic increment-and-expire script. It applies `RateLimit.Window` on the first increment and returns `identity.ErrRateLimited` without incrementing when the current value is already at `RateLimit.Max`.

- [ ] **Step 5: Run Redis tests with race detection**

Run: `TEST_REDIS_ADDR=localhost:6379 go test -race ./internal/identity/redisotp`

Expected: PASS.

- [ ] **Step 6: Commit the Redis adapter**

```bash
git add internal/identity/redisotp
git commit -m "feat(identity): store OTP challenges atomically in Redis"
```

### Task 8: Implement the SMTP verification-code sender

**Files:**
- Create: `internal/identity/smtp/sender.go`
- Create: `internal/identity/smtp/sender_test.go`

**Interfaces:**
- Consumes: `identity.EmailSender` and `config.SMTP`.
- Produces: `smtp.New(config config.SMTP) (*Sender, error)` and `smtp.NewMemory() *MemorySender`.

- [ ] **Step 1: Write tests with an injected SMTP client**

Assert envelope sender, one recipient, UTF-8 subject, plain-text body containing the code and expiry minutes, CRLF header safety, timeout propagation, and that error strings do not contain the recipient or code. Test the memory sender through `Codes(email) []string` without logging.

- [ ] **Step 2: Run tests and observe the missing adapter**

Run: `go test ./internal/identity/smtp`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Build a timeout-bounded standard-library SMTP client**

Use `net.Dialer{Timeout: cfg.Timeout}` and set a connection deadline for the complete exchange. Support explicit TLS for port 465 and STARTTLS for configured non-implicit TLS. Call `smtp.NewClient`, `Hello`, `StartTLS`, `Auth`, `Mail`, `Rcpt`, `Data`, `Quit` in order. Reject `\r` or `\n` in configured From, recipient and subject before dialing.

- [ ] **Step 4: Use fixed safe message copy**

Use subject `农产品价格助手登录验证码` and text `您的验证码是 %s，有效期 %d 分钟。请勿向任何人泄露此验证码。`. Encode headers as UTF-8 and never return an error containing message content, code or recipient.

- [ ] **Step 5: Run tests and commit**

Run: `go test -race ./internal/identity/smtp`

Expected: PASS.

```bash
git add internal/identity/smtp
git commit -m "feat(identity): send verification codes over SMTP"
```

### Task 9: Implement the WeChat code exchanger

**Files:**
- Create: `internal/identity/wechat/client.go`
- Create: `internal/identity/wechat/client_test.go`

**Interfaces:**
- Consumes: `identity.WeChatExchanger`, `identity.WeChatIdentity`, `config.WeChat`.
- Produces: `wechat.New(config config.WeChat) (*Client, error)`.

- [ ] **Step 1: Write `httptest` contract tests**

Verify the exact `/sns/jscode2session` request parameters, successful OpenID/UnionID parsing, missing OpenID rejection, WeChat `errcode` mapping, HTTP 5xx mapping, invalid JSON, body-size limit and caller cancellation. Assert returned errors never contain AppSecret, temporary code, OpenID or session key.

- [ ] **Step 2: Run tests and observe the missing client**

Run: `go test ./internal/identity/wechat`

Expected: FAIL because the adapter does not exist.

- [ ] **Step 3: Implement the constrained HTTP client**

Construct a client with configured timeout and base URL. Send `appid`, `secret`, `js_code` and `grant_type=authorization_code` as query parameters. Read at most 64 KiB, close the body, and require a nonempty `openid`.

Map invalid or consumed WeChat codes to `identity.ErrCodeInvalid`; map network failures, HTTP 429, HTTP 5xx and provider throttling to `identity.ErrUpstreamUnavailable`; map malformed successful responses to a wrapped upstream error. Return only `WeChatIdentity{AppID, OpenID, UnionID}` and discard `session_key` immediately.

- [ ] **Step 4: Run adapter tests and commit**

Run: `go test -race ./internal/identity/wechat`

Expected: PASS.

```bash
git add internal/identity/wechat
git commit -m "feat(identity): exchange mini-program login codes"
```

### Task 10: Implement session refresh and logout use cases

**Files:**
- Create: `internal/identity/session.go`
- Create: `internal/identity/session_test.go`
- Modify: `internal/identity/service.go`

**Interfaces:**
- Consumes: token primitives and repository contracts.
- Produces: `Service.Refresh`, `Service.Logout`, `Service.LogoutAll`, `Service.Me`, and the shared private `createSession` helper.

- [ ] **Step 1: Write fake-repository use-case tests**

Cover valid rotation, client-kind mismatch without token consumption, expired token, revoked session, immediate duplicate within grace, reuse outside grace with committed session revocation, logout-current isolation, idempotent repeated logout, logout-all, disabled account, and summary lookup. Use a fixed clock and deterministic random reader.

- [ ] **Step 2: Run session tests and observe missing methods**

Run: `go test ./internal/identity -run 'Refresh|Logout|Me'`

Expected: FAIL because session use cases do not exist.

- [ ] **Step 3: Add shared session creation**

`createSession` must generate a session UUID and refresh-record UUID, generate one 32-byte Refresh Token, insert the session and hashed token in the caller's transaction, and issue the Access Token only after durable inserts succeed. Set `LoginResult.Client`, session expiry and refresh expiry, using `now + RefreshTTL` for both expiries.

- [ ] **Step 4: Add atomic refresh rotation**

Validate the requested client and hash the presented token. Inside the
transaction, first perform an unlocked token lookup only to discover its
`SessionID`; never lock a refresh-token row before its session. Lock the
session row, then re-read the token with `FOR UPDATE` and verify that it still
belongs to the locked session before validating or mutating either row. This
session-then-token order must match logout and logout-all; repository bulk
revocation locks session rows in UUID order and then token rows in UUID order.
Require `session.Client == client`, reject expired/revoked state, and handle
consumed tokens as follows: within `ReuseGrace` return `ErrTokenInvalid`
without mutation; outside the grace revoke the session with reason
`refresh_token_reused`, set a post-commit result to `ErrTokenReused`, return
nil from the transaction callback so revocation commits, then return the
post-commit error. For a fresh token, insert replacement, consume old token
with `replacement_token_id`, extend session, and return newly issued
credentials with `LoginResult.Client` copied from the durable session.

- [ ] **Step 5: Add logout and current-user behavior**

`Logout` revokes only `Principal.SessionID` with reason `logout` and treats an already-revoked session as idempotent success. `LogoutAll` first locks and validates the principal session, then revokes all user sessions with reason `logout_all`. `Me` reads the user summary and converts merged or disabled users to stable public errors. Mask email identities as the first Unicode character plus `***@domain`, use `***@domain` for an empty local part after validation, and expose WeChat only as `已绑定微信`; never copy OpenID or UnionID into `AccountSummary`.

- [ ] **Step 6: Run tests and commit**

Run: `go test -race ./internal/identity -run 'Refresh|Logout|Me|CreateSession'`

Expected: PASS.

```bash
git add internal/identity/session.go internal/identity/session_test.go internal/identity/service.go
git commit -m "feat(identity): rotate and revoke device sessions"
```

### Task 11: Implement email and WeChat login use cases

**Files:**
- Create: `internal/identity/login.go`
- Create: `internal/identity/login_test.go`

**Interfaces:**
- Consumes: OTP, SMTP, WeChat, repository and shared session creation.
- Produces: `Service.RequestEmailLoginCode`, `Service.LoginEmail`, and `Service.LoginWeChat`.

- [ ] **Step 1: Write login workflow tests**

Cover email normalization, HMAC digest generation, SMTP failure cleanup with no database writes, wrong/expired OTP, first-login account creation, repeat login, concurrent unique-identity conflict retry, WeChat source-IP limiting, WeChat upstream failure with no database writes, WeChat first/repeat login, UnionID update without UnionID-based account merging, disabled account, and rejection of `ClientApp`.

- [ ] **Step 2: Run login tests and observe missing methods**

Run: `go test ./internal/identity -run 'EmailLogin|WeChatLogin'`

Expected: FAIL because login use cases do not exist.

- [ ] **Step 3: Implement email code issuance and verification**

Generate a zero-padded six-digit code with rejection sampling from the injected secure random reader. Compute `HMAC-SHA256(OTPPepper, code)`, use `SHA-256(normalizedEmail)` as the Redis email key, normalize the parsed source address with `netip.Addr.Unmap`, and use its SHA-256 digest as the IP key. Issue Redis state before calling SMTP; on SMTP failure call `DeleteIfMatch` and return a wrapped upstream error without including the address or code. Login verifies and consumes the `login` challenge before opening the PostgreSQL transaction.

- [ ] **Step 4: Resolve or create an identity transactionally**

For both email and WeChat, find `(kind, issuer, subject)`. If found, require its user to be active and create a new session. If absent, insert a new active user and verified identity, then create the session. On `ErrConflict` from the unique identity insert, retry the complete transaction once and resolve the now-existing identity; a second conflict returns `ErrConflict`.

- [ ] **Step 5: Exchange WeChat codes outside the database transaction**

Before contacting WeChat, hash the normalized source IP and call `OTPStore.Allow` with the configured WeChat hourly policy. Call the WeChat adapter before opening PostgreSQL. Store AppID as issuer, OpenID as subject, and returned UnionID. Never accept issuer or subject from the client request.

- [ ] **Step 6: Run tests and commit**

Run: `go test -race ./internal/identity -run 'EmailLogin|WeChatLogin|RequestEmail'`

Expected: PASS.

```bash
git add internal/identity/login.go internal/identity/login_test.go
git commit -m "feat(identity): login with email and WeChat proofs"
```

### Task 12: Implement binding and atomic account merging

**Files:**
- Create: `internal/identity/bind.go`
- Create: `internal/identity/bind_test.go`

**Interfaces:**
- Consumes: `MergeParticipant`, active-session validation, OTP/WeChat proof and session creation.
- Produces: `Service.RequestBindEmailCode`, `Service.BindEmail`, and `Service.BindWeChat`.

- [ ] **Step 1: Write binding and merge tests**

Cover inactive current session, unowned identity binding, idempotent same-account binding, older-account selection, UUID tie-break, participant call order, participant rollback, identity reassignment, audit insertion, both-account session revocation, new-session creation, WeChat source-IP limiting, disabled target rejection and simultaneous cross-merge locking order.

- [ ] **Step 2: Run binding tests and observe missing methods**

Run: `go test ./internal/identity -run 'Bind|Merge'`

Expected: FAIL because binding methods do not exist.

- [ ] **Step 3: Require dual proof**

`RequestBindEmailCode` locks or reads the principal session and rejects revoked, expired or wrong-user sessions before issuing an OTP with purpose `bind` and owner equal to `Principal.UserID.String()`. `BindEmail` consumes that exact challenge. `BindWeChat` applies the configured hashed source-IP limit, exchanges a temporary code and derives AppID/OpenID server-side.

- [ ] **Step 4: Add deterministic atomic merge**

If the identity is unowned, attach it to the current user and return `BindResult{Account: summary}`. If it belongs to the current user, return the same shape without creating a new session. Otherwise load the principal's active session to retain its `Client` value, lock both users in UUID order, then choose primary by `created_at` and UUID tie-break. Call merge participants in registration order with `tx.SQL()`, reassign identities, call `MarkUserMerged`, record the merge, revoke every old session for both users, and create one new session for the retained current client. Before the transaction callback returns, call `tx.ListIdentities` for the primary user and construct the complete `AccountSummary`; do not defer this to a post-commit `Repository.UserSummary` read that could fail after the merge is durable. A successful merge returns the new credentials in `BindResult.Session`; non-merge binding leaves `Session` nil.

- [ ] **Step 5: Protect failure behavior**

The service must return no credentials until the transaction commits. Any participant or database failure leaves users, identities and sessions unchanged. Never merge a disabled account or follow a client-provided primary account preference.

- [ ] **Step 6: Run tests and commit**

Run: `go test -race ./internal/identity -run 'Bind|Merge'`

Expected: PASS.

```bash
git add internal/identity/bind.go internal/identity/bind_test.go
git commit -m "feat(identity): bind identities and merge accounts atomically"
```

### Task 13: Add Problem Details, request tracing and CORS primitives

**Files:**
- Create: `internal/platform/httpx/problem.go`
- Create: `internal/platform/httpx/problem_test.go`
- Create: `internal/platform/httpx/requestid.go`
- Create: `internal/platform/httpx/requestid_test.go`
- Create: `internal/platform/httpx/cors.go`
- Create: `internal/platform/httpx/cors_test.go`
- Create: `internal/platform/httpx/clientip.go`
- Create: `internal/platform/httpx/clientip_test.go`

**Interfaces:**
- Produces: `httpx.Problem`, `httpx.WriteProblem`, `httpx.RequestID`, `httpx.WithRequestID`, `httpx.CORS`, `httpx.RequireAllowedOrigin`, `httpx.ParseTrustedProxies`, and `httpx.ClientIP`.

- [ ] **Step 1: Write transport primitive tests**

Require `application/problem+json`, stable code and Trace ID fields; preserve a valid inbound `X-Request-ID` capped at 128 bytes; generate a UUID when absent; allow exact configured origins with credentials; reject wildcard credentials; reject missing or foreign Origin on Cookie-authenticated writes. Verify `ClientIP` ignores forwarding headers from untrusted peers and walks `X-Forwarded-For` right-to-left to return the first untrusted address behind a trusted proxy chain.

- [ ] **Step 2: Run tests and observe missing package**

Run: `go test ./internal/platform/httpx`

Expected: FAIL because `httpx` does not exist.

- [ ] **Step 3: Implement the exact Problem model**

```go
type Problem struct {
	Type string `json:"type"`
	Title string `json:"title"`
	Status int `json:"status"`
	Code string `json:"code"`
	TraceID string `json:"trace_id"`
	Detail string `json:"detail,omitempty"`
}
```

`WriteProblem` sets Content-Type before status, JSON-encodes once, and never includes a wrapped internal error. Request ID middleware stores the value in an unexported context key and returns it in the response header.

- [ ] **Step 4: Implement exact-origin CORS and Origin checking**

Normalize configured origins once, compare scheme/host/port exactly, emit `Vary: Origin`, and allow credentials only for a match. Preflight permits `GET, POST, OPTIONS` and headers `Authorization, Content-Type, X-Request-ID`. `RequireAllowedOrigin` returns `403 origin_not_allowed` for Cookie-authenticated state-changing requests without a configured matching Origin.

Parse trusted proxy entries as exact IPs or CIDR prefixes during construction. `ClientIP` must parse `RemoteAddr` with `net.SplitHostPort`, reject malformed forwarding entries, and never return an unvalidated header string.

```go
func ParseTrustedProxies(values []string) ([]netip.Prefix, error)
func ClientIP(r *http.Request, trusted []netip.Prefix) (netip.Addr, error)
func CORS(allowedOrigins []string, next http.Handler) (http.Handler, error)
func RequireAllowedOrigin(r *http.Request, allowedOrigins []string) error
```

- [ ] **Step 5: Run tests and commit**

Run: `go test -race ./internal/platform/httpx`

Expected: PASS.

```bash
git add internal/platform/httpx
git commit -m "feat(platform): standardize API errors and request security"
```

### Task 14: Add Identity HTTP routes and authentication middleware

**Files:**
- Create: `internal/identity/httpapi/handler.go`
- Create: `internal/identity/httpapi/handler_test.go`
- Create: `internal/identity/httpapi/auth.go`
- Create: `internal/identity/httpapi/auth_test.go`
- Create: `internal/identity/httpapi/cookie.go`
- Modify: `internal/gateway/server.go`
- Modify: `internal/gateway/server_test.go`

**Interfaces:**
- Consumes: all public `identity.Service` methods and `httpx` primitives.
- Produces: `httpapi.New(Config) http.Handler`; changes `gateway.Config` to accept `API http.Handler` and `Middleware func(http.Handler) http.Handler`.

The HTTP package defines this narrow fakeable interface and a separate Token parser:

```go
type Service interface {
	RequestEmailLoginCode(context.Context, string, string) error
	LoginEmail(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	LoginWeChat(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	Refresh(context.Context, string, identity.ClientKind) (identity.LoginResult, error)
	Logout(context.Context, identity.Principal) error
	LogoutAll(context.Context, identity.Principal) error
	RequestBindEmailCode(context.Context, identity.Principal, string, string) error
	BindEmail(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	BindWeChat(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	Me(context.Context, identity.Principal) (identity.AccountSummary, error)
}
type TokenParser interface { ParseAccess(string, time.Time) (identity.Principal, error) }
```

`Config` contains `Service`, `TokenParser`, Cookie policy, trusted proxies, clock and logger; handlers never read process environment directly.

- [ ] **Step 1: Write HTTP contract tests with a fake service**

Test all ten routes, malformed JSON, 1 MiB body limit, unknown fields, content type, Bearer parsing, principal context, status mapping, safe Chinese details, Trace ID, Web Cookie flags, mini-program JSON Refresh Token, refresh source ambiguity, Cookie Origin rejection, and logout Cookie clearing.

- [ ] **Step 2: Run HTTP tests and observe missing handlers**

Run: `go test ./internal/identity/httpapi ./internal/gateway`

Expected: FAIL because the Identity handler and Gateway injection point do not exist.

- [ ] **Step 3: Register exact routes on a private `http.ServeMux`**

```go
mux.HandleFunc("POST /api/v1/auth/email/code", h.requestEmailCode)
mux.HandleFunc("POST /api/v1/auth/email/login", h.emailLogin)
mux.HandleFunc("POST /api/v1/auth/wechat/login", h.wechatLogin)
mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
mux.Handle("POST /api/v1/auth/logout", h.authenticate(http.HandlerFunc(h.logout)))
mux.Handle("POST /api/v1/auth/logout-all", h.authenticate(http.HandlerFunc(h.logoutAll)))
mux.Handle("POST /api/v1/auth/bind/email/code", h.authenticate(http.HandlerFunc(h.requestBindEmailCode)))
mux.Handle("POST /api/v1/auth/bind/email", h.authenticate(http.HandlerFunc(h.bindEmail)))
mux.Handle("POST /api/v1/auth/bind/wechat", h.authenticate(http.HandlerFunc(h.bindWeChat)))
mux.Handle("GET /api/v1/me", h.authenticate(http.HandlerFunc(h.me)))
```

Decode exactly one JSON object with `DisallowUnknownFields`, enforce the body limit, and reject trailing JSON. Resolve source IP through `httpx.ClientIP`. Use this public mapping and log only internal category and Trace ID:

| Error | HTTP status | Public code |
|---|---:|---|
| malformed input or `ErrInvalidRequest` | 400 | `invalid_request` |
| email `ErrCodeInvalid` | 400 | `invalid_email_code` |
| email `ErrCodeExpired` | 400 | `email_code_expired` |
| WeChat `ErrCodeInvalid` | 400 | `invalid_wechat_code` |
| access `ErrTokenInvalid` | 401 | `invalid_access_token` |
| refresh `ErrTokenInvalid` | 401 | `invalid_refresh_token` |
| `ErrTokenReused` | 401 | `refresh_token_reused` |
| Origin rejected | 403 | `origin_not_allowed` |
| `ErrAccountDisabled` | 403 | `account_disabled` |
| `ErrConflict` | 409 | `identity_conflict` |
| `ErrRateLimited` | 429 | `rate_limited` |
| `ErrStateUnavailable` | 503 | `service_not_ready` |
| SMTP `ErrUpstreamUnavailable` | 503 | `email_delivery_unavailable` |
| WeChat `ErrUpstreamUnavailable` | 503 | `wechat_unavailable` |
| unclassified error | 500 | `internal_error` |

- [ ] **Step 4: Enforce client-specific Refresh Token transport**

For `client_kind=web`, set the configured Cookie and omit `refresh_token` from JSON. For `wechat_mini`, include it in JSON and never set a Cookie. Refresh accepts the Cookie for Web or a JSON token for mini-program, rejects both or neither, passes the inferred client kind to `Service.Refresh`, and returns only when `LoginResult.Client` matches that kind. A merge `BindResult.Session` uses the same client-specific transport; a non-merge result returns only the account. Logout clears the Web Cookie with Max-Age `-1`.

- [ ] **Step 5: Inject the API handler without changing health routes**

Gateway registers `/livez` and `/readyz` directly and mounts `Config.API` at `/api/v1/`. Wrap the resulting mux in request-ID and CORS middleware supplied by Bootstrap. Existing successful health response bodies remain byte-for-byte compatible; readiness failures move to `httpx.Problem` so they include the new Trace ID while retaining status `503` and code `service_not_ready`.

- [ ] **Step 6: Run HTTP tests and commit**

Run: `go test -race ./internal/identity/httpapi ./internal/gateway`

Expected: PASS.

```bash
git add internal/identity/httpapi internal/gateway
git commit -m "feat(gateway): expose Identity REST endpoints"
```

### Task 15: Expand the OpenAPI contract

**Files:**
- Modify: `api/openapi.yaml`
- Create: `internal/gateway/openapi_test.go`

**Interfaces:**
- Consumes: the exact paths and DTOs from Task 14.
- Produces: OpenAPI schemas for all Identity requests, responses, errors, Bearer auth and Web Cookie auth.

- [ ] **Step 1: Add a failing route-coverage test**

Parse `api/openapi.yaml` with `yaml.v3` and assert the ten Identity paths and existing health paths exist. Assert every protected operation declares `bearerAuth`, refresh declares `refreshCookie` or request-body token support, and all documented error responses reference `Problem`.

- [ ] **Step 2: Run the coverage test**

Run: `go test ./internal/gateway -run OpenAPI`

Expected: FAIL listing the first missing Identity path.

- [ ] **Step 3: Add exact security schemes and reusable schemas**

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
    refreshCookie:
      type: apiKey
      in: cookie
      name: __Secure-agri_refresh
```

Add schemas `Problem`, `UserSummary`, `BoundIdentitySummary`, `LoginResponse`, `BindResponse`, `EmailCodeRequest`, `EmailLoginRequest`, `WeChatLoginRequest`, `RefreshRequest`, `BindEmailRequest`, and `BindWeChatRequest`. `BindResponse` contains the account and optional session credentials only when a merge replaces the current session. Constrain `client_kind` to `web` and `wechat_mini`, email to format `email`, OTP to pattern `^[0-9]{6}$`, and WeChat code to 1 through 256 characters.

- [ ] **Step 4: Document every response and privacy rule**

Document `202` for both code-send endpoints, `200` for login/refresh/bind/logout/me, `400`, `401`, `403`, `409`, `429`, and `503` where reachable. State that Web responses omit `refresh_token`, mini-program responses include it, and `/me` returns masked identities only.

- [ ] **Step 5: Run the OpenAPI test and commit**

Run: `go test ./internal/gateway -run OpenAPI`

Expected: PASS.

```bash
git add api/openapi.yaml internal/gateway/openapi_test.go
git commit -m "docs(api): define Identity REST contract"
```

### Task 16: Assemble Identity in Bootstrap and verify the complete phase

**Files:**
- Modify: `internal/bootstrap/gateway.go`
- Modify: `internal/bootstrap/bootstrap_test.go`
- Modify: `internal/bootstrap/schema_test.go`
- Modify: `docker-compose.v2.yaml`
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`
- Create: `.env.v2.example`
- Create: `internal/bootstrap/identity_integration_test.go`

**Interfaces:**
- Consumes: every adapter and handler constructor from Tasks 3 through 15.
- Produces: a runnable Gateway with Identity endpoints and reproducible local/CI verification.

- [ ] **Step 1: Write a Bootstrap integration test**

With real PostgreSQL and Redis, memory email, fake WeChat HTTP server and deterministic test keys, call an unexported `buildGatewayHandler` assembly helper and run its result through `httptest.Server`. Request an email code, read it from the memory sender, login, call `/api/v1/me`, and refresh. Close the Redis client, assert a new email-code request returns `503`, then assert `/api/v1/me` and PostgreSQL-backed refresh still succeed. Logout and assert the rotated token cannot refresh again.

- [ ] **Step 2: Run the integration test and observe missing assembly**

Run: `TEST_DATABASE_URL=postgres://agri:agri_dev@localhost:5432/agri?sslmode=disable TEST_REDIS_ADDR=localhost:6379 go test ./internal/bootstrap -run IdentityIntegration`

Expected: FAIL because Bootstrap does not construct Identity.

- [ ] **Step 3: Assemble dependencies explicitly**

Define `gatewayOverrides struct { Email identity.EmailSender; WeChat identity.WeChatExchanger; Clock identity.Clock; Random io.Reader }`. Extract `buildGatewayHandler(pool, redisClient, cfg, logger, overrides) (http.Handler, error)` so integration tests can inject memory email and a fake WeChat exchanger without opening a listener. `RunGateway` passes empty overrides and calls `cfg.ValidateGateway` before constructing Identity. Then create `identitypostgres.NewRepository(pool)`, `redisotp.New(redisClient, "agri:v2:identity")`, the configured memory or SMTP sender, WeChat client, Token Manager and Identity Service. Construct `httpapi.New`, wrap Gateway with request-ID and exact-origin CORS, and inject the API handler. Update the existing PostgreSQL-failure Bootstrap test to supply a valid Gateway Identity config; Worker and Migrate cases remain free of Identity credentials. Return constructor errors with component names and never log config structs containing secrets.

- [ ] **Step 4: Make local and CI configuration reproducible**

Add `TEST_REDIS_ADDR=localhost:6379` to the backend-v2 CI environment. Generate a 32-byte JWT seed and OTP Pepper inside the CI step and export only for that job. Compose passes `${IDENTITY_JWT_PRIVATE_KEY_BASE64:-}` and `${IDENTITY_OTP_PEPPER_BASE64:-}` so `docker compose up postgres redis` still works without Gateway secrets; starting Gateway with empty values must fail configuration validation. The `v2-smoke` target generates ephemeral values before starting Gateway. Use memory email in development, set non-secret fake WeChat values, and allow `http://localhost:3000`. Document every variable in `.env.v2.example` with generation commands, not usable secret values.

- [ ] **Step 5: Expand v2 verification targets**

Change `v2-test` to include `./internal/identity/...` and `./internal/platform/httpx/...`. Add `v2-identity-integration` that starts PostgreSQL/Redis, migrates, runs Identity integration tests with both test URLs, and always stops services without deleting the developer's named volume unless explicitly requested.

- [ ] **Step 6: Run focused integration verification**

Run:

```bash
docker compose -f docker-compose.v2.yaml up -d --wait postgres redis
GOTOOLCHAIN=go1.24.3 go test -count=1 -race ./internal/identity/... ./internal/platform/httpx/... ./internal/gateway/... ./internal/bootstrap/...
```

Expected: PASS with PostgreSQL and Redis integration tests enabled.

- [ ] **Step 7: Run repository and build verification**

Run:

```bash
GOTOOLCHAIN=go1.24.3 go test ./...
GOTOOLCHAIN=go1.24.3 make v2-build
docker build -f Dockerfile.v2 -t agri-price-crawler-v2:identity .
```

Expected: all tests pass, all three v2 binaries build, and the Docker image builds successfully.

- [ ] **Step 8: Stop temporary services and inspect the diff**

Run: `docker compose -f docker-compose.v2.yaml stop postgres redis`

Expected: containers stop without deleting the named PostgreSQL volume.

Run: `git status --short && git diff --check`

Expected: only intended Identity-phase files are modified and `git diff --check` prints no errors.

- [ ] **Step 9: Commit the assembled phase**

```bash
git add internal/bootstrap docker-compose.v2.yaml .github/workflows/ci.yml Makefile .env.v2.example
git commit -m "feat(identity): assemble authentication in Gateway"
```

- [ ] **Step 10: Request final code review**

Invoke `superpowers:requesting-code-review` against the complete Identity commit range. Resolve every correctness or security issue, rerun Step 6 and Step 7, and use `superpowers:verification-before-completion` before claiming the phase is complete.
