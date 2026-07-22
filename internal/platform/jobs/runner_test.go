package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/jobs"
)

func TestRunnerCompletesSuccessfulJob(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 1, Kind: "prices.fetch", Payload: json.RawMessage(`{"market":"east"}`), Attempts: 1, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)
	var handlerCalled atomic.Bool
	if err := runner.Register("prices.fetch", func(context.Context, json.RawMessage) error {
		handlerCalled.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if !handlerCalled.Load() {
		t.Fatal("handler was not called")
	}

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "complete" || calls[0].id != 1 {
		t.Fatalf("lifecycle calls = %+v, want one complete for job 1", calls)
	}
	if calls[0].contextErr != nil || !calls[0].contextHasDeadline {
		t.Fatalf("lifecycle context err=%v hasDeadline=%t, want fresh bounded context", calls[0].contextErr, calls[0].contextHasDeadline)
	}
}

func TestRunnerRetriesFirstFailureAfterOneMinute(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 2, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)
	if err := runner.Register("prices.fetch", failingHandler("temporary failure")); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	assertSingleRetry(t, repository.lifecycleCalls(), 2, now.Add(time.Minute), "temporary failure")
}

func TestRunnerRetriesSecondFailureAfterTwoMinutes(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 3, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 2, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)
	if err := runner.Register("prices.fetch", failingHandler("still unavailable")); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	assertSingleRetry(t, repository.lifecycleCalls(), 3, now.Add(2*time.Minute), "still unavailable")
}

func TestRunnerMarksFinalFailureDead(t *testing.T) {
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 4, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 3, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)
	if err := runner.Register("prices.fetch", failingHandler("permanent failure")); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "dead" || calls[0].id != 4 || calls[0].lastError != "permanent failure" {
		t.Fatalf("lifecycle calls = %+v, want one dead for job 4", calls)
	}
}

func TestRunnerMarksPermanentFirstFailureDead(t *testing.T) {
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 10, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)
	cause := errors.New("invalid market code")
	if err := runner.Register("prices.fetch", func(context.Context, json.RawMessage) error {
		return jobs.Permanent(cause)
	}); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "dead" || calls[0].id != 10 || calls[0].lastError != cause.Error() {
		t.Fatalf("lifecycle calls = %+v, want first-attempt dead preserving %q", calls, cause)
	}
}

func TestRunnerMarksUnknownKindDead(t *testing.T) {
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 5, Kind: "prices.unknown", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3,
	}}}
	runner := newTestRunner(repository)

	if err := runner.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "dead" || calls[0].id != 5 || calls[0].lastError == "" {
		t.Fatalf("lifecycle calls = %+v, want one dead with an error for job 5", calls)
	}
}

func TestRunnerRejectsDuplicateHandlerRegistration(t *testing.T) {
	runner := newTestRunner(&fakeRepository{})
	handler := func(context.Context, json.RawMessage) error { return nil }
	if err := runner.Register("prices.fetch", handler); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register("prices.fetch", handler); err == nil {
		t.Fatal("second Register succeeded, want duplicate registration error")
	}
}

func TestRunnerHandlesClaimedJobsConcurrently(t *testing.T) {
	repository := &fakeRepository{claimed: []jobs.Job{
		{ID: 6, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3},
		{ID: 7, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3},
	}}
	runner := newTestRunner(repository)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	if err := runner.Register("prices.fetch", func(context.Context, json.RawMessage) error {
		started <- struct{}{}
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- runner.RunOnce(context.Background(), time.Now())
	}()

	for startedCount := 0; startedCount < 2; startedCount++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			<-done
			t.Fatal("claimed handlers did not run concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls := repository.lifecycleCalls(); len(calls) != 2 {
		t.Fatalf("lifecycle calls = %+v, want two completes", calls)
	}
}

func TestRunnerCapsRetryDelayAtOneHour(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 8, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 8, MaxAttempts: 10,
	}}}
	runner := newTestRunner(repository)
	if err := runner.Register("prices.fetch", failingHandler("still unavailable")); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	assertSingleRetry(t, repository.lifecycleCalls(), 8, now.Add(time.Hour), "still unavailable")
}

func TestRunnerRunsImmediatelyAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var claimCalls atomic.Int64
	repository := &fakeRepository{claimFunc: func(context.Context, string, time.Time, time.Duration, int) ([]jobs.Job, error) {
		claimCalls.Add(1)
		cancel()
		return nil, nil
	}}

	if err := newTestRunner(repository).Run(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := claimCalls.Load(); got != 1 {
		t.Fatalf("Claim called %d times, want once", got)
	}
}

func TestRunnerDoesNotClaimWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var claimCalls atomic.Int64
	repository := &fakeRepository{claimFunc: func(context.Context, string, time.Time, time.Duration, int) ([]jobs.Job, error) {
		claimCalls.Add(1)
		return nil, nil
	}}

	if err := newTestRunner(repository).Run(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := claimCalls.Load(); got != 0 {
		t.Fatalf("Claim called %d times after shutdown began, want zero", got)
	}
}

