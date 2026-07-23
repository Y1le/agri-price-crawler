package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
)

const rollbackTimeout = 5 * time.Second

type repository struct {
	pool *pgxpool.Pool
}

type transaction struct {
	tx pgx.Tx
}

var (
	_ identity.Repository = (*repository)(nil)
	_ identity.Tx         = (*transaction)(nil)
)

// NewRepository creates the durable Identity repository backed by pool.
func NewRepository(pool *pgxpool.Pool) identity.Repository {
	return &repository{pool: pool}
}

func (r *repository) WithinTx(ctx context.Context, fn func(identity.Tx) error) error {
	return r.runTransaction(ctx, pgx.TxOptions{}, fn)
}

func (r *repository) runTransaction(
	ctx context.Context,
	options pgx.TxOptions,
	fn func(identity.Tx) error,
) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("begin identity transaction: %w", identity.ErrStateUnavailable)
	}
	if fn == nil {
		return fmt.Errorf("run identity transaction: callback is nil")
	}

	pgxTx, err := r.pool.BeginTx(ctx, options)
	if err != nil {
		return mapDatabaseError("begin identity transaction", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = pgxTx.Rollback(rollbackCtx)
	}()

	if err := fn(&transaction{tx: pgxTx}); err != nil {
		return mapDatabaseError("run identity transaction", err)
	}
	if err := pgxTx.Commit(ctx); err != nil {
		return mapDatabaseError("commit identity transaction", err)
	}
	committed = true
	return nil
}

func (r *repository) UserSummary(
	ctx context.Context,
	userID uuid.UUID,
) (identity.User, []identity.ExternalIdentity, error) {
	var (
		user       identity.User
		identities []identity.ExternalIdentity
	)
	err := r.runTransaction(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	}, func(tx identity.Tx) error {
		var err error
		user, err = tx.FindUser(ctx, userID, false)
		if err != nil {
			return err
		}
		identities, err = tx.ListIdentities(ctx, userID)
		return err
	})
	if err != nil {
		return identity.User{}, nil, err
	}
	return user, identities, nil
}

func (t *transaction) SQL() platformpostgres.Tx {
	return t.tx
}

func (t *transaction) FindIdentity(
	ctx context.Context,
	kind identity.IdentityKind,
	issuer string,
	subject string,
	forUpdate bool,
) (identity.ExternalIdentity, error) {
	query := `
		SELECT
			id,
			user_id,
			kind,
			issuer,
			subject,
			COALESCE(union_id, ''),
			verified_at,
			created_at
		FROM identity_identities
		WHERE kind = $1
		  AND issuer = $2
		  AND subject = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}

	external, err := scanIdentity(t.tx.QueryRow(ctx, query, kind, issuer, subject))
	if err != nil {
		return identity.ExternalIdentity{}, mapDatabaseError("find identity", err)
	}
	return external, nil
}

func (t *transaction) FindUser(
	ctx context.Context,
	userID uuid.UUID,
	forUpdate bool,
) (identity.User, error) {
	query := `
		SELECT
			id,
			status,
			merged_into_user_id,
			created_at,
			updated_at
		FROM identity_users
		WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}

	user, err := scanUser(t.tx.QueryRow(ctx, query, userID))
	if err != nil {
		return identity.User{}, mapDatabaseError("find user", err)
	}
	return user, nil
}

func (t *transaction) ListIdentities(
	ctx context.Context,
	userID uuid.UUID,
) ([]identity.ExternalIdentity, error) {
	const query = `
		SELECT
			id,
			user_id,
			kind,
			issuer,
			subject,
			COALESCE(union_id, ''),
			verified_at,
			created_at
		FROM identity_identities
		WHERE user_id = $1
		ORDER BY created_at, id`

	rows, err := t.tx.Query(ctx, query, userID)
	if err != nil {
		return nil, mapDatabaseError("list identities", err)
	}
	defer rows.Close()

	identities := make([]identity.ExternalIdentity, 0)
	for rows.Next() {
		external, err := scanIdentity(rows)
		if err != nil {
			return nil, mapDatabaseError("scan identity", err)
		}
		identities = append(identities, external)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDatabaseError("iterate identities", err)
	}
	return identities, nil
}

