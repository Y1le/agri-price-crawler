// Package identity defines the account and authentication module.
package identity

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxEmailBytes = 254

// ClientKind identifies the client that owns a session.
type ClientKind string

const (
	ClientWeb        ClientKind = "web"
	ClientWeChatMini ClientKind = "wechat_mini"
	ClientApp        ClientKind = "app"
)

// Validate reports whether the client is enabled in this release.
func (c ClientKind) Validate() error {
	switch c {
	case ClientWeb, ClientWeChatMini:
		return nil
	default:
		return fmt.Errorf("%w: unsupported client kind %q", ErrInvalidRequest, c)
	}
}

// IdentityKind identifies a verified external identity.
type IdentityKind string

const (
	IdentityEmail      IdentityKind = "email"
	IdentityWeChatMini IdentityKind = "wechat_mini"
)

// UserStatus identifies the lifecycle state of an account.
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserMerged   UserStatus = "merged"
	UserDisabled UserStatus = "disabled"
)

// User is the durable account root.
type User struct {
	ID         uuid.UUID
	Status     UserStatus
	MergedInto *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ExternalIdentity is a verified login identity belonging to a user.
type ExternalIdentity struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       IdentityKind
	Issuer     string
	Subject    string
	UnionID    string
	VerifiedAt time.Time
	CreatedAt  time.Time
}

// Session is one independently revocable client login.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Client     ClientKind
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

// RefreshTokenRecord is the durable hash and lifecycle of an opaque refresh
// token. Token plaintext is never persisted.
type RefreshTokenRecord struct {
	ID            uuid.UUID
	SessionID     uuid.UUID
	Hash          [32]byte
	CreatedAt     time.Time
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	RevokedAt     *time.Time
	ReplacementID *uuid.UUID
}

// Principal is the authenticated account and session carried by an access
// token.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// LoginResult contains credentials for one newly created or rotated session.
type LoginResult struct {
	User             User
	Client           ClientKind
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

// IdentitySummary is a safe-to-display identity description.
type IdentitySummary struct {
	Kind    IdentityKind
	Display string
}

// AccountSummary contains the current user and only masked identity data.
type AccountSummary struct {
	User       User
	Identities []IdentitySummary
}

// BindResult contains an account after binding. Session is non-nil only when
// an account merge replaced all prior sessions.
type BindResult struct {
	Account AccountSummary
	Session *LoginResult
}

// WeChatIdentity contains only the durable identity data returned by WeChat.
// The provider session key must not be retained here.
type WeChatIdentity struct {
	AppID   string
	OpenID  string
	UnionID string
}

// OTPChallenge describes one atomic OTP issue operation.
type OTPChallenge struct {
	EmailKey string
	Digest   string
	Purpose  string
	Owner    string
	IP       string
	TTL      time.Duration
	Cooldown time.Duration
	Window   time.Duration
	MaxEmail int
	MaxIP    int
	Attempts int
}

// OTPAttempt describes one atomic OTP verification operation.
type OTPAttempt struct {
	EmailKey string
	Digest   string
	Purpose  string
	Owner    string
}

// RateLimit describes one generic fixed-window allowance.
type RateLimit struct {
	Key    string
	Max    int
	Window time.Duration
}

// NormalizeEmail parses and canonicalizes a single bare email address.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: email is required", ErrInvalidRequest)
	}

	address, err := mail.ParseAddress(trimmed)
	if err != nil || address.Name != "" || address.Address != trimmed {
		return "", fmt.Errorf("%w: invalid email", ErrInvalidRequest)
	}

	normalized := strings.ToLower(address.Address)
	if len(normalized) > maxEmailBytes {
		return "", fmt.Errorf("%w: email exceeds %d bytes", ErrInvalidRequest, maxEmailBytes)
	}
	return normalized, nil
}
