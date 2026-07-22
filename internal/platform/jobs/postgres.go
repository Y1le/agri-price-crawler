package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	minMaxAttempts = 1
	maxMaxAttempts = 10
	minClaimLimit  = 1
	maxClaimLimit  = 100
)

// PostgresRepository is a PostgreSQL-backed Repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*PostgresRepository)(nil)

// NewPostgresRepository creates a Repository backed by pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Enqueue creates a pending job, or returns the existing job with the same
// kind and business key.
func (r *PostgresRepository) Enqueue(ctx context.Context, job NewJob) (int64, bool, error) {
	if err := validateNewJob(job); err != nil {
		return 0, false, err
	}

	const insert = `
		INSERT INTO platform_jobs (kind, business_key, payload, run_at, max_attempts)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (kind, business_key) DO NOTHING
		RETURNING id`

	var id int64
	err := r.pool.QueryRow(ctx, insert, job.Kind, job.BusinessKey, job.Payload, job.RunAt, job.MaxAttempts).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("insert job: %w", err)
	}

	const existing = `SELECT id FROM platform_jobs WHERE kind = $1 AND business_key = $2`
	if err := r.pool.QueryRow(ctx, existing, job.Kind, job.BusinessKey).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("select existing job: %w", err)
	}

	return id, false, nil
}

// Claim atomically recovers expired jobs and claims up to limit ready jobs.
func (r *PostgresRepository) Claim(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration, limit int) ([]Job, error) {
	if limit < minClaimLimit || limit > maxClaimLimit {
		return nil, fmt.Errorf("claim limit must be between %d and %d", minClaimLimit, maxClaimLimit)
	}
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("claim worker ID is required")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("claim lease duration must be positive")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // A committed transaction cannot be rolled back.

	const recoverExpired = `
		WITH expired AS (
			SELECT id
			FROM platform_jobs
			WHERE state = 'running' AND locked_at <= $1
			ORDER BY locked_at, id
			FOR UPDATE SKIP LOCKED
		)
		UPDATE platform_jobs AS jobs
		SET state = CASE WHEN jobs.attempts < jobs.max_attempts THEN 'pending' ELSE 'dead' END,
			last_error = CASE
				WHEN jobs.attempts < jobs.max_attempts THEN 'job lease expired'
				ELSE 'job lease expired after final attempt'
			END,
			lock_owner = NULL,
			locked_at = NULL,
			updated_at = $2
		FROM expired
		WHERE jobs.id = expired.id`
	if _, err := tx.Exec(ctx, recoverExpired, now.Add(-leaseDuration), now); err != nil {
		return nil, fmt.Errorf("recover expired jobs: %w", err)
	}

	const claim = `
		WITH picked AS (
			SELECT id
			FROM platform_jobs
			WHERE state = 'pending'
				AND run_at <= $2
				AND attempts < max_attempts
			ORDER BY run_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		), updated AS (
			UPDATE platform_jobs AS jobs
			SET state = 'running',
				attempts = jobs.attempts + 1,
				lock_owner = $1,
				locked_at = $2,
				updated_at = $2
			FROM picked
			WHERE jobs.id = picked.id
			RETURNING jobs.id, jobs.kind, jobs.business_key, jobs.payload,
				jobs.run_at, jobs.attempts, jobs.max_attempts
		)
		SELECT id, kind, business_key, payload, run_at, attempts, max_attempts
		FROM updated
		ORDER BY run_at, id`

	rows, err := tx.Query(ctx, claim, workerID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}

	return jobs, nil
}

// Complete marks a running job owned by workerID as successfully completed.
func (r *PostgresRepository) Complete(ctx context.Context, id int64, workerID string) error {
	const complete = `
		UPDATE platform_jobs
		SET state = 'succeeded', last_error = NULL,
			lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running' AND lock_owner = $2`
	return r.requireOneUpdate(ctx, complete, id, workerID)
}

// Retry returns a running job owned by workerID to pending.
func (r *PostgresRepository) Retry(ctx context.Context, id int64, workerID string, runAt time.Time, lastError string) error {
	const retry = `
		UPDATE platform_jobs
		SET state = 'pending', run_at = $3, last_error = $4,
			lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running' AND lock_owner = $2`
	return r.requireOneUpdate(ctx, retry, id, workerID, runAt, lastError)
}

// Dead marks a running job owned by workerID as permanently failed.
func (r *PostgresRepository) Dead(ctx context.Context, id int64, workerID string, lastError string) error {
	const dead = `
		UPDATE platform_jobs
		SET state = 'dead', last_error = $3,
			lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running' AND lock_owner = $2`
	return r.requireOneUpdate(ctx, dead, id, workerID, lastError)
}

func (r *PostgresRepository) requireOneUpdate(ctx context.Context, query string, args ...any) error {
	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update job lifecycle: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("update job lifecycle: expected one affected row, got %d", result.RowsAffected())
	}
	return nil
}

func validateNewJob(job NewJob) error {
	if strings.TrimSpace(job.Kind) == "" {
		return errors.New("job kind is required")
	}
	if strings.TrimSpace(job.BusinessKey) == "" {
		return errors.New("job business key is required")
	}
	if len(job.Payload) == 0 || !json.Valid(job.Payload) {
		return errors.New("job payload must be nonempty valid JSON")
	}
	if job.MaxAttempts < minMaxAttempts || job.MaxAttempts > maxMaxAttempts {
		return fmt.Errorf("job max attempts must be between %d and %d", minMaxAttempts, maxMaxAttempts)
	}
	return nil
}

func scanJob(rows pgx.Rows) (Job, error) {
	var (
		job     Job
		payload []byte
	)
	if err := rows.Scan(
		&job.ID,
		&job.Kind,
		&job.BusinessKey,
		&payload,
		&job.RunAt,
		&job.Attempts,
		&job.MaxAttempts,
	); err != nil {
		return Job{}, fmt.Errorf("scan claimed job: %w", err)
	}
	job.Payload = json.RawMessage(payload)
	return job, nil
}
