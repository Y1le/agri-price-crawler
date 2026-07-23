package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	revokeReasonLogout            = "logout"
	revokeReasonLogoutAll         = "logout_all"
	revokeReasonRefreshTokenReuse = "refresh_token_reused"
	wechatIdentityDisplay         = "已绑定微信"
)

// createSession stages one durable session and refresh-token record in the
// caller's transaction. Public callers must not expose its result until their
// Repository.WithinTx call has committed successfully.
func (s *Service) createSession(
	ctx context.Context,
	tx Tx,
	user User,
	client ClientKind,
	now time.Time,
) (LoginResult, error) {
	if tx == nil || user.ID == uuid.Nil {
		return LoginResult{}, ErrTokenInvalid
	}
	if err := s.validateClient(client); err != nil {
		return LoginResult{}, err
	}
	switch user.Status {
	case UserActive:
	case UserDisabled:
		return LoginResult{}, ErrAccountDisabled
	default:
		return LoginResult{}, ErrTokenInvalid
	}

	sessionID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return LoginResult{}, fmt.Errorf("identity: generate session ID: %w", err)
	}
	refreshID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return LoginResult{}, fmt.Errorf("identity: generate refresh token ID: %w", err)
	}
	rawRefresh, refreshHash, err := NewRefreshToken(s.random)
	if err != nil {
		return LoginResult{}, err
	}

	now = now.UTC()
	refreshExpiresAt := now.Add(s.policy.RefreshTTL)
	session := Session{
		ID:         sessionID,
		UserID:     user.ID,
		Client:     client,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  refreshExpiresAt,
	}
	refresh := RefreshTokenRecord{
		ID:        refreshID,
		SessionID: sessionID,
		Hash:      refreshHash,
		CreatedAt: now,
		ExpiresAt: refreshExpiresAt,
	}
	if err := tx.InsertSession(ctx, session); err != nil {
		return LoginResult{}, err
	}
	if err := tx.InsertRefreshToken(ctx, refresh); err != nil {
		return LoginResult{}, err
	}

	access, accessExpiresAt, err := s.tokenManager.IssueAccess(
		Principal{UserID: user.ID, SessionID: sessionID},
		now,
	)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		User:             user,
		Client:           client,
		AccessToken:      access,
		RefreshToken:     rawRefresh,
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

