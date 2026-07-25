package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ingestion"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct {
	pool         *pgxpool.Pool
	rawRetention time.Duration
	now          func() time.Time
}

var _ ingestion.BatchRepository = (*repository)(nil)

// NewBatchRepository creates the durable Ingestion batch repository.
func NewBatchRepository(pool *pgxpool.Pool, rawRetention time.Duration, now func() time.Time) ingestion.BatchRepository {
	if now == nil {
		now = time.Now
	}
	return &repository{pool: pool, rawRetention: rawRetention, now: now}
}

func (r *repository) Start(ctx context.Context, source string, date ingestion.BusinessDate, jobID int64) (ingestion.Batch, bool, error) {
	if err := validScheduledStart(source, date); err != nil {
		return ingestion.Batch{}, false, err
	}
	if r == nil || r.pool == nil {
		return ingestion.Batch{}, false, fmt.Errorf("start ingestion batch: repository is unavailable")
	}
	now := r.now().UTC()
	batch := ingestion.Batch{
		ID: uuid.New(), SourceName: source, BusinessDate: date, Revision: 1,
		Origin: ingestion.BatchOriginScheduled, State: ingestion.BatchStateRunning,
	}
	command, err := r.pool.Exec(ctx, `
		INSERT INTO ingestion_batches (id, source_name, business_date, revision, origin, state, started_at, published_by_job_id)
		VALUES ($1, $2, $3::date, 1, 'scheduled', 'running', $4, $5)
		ON CONFLICT (source_name, business_date, revision) DO NOTHING`, batch.ID, source, date.String(), now, jobID)
	if err != nil {
		return ingestion.Batch{}, false, fmt.Errorf("insert ingestion batch: %w", err)
	}
	if command.RowsAffected() == 1 {
		return batch, true, nil
	}
	batch, err = r.findScheduled(ctx, source, date)
	if err != nil {
		return ingestion.Batch{}, false, err
	}
	if batch.State == ingestion.BatchStateFailed {
		if !batch.FailureCode.Retryable() {
			return ingestion.Batch{}, false, fmt.Errorf("%w: %s", ingestion.ErrBatchNotRestartable, batch.FailureCode)
		}
		command, err = r.pool.Exec(ctx, `
			UPDATE ingestion_batches
			SET state = 'running', started_at = $2, finished_at = NULL, failure_code = NULL, failure_detail = NULL
			WHERE id = $1 AND state = 'failed' AND failure_code = 'source_retryable'`, batch.ID, now)
		if err != nil {
			return ingestion.Batch{}, false, fmt.Errorf("resume ingestion batch: %w", err)
		}
		if command.RowsAffected() != 1 {
			return ingestion.Batch{}, false, fmt.Errorf("%w: batch changed while resuming", ingestion.ErrInvalidBatchState)
		}
		batch.State, batch.FailureCode = ingestion.BatchStateRunning, ""
	}
	return batch, false, nil
}

