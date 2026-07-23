package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const refreshTokenBytes = 32

// TokenConfig contains the immutable configuration used to issue and verify
// Identity access tokens.
type TokenConfig struct {
	PrivateKey ed25519.PrivateKey
	KeyID      string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
}

// TokenManager issues and verifies Ed25519 access tokens. It is safe for
// concurrent use.
type TokenManager struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
	issuer     string
	audience   string
	accessTTL  time.Duration
}

type accessClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// NewTokenManager validates and copies token configuration. Rebuilding the
// private key from its seed prevents later caller mutation from changing the
// manager's signing key.
func NewTokenManager(config TokenConfig) (*TokenManager, error) {
	if len(config.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("identity: Ed25519 private key must contain %d bytes", ed25519.PrivateKeySize)
	}
	if config.KeyID == "" {
		return nil, errors.New("identity: token key ID is required")
	}
	if config.Issuer == "" {
		return nil, errors.New("identity: token issuer is required")
	}
	if config.Audience == "" {
		return nil, errors.New("identity: token audience is required")
	}
	if config.AccessTTL < time.Second || config.AccessTTL%time.Second != 0 {
		return nil, errors.New("identity: access token TTL must be at least one whole second")
	}

	privateKey := ed25519.NewKeyFromSeed(config.PrivateKey.Seed())
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("identity: could not derive Ed25519 public key")
	}

	return &TokenManager{
		privateKey: privateKey,
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		keyID:      config.KeyID,
		issuer:     config.Issuer,
		audience:   config.Audience,
		accessTTL:  config.AccessTTL,
	}, nil
}

// IssueAccess signs a short-lived access token for one durable session.
func (m *TokenManager) IssueAccess(principal Principal, now time.Time) (string, time.Time, error) {
	if m == nil || principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return "", time.Time{}, ErrTokenInvalid
	}

	issuedAt := now.UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(m.accessTTL)
	claims := accessClaims{
		SessionID: principal.SessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   principal.UserID.String(),
			Audience:  jwt.ClaimStrings{m.audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = m.keyID

	raw, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("identity: sign access token: %w", err)
	}
	return raw, expiresAt, nil
}

// ParseAccess verifies an access token using only this manager's immutable
// configuration and the caller-supplied time. Validation errors deliberately
// collapse to ErrTokenInvalid so neither token material nor parser details
// escape through the public API.
func (m *TokenManager) ParseAccess(raw string, now time.Time) (Principal, error) {
	if m == nil || raw == "" {
		return Principal{}, ErrTokenInvalid
	}

	claims := &accessClaims{}
	parser := jwt.Parser{
		ValidMethods:         []string{jwt.SigningMethodEdDSA.Alg()},
		SkipClaimsValidation: true,
	}
	token, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA {
			return nil, ErrTokenInvalid
		}
		keyID, ok := token.Header["kid"].(string)
		if !ok || keyID != m.keyID {
			return nil, ErrTokenInvalid
		}
		return m.publicKey, nil
	})
	if err != nil || token == nil || !token.Valid {
		return Principal{}, ErrTokenInvalid
	}

	principal, valid := m.validateClaims(claims, now.UTC())
	if !valid {
		return Principal{}, ErrTokenInvalid
	}
	return principal, nil
}

func (m *TokenManager) validateClaims(claims *accessClaims, now time.Time) (Principal, bool) {
	if claims == nil ||
		claims.Issuer != m.issuer ||
		len(claims.Audience) != 1 ||
		claims.Audience[0] != m.audience ||
		claims.IssuedAt == nil ||
		claims.ExpiresAt == nil ||
		claims.IssuedAt.Time.After(now) ||
		!now.Before(claims.ExpiresAt.Time) ||
		!claims.ExpiresAt.Time.After(claims.IssuedAt.Time) {
		return Principal{}, false
	}

	userID, valid := parseCanonicalUUID(claims.Subject)
	if !valid {
		return Principal{}, false
	}
	sessionID, valid := parseCanonicalUUID(claims.SessionID)
	if !valid {
		return Principal{}, false
	}
	if _, valid = parseCanonicalUUID(claims.ID); !valid {
		return Principal{}, false
	}

	return Principal{UserID: userID, SessionID: sessionID}, true
}

func parseCanonicalUUID(raw string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil || parsed.String() != raw {
		return uuid.Nil, false
	}
	return parsed, true
}

// NewRefreshToken creates a 256-bit opaque token and the hash of its encoded
// plaintext. Only the hash is suitable for durable storage.
func NewRefreshToken(random io.Reader) (string, [32]byte, error) {
	if random == nil {
		return "", [32]byte{}, errors.New("identity: refresh token random source is required")
	}

	var bytes [refreshTokenBytes]byte
	if _, err := io.ReadFull(random, bytes[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("identity: generate refresh token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes[:])
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken hashes the encoded token plaintext presented by a client.
func HashRefreshToken(raw string) [32]byte {
	return sha256.Sum256([]byte(raw))
}

var _ AccessTokenIssuer = (*TokenManager)(nil)