func (t *transaction) InsertUser(ctx context.Context, user identity.User) error {
	const query = `
		INSERT INTO identity_users (
			id,
			status,
			merged_into_user_id,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := t.tx.Exec(
		ctx,
		query,
		user.ID,
		user.Status,
		user.MergedInto,
		user.CreatedAt,
		user.UpdatedAt,
	); err != nil {
		return mapDatabaseError("insert user", err)
	}
	return nil
}

func (t *transaction) InsertIdentity(
	ctx context.Context,
	external identity.ExternalIdentity,
) error {
	const query = `
		INSERT INTO identity_identities (
			id,
			user_id,
			kind,
			issuer,
			subject,
			union_id,
			verified_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`
	if _, err := t.tx.Exec(
		ctx,
		query,
		external.ID,
		external.UserID,
		external.Kind,
		external.Issuer,
		external.Subject,
		external.UnionID,
		external.VerifiedAt,
		external.CreatedAt,
	); err != nil {
		return mapDatabaseError("insert identity", err)
	}
	return nil
}

func (t *transaction) UpdateIdentityUnionID(
	ctx context.Context,
	identityID uuid.UUID,
	unionID string,
) error {
	const query = `
		UPDATE identity_identities
		SET union_id = $2
		WHERE id = $1
		  AND (union_id IS NULL OR union_id = '')
		  AND $2 <> ''`
	return t.requireOne(ctx, "update identity union ID", query, identityID, unionID)
}

func (t *transaction) ReassignIdentities(
	ctx context.Context,
	fromUserID uuid.UUID,
	toUserID uuid.UUID,
) error {
	const query = `
		UPDATE identity_identities
		SET user_id = $2
		WHERE user_id = $1`
	if _, err := t.tx.Exec(ctx, query, fromUserID, toUserID); err != nil {
		return mapDatabaseError("reassign identities", err)
	}
	return nil
}

func (t *transaction) MarkUserMerged(
	ctx context.Context,
	secondaryUserID uuid.UUID,
	primaryUserID uuid.UUID,
	mergedAt time.Time,
) error {
	const query = `
		UPDATE identity_users
		SET
			status = 'merged',
			merged_into_user_id = $2,
			updated_at = $3
		WHERE id = $1
		  AND id <> $2
		  AND status = 'active'
		  AND merged_into_user_id IS NULL`
	return t.requireOne(
		ctx,
		"mark user merged",
		query,
		secondaryUserID,
		primaryUserID,
		mergedAt,
	)
}

func (t *transaction) InsertSession(ctx context.Context, session identity.Session) error {
	const query = `
		INSERT INTO identity_sessions (
			id,
			user_id,
			client_kind,
			created_at,
			last_seen_at,
			expires_at,
			revoked_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := t.tx.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.Client,
		session.CreatedAt,
		session.LastSeenAt,
		session.ExpiresAt,
		session.RevokedAt,
	); err != nil {
		return mapDatabaseError("insert session", err)
	}
	return nil
}

func (t *transaction) FindSession(
	ctx context.Context,
	sessionID uuid.UUID,
	forUpdate bool,
) (identity.Session, error) {
	query := `
		SELECT
			id,
			user_id,
			client_kind,
			created_at,
			last_seen_at,
			expires_at,
			revoked_at
		FROM identity_sessions
		WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}

	session, err := scanSession(t.tx.QueryRow(ctx, query, sessionID))
	if err != nil {
		return identity.Session{}, mapDatabaseError("find session", err)
	}
	return session, nil
}

func (t *transaction) ExtendSession(
	ctx context.Context,
	sessionID uuid.UUID,
	lastSeenAt time.Time,
	expiresAt time.Time,
) error {
	const query = `
		UPDATE identity_sessions
		SET
			last_seen_at = $2,
			expires_at = $3
		WHERE id = $1
		  AND revoked_at IS NULL`
	return t.requireOne(
		ctx,
		"extend session",
		query,
		sessionID,
		lastSeenAt,
		expiresAt,
	)
}

func (t *transaction) RevokeSession(
	ctx context.Context,
	sessionID uuid.UUID,
	revokedAt time.Time,
	reason string,
) error {
	const revokeSession = `
		UPDATE identity_sessions
		SET
			revoked_at = $2,
			revoked_reason = $3
		WHERE id = $1
		  AND revoked_at IS NULL`
	if err := t.requireOne(ctx, "revoke session", revokeSession, sessionID, revokedAt, reason); err != nil {
		return err
	}

	const lockTokens = `
		SELECT id
		FROM identity_refresh_tokens
		WHERE session_id = $1
		  AND revoked_at IS NULL
		ORDER BY id
		FOR UPDATE`
	tokenIDs, err := t.lockUUIDRows(ctx, "lock session refresh tokens", lockTokens, sessionID)
	if err != nil {
		return err
	}

	const revokeTokens = `
		UPDATE identity_refresh_tokens
		SET revoked_at = $2
		WHERE session_id = $1
		  AND revoked_at IS NULL`
	return t.requireRows(
		ctx,
		"revoke session refresh tokens",
		int64(len(tokenIDs)),
		revokeTokens,
		sessionID,
		revokedAt,
	)
}

func (t *transaction) RevokeUserSessions(
	ctx context.Context,
	userID uuid.UUID,
	revokedAt time.Time,
	reason string,
) error {
	// The owning user is the common serialization point for every bulk
	// revocation path. Deliberately do not validate status here: application
	// workflows retain responsibility for active/merged/disabled semantics.
	if _, err := t.FindUser(ctx, userID, true); err != nil {
		return err
	}

	const lockSessions = `
		SELECT id
		FROM identity_sessions
		WHERE user_id = $1
		  AND revoked_at IS NULL
		ORDER BY id
		FOR UPDATE`
	sessionIDs, err := t.lockUUIDRows(ctx, "lock user sessions", lockSessions, userID)
	if err != nil {
		return err
	}
	if len(sessionIDs) == 0 {
		return nil
	}

	const lockTokens = `
		SELECT id
		FROM identity_refresh_tokens
		WHERE session_id = ANY($1)
		  AND revoked_at IS NULL
		ORDER BY id
		FOR UPDATE`
	tokenIDs, err := t.lockUUIDRows(
		ctx,
		"lock user refresh tokens",
		lockTokens,
		sessionIDs,
	)
	if err != nil {
		return err
	}

	const revokeSessions = `
		UPDATE identity_sessions
		SET
			revoked_at = $2,
			revoked_reason = $3
		WHERE id = ANY($1)
		  AND revoked_at IS NULL`
	if err := t.requireRows(
		ctx,
		"revoke user sessions",
		int64(len(sessionIDs)),
		revokeSessions,
		sessionIDs,
		revokedAt,
		reason,
	); err != nil {
		return err
	}

	const revokeTokens = `
		UPDATE identity_refresh_tokens
		SET revoked_at = $2
		WHERE session_id = ANY($1)
		  AND revoked_at IS NULL`
	return t.requireRows(
		ctx,
		"revoke user refresh tokens",
		int64(len(tokenIDs)),
		revokeTokens,
		sessionIDs,
		revokedAt,
	)
}

func (t *transaction) InsertRefreshToken(
	ctx context.Context,
	token identity.RefreshTokenRecord,
) error {
	const query = `
		INSERT INTO identity_refresh_tokens (
			id,
			session_id,
			token_hash,
			created_at,
			expires_at,
			consumed_at,
			replacement_token_id,
			revoked_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if _, err := t.tx.Exec(
		ctx,
		query,
		token.ID,
		token.SessionID,
		token.Hash[:],
		token.CreatedAt,
		token.ExpiresAt,
		token.ConsumedAt,
		token.ReplacementID,
		token.RevokedAt,
	); err != nil {
		return mapDatabaseError("insert refresh token", err)
	}
	return nil
}

func (t *transaction) FindRefreshToken(
	ctx context.Context,
	hash [32]byte,
	forUpdate bool,
) (identity.RefreshTokenRecord, error) {
	query := `
		SELECT
			id,
			session_id,
			token_hash,
			created_at,
			expires_at,
			consumed_at,
			revoked_at,
			replacement_token_id
		FROM identity_refresh_tokens
		WHERE token_hash = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}

	token, err := scanRefreshToken(t.tx.QueryRow(ctx, query, hash[:]))
	if err != nil {
		return identity.RefreshTokenRecord{}, mapDatabaseError("find refresh token", err)
	}
	return token, nil
}

func (t *transaction) ConsumeRefreshToken(
	ctx context.Context,
	tokenID uuid.UUID,
	consumedAt time.Time,
	replacementID uuid.UUID,
) error {
	const query = `
		UPDATE identity_refresh_tokens
		SET
			consumed_at = $2,
			replacement_token_id = $3
		WHERE id = $1
		  AND consumed_at IS NULL
		  AND revoked_at IS NULL`
	return t.requireOne(
		ctx,
		"consume refresh token",
		query,
		tokenID,
		consumedAt,
		replacementID,
	)
}

func (t *transaction) RecordMerge(
	ctx context.Context,
	mergeID uuid.UUID,
	primaryUserID uuid.UUID,
	secondaryUserID uuid.UUID,
	initiatedBySessionID uuid.UUID,
	createdAt time.Time,
) error {
	const query = `
		INSERT INTO identity_account_merges (
			id,
			primary_user_id,
			secondary_user_id,
			initiated_by_session_id,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5)`
	return t.requireOne(
		ctx,
		"record account merge",
		query,
		mergeID,
		primaryUserID,
		secondaryUserID,
		initiatedBySessionID,
		createdAt,
	)
}

func (t *transaction) requireOne(
	ctx context.Context,
	operation string,
	query string,
	args ...any,
) error {
	tag, err := t.tx.Exec(ctx, query, args...)
	if err != nil {
		return mapDatabaseError(operation, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%s: %w", operation, identity.ErrConflict)
	}
	return nil
}

func (t *transaction) requireRows(
	ctx context.Context,
	operation string,
	expected int64,
	query string,
	args ...any,
) error {
	tag, err := t.tx.Exec(ctx, query, args...)
	if err != nil {
		return mapDatabaseError(operation, err)
	}
	if tag.RowsAffected() != expected {
		return fmt.Errorf(
			"%s: expected %d affected rows, got %d: %w",
			operation,
			expected,
			tag.RowsAffected(),
			identity.ErrConflict,
		)
	}
	return nil
}

func (t *transaction) lockUUIDRows(
	ctx context.Context,
	operation string,
	query string,
	args ...any,
) ([]uuid.UUID, error) {
	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, mapDatabaseError(operation, err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapDatabaseError(operation, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDatabaseError(operation, err)
	}
	return ids, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanUser(row rowScanner) (identity.User, error) {
	var user identity.User
	err := row.Scan(
		&user.ID,
		&user.Status,
		&user.MergedInto,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}

func scanIdentity(row rowScanner) (identity.ExternalIdentity, error) {
	var external identity.ExternalIdentity
	err := row.Scan(
		&external.ID,
		&external.UserID,
		&external.Kind,
		&external.Issuer,
		&external.Subject,
		&external.UnionID,
		&external.VerifiedAt,
		&external.CreatedAt,
	)
	return external, err
}

func scanSession(row rowScanner) (identity.Session, error) {
	var session identity.Session
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.Client,
		&session.CreatedAt,
		&session.LastSeenAt,
		&session.ExpiresAt,
		&session.RevokedAt,
	)
	return session, err
}

func scanRefreshToken(row rowScanner) (identity.RefreshTokenRecord, error) {
	var (
		token identity.RefreshTokenRecord
		hash  []byte
	)
	err := row.Scan(
		&token.ID,
		&token.SessionID,
		&hash,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.RevokedAt,
		&token.ReplacementID,
	)
	if err != nil {
		return identity.RefreshTokenRecord{}, err
	}
	if len(hash) != len(token.Hash) {
		return identity.RefreshTokenRecord{}, fmt.Errorf(
			"refresh token hash length is %d, want %d",
			len(hash),
			len(token.Hash),
		)
	}
	copy(token.Hash[:], hash)
	return token, nil
}

func mapDatabaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, identity.ErrNotFound)
	}

	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch {
		case postgresError.Code == "23505":
			return fmt.Errorf("%s: %w", operation, identity.ErrConflict)
		case strings.HasPrefix(postgresError.Code, "08"),
			postgresError.Code == "40001",
			postgresError.Code == "40P01",
			postgresError.Code == "57014":
			return fmt.Errorf("%s: %w", operation, identity.ErrStateUnavailable)
		}
	}
	if isStateUnavailable(err) {
		return fmt.Errorf("%s: %w", operation, identity.ErrStateUnavailable)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isStateUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, puddle.ErrClosedPool) ||
		errors.Is(err, puddle.ErrNotAvailable) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		pgconn.Timeout(err) ||
		pgconn.SafeToRetry(err) {
		return true
	}

	var connectError *pgconn.ConnectError
	if errors.As(err, &connectError) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
