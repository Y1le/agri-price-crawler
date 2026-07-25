package ingestion

import (
	"context"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

// BatchRepository is Ingestion's durable batch and raw-record seam.
type BatchRepository interface {
	Start(ctx context.Context, source string, date BusinessDate, jobID int64) (Batch, bool, error)
	StartRepair(ctx context.Context, source string, date BusinessDate, request RepairRequest) (Batch, error)
	AppendRaw(ctx context.Context, batchID uuid.UUID, record RawRecord) error
	ListPending(ctx context.Context, batchID uuid.UUID) ([]RawRecord, error)
	MarkRaw(ctx context.Context, rawID int64, result ValidationResult) error
	FinishFetching(ctx context.Context, batchID uuid.UUID, expected *int, fetched int) error
	FinishValidation(ctx context.Context, batchID uuid.UUID, counts BatchCounts) error
	Fail(ctx context.Context, batchID uuid.UUID, code FailureCode) error
	PruneExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

type PublishCoordinator interface {
	WithinPublish(ctx context.Context, batchID uuid.UUID, fn func(PublishTx) error) error
}

type PublishTx interface {
	SQL() platformpostgres.Tx
	Batch() Batch
	MarkPublished(ctx context.Context, at time.Time) error
}
