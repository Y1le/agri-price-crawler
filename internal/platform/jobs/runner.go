package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	maxRetryDelay          = time.Hour
	maxShutdownGrace       = 30 * time.Second
	lifecycleWriteTimeout  = 10 * time.Second
	handlerLeaseMarginPart = 5
)

// Handler processes a claimed job payload. Handlers must stop when ctx is done.
type Handler func(context.Context, json.RawMessage) error

// Runner claims and dispatches durable jobs.
type Runner struct {
	repository Repository
	workerID   string
	batchSize  int
	lease      time.Duration
	logger     *slog.Logger

	handlersMu sync.RWMutex
	handlers   map[string]Handler
}

// NewRunner creates a job runner.
func NewRunner(repository Repository, workerID string, batchSize int, leaseDuration time.Duration, logger *slog.Logger) *Runner {
	return &Runner{
		repository: repository,
		workerID:   workerID,
		batchSize:  batchSize,
		lease:      leaseDuration,
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
	if ctx.Err() != nil {
		return nil
	}
	claimed, err := r.repository.Claim(ctx, r.workerID, now, r.lease, r.batchSize)
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
		if ctx.Err() != nil {
			return nil
		}
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
		r.updateLifecycle(ctx, job.ID, "dead", func(lifecycleCtx context.Context) error {
			return r.repository.Dead(lifecycleCtx, job.ID, r.workerID, fmt.Sprintf("no handler registered for job kind %q", job.Kind))
		})
		return
	}

	handlerCtx, cancelHandler := context.WithTimeout(context.WithoutCancel(ctx), handlerTimeout(r.lease))
	defer cancelHandler()

	result := make(chan error, 1)
	go func() {
		result <- handler(handlerCtx, job.Payload)
	}()

	select {
	case err := <-result:
		r.finish(ctx, now, job, err)
	case <-handlerCtx.Done():
		r.interrupt(ctx, now, job, fmt.Errorf("job handler stopped: %w", handlerCtx.Err()))
	case <-ctx.Done():
		r.waitForShutdownGrace(ctx, handlerCtx, cancelHandler, result, now, job)
	}
}

func (r *Runner) waitForShutdownGrace(
	ctx context.Context,
	handlerCtx context.Context,
	cancelHandler context.CancelFunc,
	result <-chan error,
	now time.Time,
	job Job,
) {
	timer := time.NewTimer(shutdownGrace(r.lease))
	defer timer.Stop()

	select {
	case err := <-result:
		r.finish(ctx, now, job, err)
	case <-handlerCtx.Done():
		r.interrupt(ctx, now, job, fmt.Errorf("job handler stopped: %w", handlerCtx.Err()))
	case <-timer.C:
		cancelHandler()
		r.interrupt(ctx, now, job, fmt.Errorf("job interrupted during shutdown: %w", ctx.Err()))
	}
}

func (r *Runner) finish(ctx context.Context, now time.Time, job Job, err error) {
	if err == nil {
		r.updateLifecycle(ctx, job.ID, "complete", func(lifecycleCtx context.Context) error {
			return r.repository.Complete(lifecycleCtx, job.ID, r.workerID)
		})
		return
	}
	if IsPermanent(err) || job.Attempts >= job.MaxAttempts {
		r.updateLifecycle(ctx, job.ID, "dead", func(lifecycleCtx context.Context) error {
			return r.repository.Dead(lifecycleCtx, job.ID, r.workerID, err.Error())
		})
		return
	}
	r.updateLifecycle(ctx, job.ID, "retry", func(lifecycleCtx context.Context) error {
		return r.repository.Retry(lifecycleCtx, job.ID, r.workerID, now.Add(retryDelay(job.Attempts)), err.Error())
	})
}

func (r *Runner) interrupt(ctx context.Context, now time.Time, job Job, err error) {
	if job.Attempts >= job.MaxAttempts {
		r.updateLifecycle(ctx, job.ID, "dead", func(lifecycleCtx context.Context) error {
			return r.repository.Dead(lifecycleCtx, job.ID, r.workerID, err.Error())
		})
		return
	}
	r.updateLifecycle(ctx, job.ID, "retry", func(lifecycleCtx context.Context) error {
		return r.repository.Retry(lifecycleCtx, job.ID, r.workerID, now.Add(retryDelay(job.Attempts)), err.Error())
	})
}

func (r *Runner) handler(kind string) (Handler, bool) {
	r.handlersMu.RLock()
	defer r.handlersMu.RUnlock()
	handler, exists := r.handlers[kind]
	return handler, exists
}

func (r *Runner) updateLifecycle(ctx context.Context, jobID int64, action string, update func(context.Context) error) {
	lifecycleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lifecycleWriteTimeout)
	defer cancel()
	if err := update(lifecycleCtx); err != nil {
		r.logger.Error("job lifecycle update failed", "job_id", jobID, "action", action, "error", err)
	}
}

func handlerTimeout(lease time.Duration) time.Duration {
	margin := lease / handlerLeaseMarginPart
	if margin < time.Nanosecond {
		margin = time.Nanosecond
	}
	if lease <= margin {
		return 0
	}
	return lease - margin
}

func shutdownGrace(lease time.Duration) time.Duration {
	grace := lease / 10
	if grace > maxShutdownGrace {
		return maxShutdownGrace
	}
	return grace
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
