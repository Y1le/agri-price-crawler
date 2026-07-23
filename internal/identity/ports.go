package identity

import (
	"context"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

// Repository owns durable Identity state.
type Repository interface {
	WithinTx(context.Context, func(Tx) error) error
	UserSummary(context.Context, uuid.UUID) (User, []ExternalIdentity, error)
}

// Tx is the complete durable transaction contract used by Identity use cases.
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

// OTPStore atomically issues, consumes and rate-limits short-lived proof state.
type OTPStore interface {
	Issue(context.Context, OTPChallenge) error
	Verify(context.Context, OTPAttempt) error
	DeleteIfMatch(context.Context, OTPChallenge) error
	Allow(context.Context, RateLimit) error
}

// EmailSender delivers a single verification code.
type EmailSender interface {
	SendCode(context.Context, string, string, time.Duration) error
}

// WeChatExchanger exchanges a temporary mini-program code for provider-owned
// identity data.
type WeChatExchanger interface {
	Exchange(context.Context, string) (WeChatIdentity, error)
}

// MergeParticipant atomically moves another module's user-owned data.
type MergeParticipant interface {
	Merge(context.Context, platformpostgres.Tx, uuid.UUID, uuid.UUID) error
}

// AccessTokenIssuer is the minimal token capability required by application
// use cases. The concrete TokenManager is implemented with token primitives.
type AccessTokenIssuer interface {
	IssueAccess(Principal, time.Time) (string, time.Time, error)
}

// Clock makes time-dependent use cases deterministic in tests.
type Clock interface {
	Now() time.Time
}

// Application is the complete public Identity use-case surface. Service gains
// these methods incrementally as their workflows are implemented.
type Application interface {
	RequestEmailLoginCode(context.Context, string, string) error
	LoginEmail(context.Context, string, string, ClientKind) (LoginResult, error)
	LoginWeChat(context.Context, string, string, ClientKind) (LoginResult, error)
	Refresh(context.Context, string, ClientKind) (LoginResult, error)
	Logout(context.Context, Principal) error
	LogoutAll(context.Context, Principal) error
	RequestBindEmailCode(context.Context, Principal, string, string) error
	BindEmail(context.Context, Principal, string, string) (BindResult, error)
	BindWeChat(context.Context, Principal, string, string) (BindResult, error)
	Me(context.Context, Principal) (AccountSummary, error)
}
