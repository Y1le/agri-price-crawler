package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	bindOTPPurpose       = "bind"
	weChatBindRatePrefix = "wechat-bind:"
	revokeReasonMerge    = "account_merged"
	bindRetryLimit       = 3
)

// RequestBindEmailCode verifies the durable access-token session before
// creating a proof that only the same account may consume.
func (s *Service) RequestBindEmailCode(
	ctx context.Context,
	principal Principal,
	rawEmail string,
	rawSourceIP string,
) error {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return ErrTokenInvalid
	}
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	ipKey, err := sourceIPDigest(rawSourceIP)
	if err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	if err := s.repository.WithinTx(ctx, func(tx Tx) error {
		_, _, err := validateBindPrincipal(ctx, tx, principal, now)
		return err
	}); err != nil {
		return err
	}

	code, err := generateOTPCode(s.random)
	if err != nil {
		return fmt.Errorf("identity: generate bind code: %w", err)
	}
	challenge := OTPChallenge{
		EmailKey: emailDigest(email),
		Digest:   otpDigest(s.policy.OTPPepper, code),
		Purpose:  bindOTPPurpose,
		Owner:    principal.UserID.String(),
		IP:       ipKey,
		TTL:      s.policy.OTPTTL,
		Cooldown: s.policy.OTPCooldown,
		Window:   time.Hour,
		MaxEmail: s.policy.OTPEmailPerHour,
		MaxIP:    s.policy.OTPIPPerHour,
		Attempts: s.policy.OTPAttempts,
	}
	if err := s.otpStore.Issue(ctx, challenge); err != nil {
		return err
	}
	if err := s.emailSender.SendCode(ctx, email, code, s.policy.OTPTTL); err != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(
			context.WithoutCancel(ctx),
			smtpCleanupTimeout,
		)
		_ = s.otpStore.DeleteIfMatch(cleanupCtx, challenge)
		cancelCleanup()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("identity: deliver bind code interrupted: %w", ctxErr)
		}
		return fmt.Errorf("identity: deliver bind code: %w", ErrUpstreamUnavailable)
	}
	return nil
}

// BindEmail consumes an account-bound email proof before opening PostgreSQL.
func (s *Service) BindEmail(
	ctx context.Context,
	principal Principal,
	rawEmail string,
	code string,
) (BindResult, error) {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return BindResult{}, ErrTokenInvalid
	}
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return BindResult{}, err
	}
	if err := s.otpStore.Verify(ctx, OTPAttempt{
		EmailKey: emailDigest(email),
		Digest:   otpDigest(s.policy.OTPPepper, code),
		Purpose:  bindOTPPurpose,
		Owner:    principal.UserID.String(),
	}); err != nil {
		return BindResult{}, err
	}
	return s.bindWithIdentity(
		ctx,
		principal,
		IdentityEmail,
		emailIdentityIssuer,
		email,
		"",
	)
}

// BindWeChat applies a bind-specific source-IP limit and accepts identity
// coordinates only from the provider exchange.
func (s *Service) BindWeChat(
	ctx context.Context,
	principal Principal,
	code string,
	rawSourceIP string,
) (BindResult, error) {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return BindResult{}, ErrTokenInvalid
	}
	ipKey, err := sourceIPDigest(rawSourceIP)
	if err != nil {
		return BindResult{}, err
	}
	if err := s.otpStore.Allow(ctx, RateLimit{
		Key:    weChatBindRatePrefix + ipKey,
		Max:    s.policy.WeChatIPPerHour,
		Window: time.Hour,
	}); err != nil {
		return BindResult{}, err
	}
	provider, err := s.weChatExchanger.Exchange(ctx, code)
	if err != nil {
		return BindResult{}, classifyWeChatLoginError(err)
	}
	if !validProviderToken(provider.AppID, false) ||
		!validProviderToken(provider.OpenID, false) ||
		!validProviderToken(provider.UnionID, true) {
		return BindResult{}, fmt.Errorf(
			"identity: WeChat exchange returned invalid identity data: %w",
			ErrUpstreamUnavailable,
		)
	}
	return s.bindWithIdentity(
		ctx,
		principal,
		IdentityWeChatMini,
		provider.AppID,
		provider.OpenID,
		provider.UnionID,
	)
}