// Refresh atomically rotates one refresh token while preserving the global
// session-before-token lock order used by single-device logout.
func (s *Service) Refresh(
	ctx context.Context,
	rawRefresh string,
	client ClientKind,
) (LoginResult, error) {
	if err := s.validateClient(client); err != nil {
		return LoginResult{}, err
	}
	if rawRefresh == "" {
		return LoginResult{}, ErrTokenInvalid
	}

	now := s.clock.Now().UTC()
	hash := HashRefreshToken(rawRefresh)
	var (
		result          LoginResult
		postCommitError error
	)
	err := s.repository.WithinTx(ctx, func(tx Tx) error {
		discovered, err := tx.FindRefreshToken(ctx, hash, false)
		if err != nil {
			return refreshLookupError(err)
		}
		if discovered.SessionID == uuid.Nil {
			return ErrTokenInvalid
		}

		session, err := tx.FindSession(ctx, discovered.SessionID, true)
		if err != nil {
			return refreshLookupError(err)
		}
		locked, err := tx.FindRefreshToken(ctx, hash, true)
		if err != nil {
			return refreshLookupError(err)
		}
		if locked.ID == uuid.Nil ||
			locked.Hash != hash ||
			locked.SessionID != discovered.SessionID ||
			locked.SessionID != session.ID {
			return ErrTokenInvalid
		}
		if session.UserID == uuid.Nil ||
			session.Client != client ||
			session.RevokedAt != nil ||
			!now.Before(session.ExpiresAt) ||
			locked.RevokedAt != nil ||
			!now.Before(locked.ExpiresAt) {
			return ErrTokenInvalid
		}

		user, err := tx.FindUser(ctx, session.UserID, false)
		if err != nil {
			return refreshLookupError(err)
		}
		if user.ID != session.UserID {
			return ErrTokenInvalid
		}
		switch user.Status {
		case UserActive:
		case UserDisabled:
			return ErrAccountDisabled
		default:
			return ErrTokenInvalid
		}

		if locked.ConsumedAt != nil {
			if !now.After(locked.ConsumedAt.Add(s.policy.ReuseGrace)) {
				return ErrTokenInvalid
			}
			if err := tx.RevokeSession(
				ctx,
				session.ID,
				now,
				revokeReasonRefreshTokenReuse,
			); err != nil {
				return err
			}
			postCommitError = ErrTokenReused
			return nil
		}

		replacementID, err := uuid.NewRandomFromReader(s.random)
		if err != nil {
			return fmt.Errorf("identity: generate refresh token ID: %w", err)
		}
		rawReplacement, replacementHash, err := NewRefreshToken(s.random)
		if err != nil {
			return err
		}
		refreshExpiresAt := now.Add(s.policy.RefreshTTL)
		replacement := RefreshTokenRecord{
			ID:        replacementID,
			SessionID: session.ID,
			Hash:      replacementHash,
			CreatedAt: now,
			ExpiresAt: refreshExpiresAt,
		}
		if err := tx.InsertRefreshToken(ctx, replacement); err != nil {
			return err
		}
		if err := tx.ConsumeRefreshToken(ctx, locked.ID, now, replacementID); err != nil {
			return err
		}
		if err := tx.ExtendSession(ctx, session.ID, now, refreshExpiresAt); err != nil {
			return err
		}

		access, accessExpiresAt, err := s.tokenManager.IssueAccess(
			Principal{UserID: user.ID, SessionID: session.ID},
			now,
		)
		if err != nil {
			return err
		}
		result = LoginResult{
			User:             user,
			Client:           session.Client,
			AccessToken:      access,
			RefreshToken:     rawReplacement,
			AccessExpiresAt:  accessExpiresAt,
			RefreshExpiresAt: refreshExpiresAt,
		}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	if postCommitError != nil {
		return LoginResult{}, postCommitError
	}
	return result, nil
}

// Logout revokes only the access token's session. A missing or already
// revoked session is an idempotent success.
func (s *Service) Logout(ctx context.Context, principal Principal) error {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return ErrTokenInvalid
	}
	now := s.clock.Now().UTC()
	return s.repository.WithinTx(ctx, func(tx Tx) error {
		session, err := tx.FindSession(ctx, principal.SessionID, true)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if session.UserID != principal.UserID {
			return ErrTokenInvalid
		}
		if session.RevokedAt != nil {
			return nil
		}
		return tx.RevokeSession(ctx, session.ID, now, revokeReasonLogout)
	})
}

// LogoutAll revalidates the account and principal session while holding locks
// in the required user-before-session order, then revokes every device.
func (s *Service) LogoutAll(ctx context.Context, principal Principal) error {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return ErrTokenInvalid
	}
	now := s.clock.Now().UTC()
	return s.repository.WithinTx(ctx, func(tx Tx) error {
		user, err := tx.FindUser(ctx, principal.UserID, true)
		if err != nil {
			return accessStateError(err)
		}
		if user.ID != principal.UserID {
			return ErrTokenInvalid
		}
		switch user.Status {
		case UserActive:
		case UserDisabled:
			return ErrAccountDisabled
		default:
			return ErrTokenInvalid
		}

		session, err := tx.FindSession(ctx, principal.SessionID, true)
		if err != nil {
			return accessStateError(err)
		}
		if session.UserID != user.ID ||
			session.RevokedAt != nil ||
			!now.Before(session.ExpiresAt) {
			return ErrTokenInvalid
		}
		return tx.RevokeUserSessions(ctx, user.ID, now, revokeReasonLogoutAll)
	})
}

// Me returns self-contained account data and safe identity descriptions. It
// intentionally does not re-read the session for this ordinary authenticated
// read.
func (s *Service) Me(
	ctx context.Context,
	principal Principal,
) (AccountSummary, error) {
	if principal.UserID == uuid.Nil {
		return AccountSummary{}, ErrTokenInvalid
	}
	user, identities, err := s.repository.UserSummary(ctx, principal.UserID)
	if err != nil {
		return AccountSummary{}, accessStateError(err)
	}
	if user.ID != principal.UserID {
		return AccountSummary{}, ErrTokenInvalid
	}
	switch user.Status {
	case UserActive:
	case UserDisabled:
		return AccountSummary{}, ErrAccountDisabled
	default:
		return AccountSummary{}, ErrTokenInvalid
	}

	summaries := make([]IdentitySummary, 0, len(identities))
	for _, external := range identities {
		switch external.Kind {
		case IdentityEmail:
			summaries = append(summaries, IdentitySummary{
				Kind:    external.Kind,
				Display: maskEmailIdentity(external.Subject),
			})
		case IdentityWeChatMini:
			summaries = append(summaries, IdentitySummary{
				Kind:    external.Kind,
				Display: wechatIdentityDisplay,
			})
		}
	}
	return AccountSummary{User: user, Identities: summaries}, nil
}

func refreshLookupError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return ErrTokenInvalid
	}
	return err
}

func accessStateError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return ErrTokenInvalid
	}
	return err
}

func maskEmailIdentity(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return "***"
	}
	local, domain := email[:at], email[at+1:]
	if local == "" {
		return "***@" + domain
	}
	first, _ := utf8.DecodeRuneInString(local)
	return string(first) + "***@" + domain
}