func TestRunnerBoundsHandlerDeadlineBelowLease(t *testing.T) {
	leaseDuration := 5 * time.Minute
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 11, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3,
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := jobs.NewRunner(repository, "worker-test", 10, leaseDuration, logger)
	if err := runner.Register("prices.fetch", func(ctx context.Context, _ json.RawMessage) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("handler context has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining >= leaseDuration {
			return errors.New("handler deadline is not strictly below the lease")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "complete" {
		t.Fatalf("lifecycle calls = %+v, want complete", calls)
	}
}

func TestRunnerCancellationRetriesWithFreshBoundedLifecycleContext(t *testing.T) {
	leaseDuration := 200 * time.Millisecond
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 12, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3,
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := jobs.NewRunner(repository, "worker-test", 10, leaseDuration, logger)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := runner.Register("prices.fetch", func(ctx context.Context, _ json.RawMessage) error {
		close(started)
		<-release
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.RunOnce(ctx, time.Now()) }()
	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("RunOnce did not observe its bounded shutdown grace period")
	}
	close(release)

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "retry" || calls[0].id != 12 {
		t.Fatalf("lifecycle calls = %+v, want cancellation retry", calls)
	}
	if calls[0].contextErr != nil || !calls[0].contextHasDeadline {
		t.Fatalf("lifecycle context err=%v hasDeadline=%t, want fresh bounded context", calls[0].contextErr, calls[0].contextHasDeadline)
	}
}

func TestRunnerCancellationMarksExhaustedJobDead(t *testing.T) {
	leaseDuration := 200 * time.Millisecond
	repository := &fakeRepository{claimed: []jobs.Job{{
		ID: 13, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 3, MaxAttempts: 3,
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := jobs.NewRunner(repository, "worker-test", 10, leaseDuration, logger)
	started := make(chan struct{})
	if err := runner.Register("prices.fetch", func(ctx context.Context, _ json.RawMessage) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.RunOnce(ctx, time.Now()) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	calls := repository.lifecycleCalls()
	if len(calls) != 1 || calls[0].action != "dead" || calls[0].id != 13 {
		t.Fatalf("lifecycle calls = %+v, want cancellation dead on exhausted attempt", calls)
	}
	if calls[0].contextErr != nil || !calls[0].contextHasDeadline {
		t.Fatalf("lifecycle context err=%v hasDeadline=%t, want fresh bounded context", calls[0].contextErr, calls[0].contextHasDeadline)
	}
}

func TestRunnerLogsLifecycleUpdateFailure(t *testing.T) {
	repository := &fakeRepository{
		claimed:     []jobs.Job{{ID: 9, Kind: "prices.fetch", Payload: json.RawMessage(`{}`), Attempts: 1, MaxAttempts: 3}},
		completeErr: errors.New("database unavailable"),
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	runner := jobs.NewRunner(repository, "worker-test", 10, 5*time.Minute, logger)
	if err := runner.Register("prices.fetch", func(context.Context, json.RawMessage) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}

	logged := output.String()
	for _, want := range []string{"job_id=9", "action=complete", "database unavailable"} {
		if !bytes.Contains([]byte(logged), []byte(want)) {
			t.Fatalf("log output %q does not contain %q", logged, want)
		}
	}
}

func newTestRunner(repository jobs.Repository) *jobs.Runner {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return jobs.NewRunner(repository, "worker-test", 10, 5*time.Minute, logger)
}

func failingHandler(message string) jobs.Handler {
	return func(context.Context, json.RawMessage) error { return errors.New(message) }
}

func assertSingleRetry(t *testing.T, calls []lifecycleCall, id int64, runAt time.Time, lastError string) {
	t.Helper()
	if len(calls) != 1 || calls[0].action != "retry" || calls[0].id != id ||
		!calls[0].runAt.Equal(runAt) || calls[0].lastError != lastError {
		t.Fatalf("lifecycle calls = %+v, want retry for job %d at %s", calls, id, runAt)
	}
}

type lifecycleCall struct {
	action             string
	id                 int64
	runAt              time.Time
	lastError          string
	contextErr         error
	contextHasDeadline bool
}

type fakeRepository struct {
	mu          sync.Mutex
	claimed     []jobs.Job
	claimFunc   func(context.Context, string, time.Time, time.Duration, int) ([]jobs.Job, error)
	completeErr error
	calls       []lifecycleCall
}

func (r *fakeRepository) Enqueue(context.Context, jobs.NewJob) (int64, bool, error) {
	return 0, false, errors.New("unexpected Enqueue call")
}

func (r *fakeRepository) Claim(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration, limit int) ([]jobs.Job, error) {
	r.mu.Lock()
	claimFunc := r.claimFunc
	claimed := append([]jobs.Job(nil), r.claimed...)
	r.mu.Unlock()
	if claimFunc != nil {
		return claimFunc(ctx, workerID, now, leaseDuration, limit)
	}
	return claimed, nil
}

func (r *fakeRepository) Complete(ctx context.Context, id int64, _ string) error {
	r.recordLifecycleContext(ctx, lifecycleCall{action: "complete", id: id})
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeErr
}

func (r *fakeRepository) Retry(ctx context.Context, id int64, _ string, runAt time.Time, lastError string) error {
	r.recordLifecycleContext(ctx, lifecycleCall{action: "retry", id: id, runAt: runAt, lastError: lastError})
	return nil
}

func (r *fakeRepository) Dead(ctx context.Context, id int64, _ string, lastError string) error {
	r.recordLifecycleContext(ctx, lifecycleCall{action: "dead", id: id, lastError: lastError})
	return nil
}

func (r *fakeRepository) recordLifecycleContext(ctx context.Context, call lifecycleCall) {
	call.contextErr = ctx.Err()
	_, call.contextHasDeadline = ctx.Deadline()
	r.record(call)
}

func (r *fakeRepository) record(call lifecycleCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *fakeRepository) lifecycleCalls() []lifecycleCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]lifecycleCall(nil), r.calls...)
}