func (s *Service) bindWithIdentity(
	ctx context.Context,
	principal Principal,
	kind IdentityKind,
	issuer string,
	subject string,
	unionID string,
) (BindResult, error) {
	now := s.clock.Now().UTC()
	for attempt := 0; attempt < bindRetryLimit; attempt++ {
		var result BindResult
		err := s.repository.WithinTx(ctx, func(tx Tx) error {
			discovered, err := tx.FindIdentity(ctx, kind, issuer, subject, false)
			switch {
			case err == nil:
				if discovered.ID == uuid.Nil || discovered.UserID == uuid.Nil {
					return fmt.Errorf(
						"identity: persisted external identity is invalid: %w",
						ErrStateUnavailable,
					)
				}
			case errors.Is(err, ErrNotFound):
				discovered = ExternalIdentity{}
			default:
				return err
			}

			if discovered.UserID == uuid.Nil ||
				discovered.UserID == principal.UserID {
				var resolveErr error
				result, resolveErr = s.bindWithoutMerge(
					ctx,
					tx,
					principal,
					discovered,
					kind,
					issuer,
					subject,
					unionID,
					now,
				)
				return resolveErr
			}

			var mergeErr error
			result, mergeErr = s.mergeBoundIdentity(
				ctx,
				tx,
				principal,
				discovered,
				kind,
				issuer,
				subject,
				unionID,
				now,
			)
			return mergeErr
		})
		if err == nil {
			return result, nil
		}
		var retry *bindOwnershipChanged
		if !errors.As(err, &retry) {
			return BindResult{}, err
		}
	}
	return BindResult{}, ErrConflict
}

func (s *Service) bindWithoutMerge(
	ctx context.Context,
	tx Tx,
	principal Principal,
	discovered ExternalIdentity,
	kind IdentityKind,
	issuer string,
	subject string,
	unionID string,
	now time.Time,
) (BindResult, error) {
	user, _, err := validateBindPrincipal(ctx, tx, principal, now)
	if err != nil {
		return BindResult{}, err
	}

	locked, err := tx.FindIdentity(ctx, kind, issuer, subject, true)
	switch {
	case err == nil:
		if locked.ID == uuid.Nil || locked.UserID == uuid.Nil {
			return BindResult{}, fmt.Errorf(
				"identity: persisted external identity is invalid: %w",
				ErrStateUnavailable,
			)
		}
		if locked.UserID != user.ID {
			return BindResult{}, &bindOwnershipChanged{cause: ErrConflict}
		}
		if discovered.UserID != uuid.Nil &&
			(discovered.ID != locked.ID || discovered.UserID != locked.UserID) {
			return BindResult{}, &bindOwnershipChanged{cause: ErrConflict}
		}
		if err := updateBoundUnionID(ctx, tx, locked, unionID); err != nil {
			return BindResult{}, err
		}

	case errors.Is(err, ErrNotFound):
		if discovered.UserID != uuid.Nil {
			return BindResult{}, &bindOwnershipChanged{cause: ErrConflict}
		}
		identityID, err := uuid.NewRandomFromReader(s.random)
		if err != nil {
			return BindResult{}, fmt.Errorf("identity: generate identity ID: %w", err)
		}
		locked = ExternalIdentity{
			ID:         identityID,
			UserID:     user.ID,
			Kind:       kind,
			Issuer:     issuer,
			Subject:    subject,
			UnionID:    unionID,
			VerifiedAt: now,
			CreatedAt:  now,
		}
		if err := tx.InsertIdentity(ctx, locked); err != nil {
			if errors.Is(err, ErrConflict) {
				return BindResult{}, &bindOwnershipChanged{cause: err}
			}
			return BindResult{}, err
		}

	default:
		return BindResult{}, err
	}

	identities, err := tx.ListIdentities(ctx, user.ID)
	if err != nil {
		return BindResult{}, err
	}
	return BindResult{
		Account: accountSummary(user, identities),
	}, nil
}