func (r *repository) StartRepair(ctx context.Context, source string, date ingestion.BusinessDate, request ingestion.RepairRequest) (batch ingestion.Batch, err error) {
	if err := validScheduledStart(source, date); err != nil {
		return ingestion.Batch{}, err
	}
	if strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.Reason) == "" {
		return ingestion.Batch{}, fmt.Errorf("%w: actor and reason are required", ingestion.ErrInvalidRepair)
	}
	if r == nil || r.pool == nil {
		return ingestion.Batch{}, fmt.Errorf("start ingestion repair: repository is unavailable")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ingestion.Batch{}, fmt.Errorf("begin ingestion repair: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var activeID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM ingestion_batches
		WHERE source_name = $1 AND business_date = $2::date AND state IN ('running', 'validated')
		FOR UPDATE`, source, date.String()).Scan(&activeID)
	if err == nil {
		return ingestion.Batch{}, fmt.Errorf("%w: active batch %s", ingestion.ErrRepairNotAllowed, activeID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ingestion.Batch{}, fmt.Errorf("lock active ingestion batch: %w", err)
	}

	var replaces uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM ingestion_batches
		WHERE source_name = $1 AND business_date = $2::date AND state = 'published'
		FOR UPDATE`, source, date.String()).Scan(&replaces)
	if errors.Is(err, pgx.ErrNoRows) {
		return ingestion.Batch{}, fmt.Errorf("%w: no published batch", ingestion.ErrRepairNotAllowed)
	}
	if err != nil {
		return ingestion.Batch{}, fmt.Errorf("lock published ingestion batch: %w", err)
	}

	var revision int
	if err = tx.QueryRow(ctx, `
		SELECT revision FROM ingestion_batches
		WHERE source_name = $1 AND business_date = $2::date
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE`, source, date.String()).Scan(&revision); err != nil {
		return ingestion.Batch{}, fmt.Errorf("find ingestion repair revision: %w", err)
	}
	batch = ingestion.Batch{
		ID: uuid.New(), SourceName: source, BusinessDate: date, Revision: revision + 1,
		Origin: ingestion.BatchOriginRepair, State: ingestion.BatchStateRunning,
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO ingestion_batches (
			id, source_name, business_date, revision, origin, state, started_at,
			replaces_batch_id, repair_reason, repaired_by
		) VALUES ($1, $2, $3::date, $4, 'repair', 'running', $5, $6, $7, $8)`,
		batch.ID, source, date.String(), batch.Revision, r.now().UTC(), replaces, strings.TrimSpace(request.Reason), strings.TrimSpace(request.Actor)); err != nil {
		return ingestion.Batch{}, fmt.Errorf("insert ingestion repair batch: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ingestion.Batch{}, fmt.Errorf("commit ingestion repair: %w", err)
	}
	return batch, nil
}

func (r *repository) AppendRaw(ctx context.Context, batchID uuid.UUID, record ingestion.RawRecord) (err error) {
	if batchID == uuid.Nil || strings.TrimSpace(record.SourceRecordKey) == "" || record.ObservedAt.IsZero() {
		return fmt.Errorf("append raw record: source record key and observed time are required")
	}
	payload, err := sanitizePayload(record.Payload)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	if r == nil || r.pool == nil {
		return fmt.Errorf("append raw record: repository is unavailable")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin raw record append: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var state ingestion.BatchState
	if err = tx.QueryRow(ctx, `SELECT state FROM ingestion_batches WHERE id = $1 FOR UPDATE`, batchID).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return ingestion.ErrBatchNotFound
	} else if err != nil {
		return fmt.Errorf("lock ingestion batch for raw append: %w", err)
	}
	if state != ingestion.BatchStateRunning {
		return fmt.Errorf("%w: append raw record to %s batch", ingestion.ErrInvalidBatchState, state)
	}
	var rawID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO ingestion_raw_records (batch_id, source_record_key, payload, payload_hash, observed_at, expires_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)
		ON CONFLICT (batch_id, source_record_key) DO NOTHING
		RETURNING id`, batchID, record.SourceRecordKey, payload, hash[:], record.ObservedAt.UTC(), r.now().UTC().Add(r.rawRetention)).Scan(&rawID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash []byte
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM ingestion_raw_records WHERE batch_id = $1 AND source_record_key = $2`, batchID, record.SourceRecordKey).Scan(&existingHash); err != nil {
			return fmt.Errorf("find duplicate raw record: %w", err)
		}
		if !bytes.Equal(existingHash, hash[:]) {
			return ingestion.ErrDuplicateConflict
		}
		err = nil
	} else if err != nil {
		return fmt.Errorf("insert raw record: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit raw record append: %w", err)
	}
	return nil
}

func (r *repository) ListPending(ctx context.Context, batchID uuid.UUID) ([]ingestion.RawRecord, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("list pending raw records: repository is unavailable")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, batch_id, source_record_key, payload, observed_at, validation_state, COALESCE(reject_code, ''), expires_at
		FROM ingestion_raw_records
		WHERE batch_id = $1 AND validation_state = 'pending'
		ORDER BY id ASC`, batchID)
	if err != nil {
		return nil, fmt.Errorf("query pending raw records: %w", err)
	}
	defer rows.Close()
	records := make([]ingestion.RawRecord, 0)
	for rows.Next() {
		var record ingestion.RawRecord
		if err := rows.Scan(&record.ID, &record.BatchID, &record.SourceRecordKey, &record.Payload, &record.ObservedAt, &record.ValidationState, &record.RejectCode, &record.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan pending raw record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending raw records: %w", err)
	}
	return records, nil
}

func (r *repository) MarkRaw(ctx context.Context, rawID int64, result ingestion.ValidationResult) error {
	if rawID <= 0 || (result.State != ingestion.RawValidationAccepted && result.State != ingestion.RawValidationRejected) {
		return fmt.Errorf("mark raw record: %w", ingestion.ErrInvalidBatchState)
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE ingestion_raw_records
		SET validation_state = $2, reject_code = NULLIF($3, '')
		WHERE id = $1 AND validation_state = 'pending'`, rawID, result.State, result.RejectCode)
	if err != nil {
		return fmt.Errorf("mark raw record: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("mark raw record: %w", ingestion.ErrInvalidBatchState)
	}
	return nil
}

func (r *repository) FinishFetching(ctx context.Context, batchID uuid.UUID, expected *int, fetched int) error {
	if fetched < 0 || (expected != nil && *expected < 0) {
		return fmt.Errorf("finish fetching: counts must not be negative")
	}
	var expectedValue any
	if expected != nil {
		expectedValue = *expected
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE ingestion_batches
		SET source_expected_count = $2, fetched_count = $3
		WHERE id = $1 AND state = 'running'`, batchID, expectedValue, fetched)
	if err != nil {
		return fmt.Errorf("finish ingestion fetching: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("finish ingestion fetching: %w", ingestion.ErrInvalidBatchState)
	}
	return nil
}

func (r *repository) FinishValidation(ctx context.Context, batchID uuid.UUID, counts ingestion.BatchCounts) error {
	if counts.Fetched < 0 || counts.Accepted < 0 || counts.Rejected < 0 || counts.Accepted+counts.Rejected > counts.Fetched {
		return fmt.Errorf("finish ingestion validation: invalid counts")
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE ingestion_batches
		SET state = 'validated', fetched_count = $2, accepted_count = $3, rejected_count = $4, finished_at = $5
		WHERE id = $1 AND state = 'running'`, batchID, counts.Fetched, counts.Accepted, counts.Rejected, r.now().UTC())
	if err != nil {
		return fmt.Errorf("finish ingestion validation: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("finish ingestion validation: %w", ingestion.ErrInvalidBatchState)
	}
	return nil
}

func (r *repository) Fail(ctx context.Context, batchID uuid.UUID, code ingestion.FailureCode) error {
	if !code.Valid() {
		return fmt.Errorf("fail ingestion batch: %w", ingestion.ErrInvalidFailureCode)
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE ingestion_batches
		SET state = 'failed', failure_code = $2, finished_at = $3
		WHERE id = $1 AND state IN ('running', 'validated')`, batchID, code, r.now().UTC())
	if err != nil {
		return fmt.Errorf("fail ingestion batch: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("fail ingestion batch: %w", ingestion.ErrInvalidBatchState)
	}
	return nil
}

func (r *repository) PruneExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("prune raw records: limit must be positive")
	}
	command, err := r.pool.Exec(ctx, `
		WITH expired AS (
			SELECT id FROM ingestion_raw_records
			WHERE expires_at < $1
			ORDER BY expires_at ASC, id ASC
			LIMIT $2
		)
		DELETE FROM ingestion_raw_records WHERE id IN (SELECT id FROM expired)`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("prune raw records: %w", err)
	}
	return command.RowsAffected(), nil
}

func (r *repository) findScheduled(ctx context.Context, source string, date ingestion.BusinessDate) (ingestion.Batch, error) {
	batch, err := scanBatch(r.pool.QueryRow(ctx, `
		SELECT id, source_name, business_date::text, revision, origin, state, COALESCE(failure_code, '')
		FROM ingestion_batches
		WHERE source_name = $1 AND business_date = $2::date AND revision = 1`, source, date.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return ingestion.Batch{}, ingestion.ErrBatchNotFound
	}
	if err != nil {
		return ingestion.Batch{}, fmt.Errorf("find ingestion batch: %w", err)
	}
	return batch, nil
}

func scanBatch(row pgx.Row) (ingestion.Batch, error) {
	var (
		batch    ingestion.Batch
		dateText string
	)
	if err := row.Scan(&batch.ID, &batch.SourceName, &dateText, &batch.Revision, &batch.Origin, &batch.State, &batch.FailureCode); err != nil {
		return ingestion.Batch{}, err
	}
	date, err := ingestion.ParseBusinessDate(dateText)
	if err != nil {
		return ingestion.Batch{}, err
	}
	batch.BusinessDate = date
	return batch, nil
}

func validScheduledStart(source string, date ingestion.BusinessDate) error {
	if strings.TrimSpace(source) == "" {
		return fmt.Errorf("start ingestion batch: source is required")
	}
	if _, err := ingestion.ParseBusinessDate(date.String()); err != nil {
		return err
	}
	return nil
}

func sanitizePayload(raw []byte) ([]byte, error) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("sanitize raw payload: invalid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("sanitize raw payload: invalid JSON")
	}
	cleaned := sanitizeValue(decoded)
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("sanitize raw payload: %w", err)
	}
	return encoded, nil
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitivePayloadKey(key) {
				continue
			}
			cleaned[key] = sanitizeValue(child)
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(typed))
		for index, child := range typed {
			cleaned[index] = sanitizeValue(child)
		}
		return cleaned
	default:
		return value
	}
}

func sensitivePayloadKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	return strings.Contains(normalized, "authorization") || strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "signature") || strings.Contains(normalized, "sign") ||
		strings.Contains(normalized, "token") || strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "deviceid")
}
