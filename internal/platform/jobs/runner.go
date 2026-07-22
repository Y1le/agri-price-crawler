package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const maxRetryDelay = time.Hour

// Handler processes a claimed job payload.
type Handler func(context.Context, json.RawMessage) error

// Runner claims and dispatches durable jobs.
type Runner struct {
	repository Repository
	workerID   string
	batchSize  int
	logger     *slog.Logger

	handlersMu sync.RWMutex
	handlers   map[string]Handler
}

// NewRunner creates a job runner.
func NewRunner(repository Repository, workerID string, batchSize int, logger *slog.Logger) *Runner {
	return &Runner{
		repository: repository,
		workerID:   workerID,
		batchSize:  batchSize,
		logger:     logger,
		handlers:   make(map[string]Handler),
	}
}

// Register associates kind with handler. A kind can only be registered once.
func (r *Runner) Register(kind string, handler Handler) error {
	r.handlersMu.Lock()
	defer r.handlersMu.Unlock()

	if _, exists := r.handlers[kind]; exists {
		return fmt.Errorf("handler already registered for job kind %q", kind)
	}
	r.handlers[kind] = handler
	return nil
}

// RunOnce claims one batch of ready jobs and waits for them to finish.
func (r *Runner) RunOnce(ctx context.Context, now time.Time) error {
	claimed, err := r.repository.Claim(ctx, r.workerID, now, r.batchSize)
	if err != nil {
		return fmt.Errorf("claim jobs: %w", err)
	}

	var workers sync.WaitGroup
	workers.Add(len(claimed))
	for _, job := range claimed {
		go func() {
			defer workers.Done()
			r.handle(ctx, now, job)
		}()
	}
	workers.Wait()
	return nil
}

// Run polls immediately and then at pollInterval until ctx is cancelled.
func (r *Runner) Run(ctx context.Context, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := r.RunOnce(ctx, time.Now()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) handle(ctx context.Context, now time.Time, job Job) {
	handler, exists := r.handler(job.Kind)
	if !exists {
		r.updateLifecycle(job.ID, "dead", func() error {
			return r.repository.Dead(ctx, job.ID, fmt.Sprintf("no handler registered for job kind %q", job.Kind))
		})
		return
	}

	if err := handler(ctx, job.Payload); err != nil {
		if job.Attempts >= job.MaxAttempts {
			r.updateLifecycle(job.ID, "dead", func() error {
				return r.repository.Dead(ctx, job.ID, err.Error())
			})
			return
		}

		r.updateLifecycle(job.ID, "retry", func() error {
			return r.repository.Retry(ctx, job.ID, now.Add(retryDelay(job.Attempts)), err.Error())
		})
		return
	}

	r.updateLifecycle(job.ID, "complete", func() error {
		return r.repository.Complete(ctx, job.ID)
	})
}

func (r *Runner) handler(kind string) (Handler, bool) {
	r.handlersMu.RLock()
	defer r.handlersMu.RUnlock()
	handler, exists := r.handlers[kind]
	return handler, exists
}

func (r *Runner) updateLifecycle(jobID int64, action string, update func() error) {
	if err := update(); err != nil {
		r.logger.Error("job lifecycle update failed", "job_id", jobID, "action", action, "error", err)
	}
}

func retryDelay(attempts int) time.Duration {
	delay := time.Minute
	for attempt := 1; attempt < attempts && delay < maxRetryDelay; attempt++ {
		delay *= 2
		if delay > maxRetryDelay {
			delay = maxRetryDelay
		}
	}
	return delay
}
