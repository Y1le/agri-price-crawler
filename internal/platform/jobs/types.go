// Package jobs provides durable background-job storage primitives.
package jobs

import (
	"context"
	"encoding/json"
	"time"
)

// Job is a claimed or persisted background job.
type Job struct {
	ID          int64
	Kind        string
	BusinessKey string
	Payload     json.RawMessage
	RunAt       time.Time
	Attempts    int
	MaxAttempts int
}

// NewJob contains the fields needed to enqueue a background job.
type NewJob struct {
	Kind        string
	BusinessKey string
	Payload     json.RawMessage
	RunAt       time.Time
	MaxAttempts int
}

// Repository persists and advances background jobs through their lifecycle.
type Repository interface {
	Enqueue(context.Context, NewJob) (int64, bool, error)
	Claim(context.Context, string, time.Time, int) ([]Job, error)
	Complete(context.Context, int64) error
	Retry(context.Context, int64, time.Time, string) error
	Dead(context.Context, int64, string) error
}
