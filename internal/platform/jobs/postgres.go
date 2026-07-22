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

// Claim atomically claims up to limit pending jobs that are ready to run.
func (r *PostgresRepository) Claim(ctx context.Context, workerID string, now time.Time, limit int) ([]Job, error) {
	if limit < minClaimLimit || limit > maxClaimLimit {
		return nil, fmt.Errorf("claim limit must be between %d and %d", minClaimLimit, maxClaimLimit)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // A committed transaction cannot be rolled back.

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
				locked_at = now(),
				updated_at = now()
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

// Complete marks a running job as successfully completed.
func (r *PostgresRepository) Complete(ctx context.Context, id int64) error {
	const complete = `
		UPDATE platform_jobs
		SET state = 'succeeded', lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running'`
	return r.requireOneUpdate(ctx, complete, id)
}

// Retry returns a running job to pending with its next execution time and error.
func (r *PostgresRepository) Retry(ctx context.Context, id int64, runAt time.Time, lastError string) error {
	const retry = `
		UPDATE platform_jobs
		SET state = 'pending', run_at = $2, last_error = $3,
			lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running'`
	return r.requireOneUpdate(ctx, retry, id, runAt, lastError)
}

// Dead marks a running job as permanently failed with its final error.
func (r *PostgresRepository) Dead(ctx context.Context, id int64, lastError string) error {
	const dead = `
		UPDATE platform_jobs
		SET state = 'dead', last_error = $2,
			lock_owner = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running'`
	return r.requireOneUpdate(ctx, dead, id, lastError)
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
