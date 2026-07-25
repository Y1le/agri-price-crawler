package ingestion

import (
	"time"

	"github.com/google/uuid"
)

type BatchState string

const (
	BatchStateRunning    BatchState = "running"
	BatchStateFailed     BatchState = "failed"
	BatchStateValidated  BatchState = "validated"
	BatchStatePublished  BatchState = "published"
	BatchStateSuperseded BatchState = "superseded"
)

type BatchOrigin string

const (
	BatchOriginScheduled BatchOrigin = "scheduled"
	BatchOriginRepair    BatchOrigin = "repair"
)

type FailureCode string

const (
	FailureSourceRetryable       FailureCode = "source_retryable"
	FailureSourceContract        FailureCode = "source_contract"
	FailureExpectedCountMismatch FailureCode = "expected_count_mismatch"
	FailureDuplicateConflict     FailureCode = "duplicate_conflict"
	FailureRejectRatioExceeded   FailureCode = "reject_ratio_exceeded"
	FailurePublishFailed         FailureCode = "publish_failed"
)

func (c FailureCode) Retryable() bool {
	return c == FailureSourceRetryable
}

func (c FailureCode) Valid() bool {
	switch c {
	case FailureSourceRetryable, FailureSourceContract, FailureExpectedCountMismatch,
		FailureDuplicateConflict, FailureRejectRatioExceeded, FailurePublishFailed:
		return true
	default:
		return false
	}
}

type Batch struct {
	ID           uuid.UUID
	SourceName   string
	BusinessDate BusinessDate
	Revision     int
	Origin       BatchOrigin
	State        BatchState
	FailureCode  FailureCode
}

type RepairRequest struct {
	Actor  string
	Reason string
}

type RawValidationState string

const (
	RawValidationPending  RawValidationState = "pending"
	RawValidationAccepted RawValidationState = "accepted"
	RawValidationRejected RawValidationState = "rejected"
)

type RawRecord struct {
	ID              int64
	BatchID         uuid.UUID
	SourceRecordKey string
	Payload         []byte
	ObservedAt      time.Time
	ValidationState RawValidationState
	RejectCode      string
	ExpiresAt       time.Time
}

type ValidationResult struct {
	State      RawValidationState
	RejectCode string
}

type BatchCounts struct {
	Fetched  int
	Accepted int
	Rejected int
}
