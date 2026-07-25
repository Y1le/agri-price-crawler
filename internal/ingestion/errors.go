package ingestion

import "errors"

var (
	ErrBatchNotFound       = errors.New("ingestion batch not found")
	ErrBatchNotRestartable = errors.New("ingestion batch is not restartable")
	ErrInvalidBatchState   = errors.New("invalid ingestion batch state")
	ErrDuplicateConflict   = errors.New("duplicate source record has different payload")
	ErrInvalidFailureCode  = errors.New("invalid ingestion failure code")
	ErrInvalidRepair       = errors.New("invalid ingestion repair request")
	ErrRepairNotAllowed    = errors.New("ingestion repair is not allowed")
)