func (s *Service) mergeBoundIdentity(
	ctx context.Context,
	tx Tx,
	principal Principal,
	discovered ExternalIdentity,
	kind IdentityKind,
	issuer string,
	subject string,
	unionID string,
	now time.Time,
) (BindResult, error) {
	firstID, secondID := orderedUserIDs(principal.UserID, discovered.UserID)
	first, err := tx.FindUser(ctx, firstID, true)
	if err != nil {
		return BindResult{}, bindUserLookupError(err, firstID == principal.UserID)
	}
	second, err := tx.FindUser(ctx, secondID, true)
	if err != nil {
		return BindResult{}, bindUserLookupError(err, secondID == principal.UserID)
	}
	users := map[uuid.UUID]User{first.ID: first, second.ID: second}
	current, currentOK := users[principal.UserID]
	target, targetOK := users[discovered.UserID]
	if !currentOK || !targetOK {
		return BindResult{}, fmt.Errorf(
			"identity: locked account IDs are invalid: %w",
			ErrStateUnavailable,
		)
	}

	locked, err := tx.FindIdentity(ctx, kind, issuer, subject, true)
	if errors.Is(err, ErrNotFound) {
		return BindResult{}, &bindOwnershipChanged{cause: ErrConflict}
	}
	if err != nil {
		return BindResult{}, err
	}
	if locked.ID == uuid.Nil || locked.UserID == uuid.Nil {
		return BindResult{}, fmt.Errorf(
			"identity: persisted external identity is invalid: %w",
			ErrStateUnavailable,
		)
	}
	if locked.ID != discovered.ID || locked.UserID != discovered.UserID {
		return BindResult{}, &bindOwnershipChanged{cause: ErrConflict}
	}

	session, err := tx.FindSession(ctx, principal.SessionID, true)
	if err != nil {
		return BindResult{}, accessStateError(err)
	}
	if err := validateActiveBindState(current, session, principal, now); err != nil {
		return BindResult{}, err
	}
	if err := validateMergeTarget(target); err != nil {
		return BindResult{}, err
	}
	if err := updateBoundUnionID(ctx, tx, locked, unionID); err != nil {
		return BindResult{}, err
	}

	primary, secondary := chooseMergeUsers(current, target)
	for _, participant := range s.mergeParticipants {
		if err := participant.Merge(ctx, tx.SQL(), primary.ID, secondary.ID); err != nil {
			return BindResult{}, err
		}
	}
	if err := tx.ReassignIdentities(ctx, secondary.ID, primary.ID); err != nil {
		return BindResult{}, err
	}
	if err := tx.MarkUserMerged(ctx, secondary.ID, primary.ID, now); err != nil {
		return BindResult{}, err
	}
	mergeID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return BindResult{}, fmt.Errorf("identity: generate merge ID: %w", err)
	}
	if err := tx.RecordMerge(
		ctx,
		mergeID,
		primary.ID,
		secondary.ID,
		principal.SessionID,
		now,
	); err != nil {
		return BindResult{}, err
	}

	for _, userID := range []uuid.UUID{firstID, secondID} {
		if err := tx.RevokeUserSessions(
			ctx,
			userID,
			now,
			revokeReasonMerge,
		); err != nil {
			return BindResult{}, err
		}
	}
	login, err := s.createSession(ctx, tx, primary, session.Client, now)
	if err != nil {
		return BindResult{}, err
	}
	identities, err := tx.ListIdentities(ctx, primary.ID)
	if err != nil {
		return BindResult{}, err
	}
	return BindResult{
		Account: accountSummary(primary, identities),
		Session: &login,
	}, nil
}

func validateBindPrincipal(
	ctx context.Context,
	tx Tx,
	principal Principal,
	now time.Time,
) (User, Session, error) {
	user, err := tx.FindUser(ctx, principal.UserID, true)
	if err != nil {
		return User{}, Session{}, accessStateError(err)
	}
	session, err := tx.FindSession(ctx, principal.SessionID, true)
	if err != nil {
		return User{}, Session{}, accessStateError(err)
	}
	if err := validateActiveBindState(user, session, principal, now); err != nil {
		return User{}, Session{}, err
	}
	return user, session, nil
}

func validateActiveBindState(
	user User,
	session Session,
	principal Principal,
	now time.Time,
) error {
	if user.ID == uuid.Nil ||
		user.ID != principal.UserID ||
		session.ID == uuid.Nil ||
		session.ID != principal.SessionID ||
		session.UserID != principal.UserID ||
		session.RevokedAt != nil ||
		!now.Before(session.ExpiresAt) {
		return ErrTokenInvalid
	}
	switch user.Status {
	case UserActive:
		return nil
	case UserDisabled:
		return ErrAccountDisabled
	default:
		return ErrTokenInvalid
	}
}

func validateMergeTarget(user User) error {
	if user.ID == uuid.Nil {
		return fmt.Errorf("identity: persisted merge target is invalid: %w", ErrStateUnavailable)
	}
	switch user.Status {
	case UserActive:
		return nil
	case UserDisabled:
		return ErrAccountDisabled
	default:
		return ErrConflict
	}
}

func bindUserLookupError(err error, current bool) error {
	if current {
		return accessStateError(err)
	}
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf(
			"identity: external identity has no account: %w",
			ErrStateUnavailable,
		)
	}
	return err
}

func updateBoundUnionID(
	ctx context.Context,
	tx Tx,
	external ExternalIdentity,
	unionID string,
) error {
	if unionID == "" || external.UnionID != "" {
		return nil
	}
	if err := tx.UpdateIdentityUnionID(ctx, external.ID, unionID); err != nil &&
		!errors.Is(err, ErrConflict) {
		return err
	}
	return nil
}

func orderedUserIDs(left uuid.UUID, right uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(left[:], right[:]) <= 0 {
		return left, right
	}
	return right, left
}

func chooseMergeUsers(left User, right User) (User, User) {
	switch {
	case left.CreatedAt.Before(right.CreatedAt):
		return left, right
	case right.CreatedAt.Before(left.CreatedAt):
		return right, left
	case bytes.Compare(left.ID[:], right.ID[:]) <= 0:
		return left, right
	default:
		return right, left
	}
}

func accountSummary(user User, identities []ExternalIdentity) AccountSummary {
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
	return AccountSummary{User: user, Identities: summaries}
}

type bindOwnershipChanged struct {
	cause error
}

func (e *bindOwnershipChanged) Error() string {
	return "identity: verified identity ownership changed concurrently"
}

func (e *bindOwnershipChanged) Unwrap() error {
	return e.cause
}
