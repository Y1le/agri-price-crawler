package jobs_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/jobs"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
)

func TestPostgresRepositoryLifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE platform_jobs RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}

	repository := jobs.NewPostgresRepository(pool)
	runAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	leaseDuration := 5 * time.Minute
	newJob := jobs.NewJob{
		Kind:        "platform.test",
		BusinessKey: "2026-07-22",
		Payload:     json.RawMessage(`{"source":"integration"}`),
		RunAt:       runAt,
		MaxAttempts: 3,
	}

	id, created, err := repository.Enqueue(ctx, newJob)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first enqueue created=false, want true")
	}

	duplicateID, created, err := repository.Enqueue(ctx, newJob)
	if err != nil {
		t.Fatal(err)
	}
	if created || duplicateID != id {
		t.Fatalf("duplicate enqueue id=%d created=%t, want id=%d created=false", duplicateID, created, id)
	}

	claimed, err := repository.Claim(ctx, "worker-1", runAt, leaseDuration, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != id || claimed[0].Attempts != 1 {
		t.Fatalf("first claim=%+v, want job id=%d attempt=1", claimed, id)
	}

	retryAt := runAt.Add(time.Minute)
	if err := repository.Retry(ctx, id, "worker-1", retryAt, "temporary failure"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Retry(ctx, id, "worker-1", retryAt, "temporary failure"); err == nil {
		t.Fatal("second retry succeeded, want an affected-row error")
	}

	claimed, err = repository.Claim(ctx, "worker-2", retryAt, leaseDuration, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != id || claimed[0].Attempts != 2 {
		t.Fatalf("second claim=%+v, want job id=%d attempt=2", claimed, id)
	}

	if err := repository.Complete(ctx, id, "worker-2"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Complete(ctx, id, "worker-2"); err == nil {
		t.Fatal("second complete succeeded, want an affected-row error")
	}

	deadJob := newJob
	deadJob.BusinessKey = "2026-07-22-dead"
	deadID, created, err := repository.Enqueue(ctx, deadJob)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("dead-job enqueue created=false, want true")
	}
	claimed, err = repository.Claim(ctx, "worker-3", runAt, leaseDuration, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != deadID {
		t.Fatalf("dead-job claim=%+v, want job id=%d", claimed, deadID)
	}
	if err := repository.Dead(ctx, deadID, "worker-3", "permanent failure"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Dead(ctx, deadID, "worker-3", "permanent failure"); err == nil {
		t.Fatal("second dead succeeded, want an affected-row error")
	}
}

func TestPostgresRepositoryReclaimsExpiredLeaseWithOwnershipFence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE platform_jobs RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}

	repository := jobs.NewPostgresRepository(pool)
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	leaseDuration := 5 * time.Minute
	id, _, err := repository.Enqueue(ctx, jobs.NewJob{
		Kind: "platform.lease", BusinessKey: "reclaim", Payload: json.RawMessage(`{}`), RunAt: now, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := repository.Claim(ctx, "worker-a", now, leaseDuration, 1)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 1 {
		t.Fatalf("worker A claim = %+v, err=%v", claimed, err)
	}
	claimed, err = repository.Claim(ctx, "worker-b", now.Add(leaseDuration+time.Nanosecond), leaseDuration, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != id || claimed[0].Attempts != 2 {
		t.Fatalf("worker B reclaim = %+v, err=%v; want job %d attempt 2", claimed, err, id)
	}
	if err := repository.Retry(ctx, id, "worker-a", now.Add(time.Minute), "stale retry"); err == nil {
		t.Fatal("worker A retried reclaimed job, want ownership-fence error")
	}
	if err := repository.Dead(ctx, id, "worker-a", "stale dead"); err == nil {
		t.Fatal("worker A killed reclaimed job, want ownership-fence error")
	}
	if err := repository.Complete(ctx, id, "worker-a"); err == nil {
		t.Fatal("worker A completed reclaimed job, want ownership-fence error")
	}
	if err := repository.Complete(ctx, id, "worker-b"); err != nil {
		t.Fatalf("worker B complete: %v", err)
	}
}

func TestPostgresRepositoryMarksExpiredExhaustedJobDead(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE platform_jobs RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}

	repository := jobs.NewPostgresRepository(pool)
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	leaseDuration := 5 * time.Minute
	id, _, err := repository.Enqueue(ctx, jobs.NewJob{
		Kind: "platform.lease", BusinessKey: "exhausted", Payload: json.RawMessage(`{}`), RunAt: now, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := repository.Claim(ctx, "worker-a", now, leaseDuration, 1); err != nil || len(claimed) != 1 {
		t.Fatalf("worker A claim = %+v, err=%v", claimed, err)
	}
	if claimed, err := repository.Claim(ctx, "worker-b", now.Add(leaseDuration+time.Nanosecond), leaseDuration, 1); err != nil || len(claimed) != 0 {
		t.Fatalf("worker B claim = %+v, err=%v; want no exhausted job", claimed, err)
	}

	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM platform_jobs WHERE id = $1", id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "dead" {
		t.Fatalf("state = %q, want dead", state)
	}
}

func TestPostgresRepositoryValidatesInputs(t *testing.T) {
	repository := jobs.NewPostgresRepository(nil)
	ctx := context.Background()
	valid := jobs.NewJob{
		Kind:        "platform.test",
		BusinessKey: "key",
		Payload:     json.RawMessage(`{}`),
		MaxAttempts: 3,
	}

	for _, test := range []struct {
		name string
		job  jobs.NewJob
	}{
		{name: "missing kind", job: jobs.NewJob{BusinessKey: valid.BusinessKey, Payload: valid.Payload, MaxAttempts: valid.MaxAttempts}},
		{name: "missing business key", job: jobs.NewJob{Kind: valid.Kind, Payload: valid.Payload, MaxAttempts: valid.MaxAttempts}},
		{name: "empty payload", job: jobs.NewJob{Kind: valid.Kind, BusinessKey: valid.BusinessKey, MaxAttempts: valid.MaxAttempts}},
		{name: "invalid payload", job: jobs.NewJob{Kind: valid.Kind, BusinessKey: valid.BusinessKey, Payload: json.RawMessage(`{`), MaxAttempts: valid.MaxAttempts}},
		{name: "too few attempts", job: jobs.NewJob{Kind: valid.Kind, BusinessKey: valid.BusinessKey, Payload: valid.Payload, MaxAttempts: 0}},
		{name: "too many attempts", job: jobs.NewJob{Kind: valid.Kind, BusinessKey: valid.BusinessKey, Payload: valid.Payload, MaxAttempts: 11}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := repository.Enqueue(ctx, test.job); err == nil {
				t.Fatal("Enqueue succeeded, want validation error")
			}
		})
	}

	for _, limit := range []int{0, 101} {
		if _, err := repository.Claim(ctx, "worker", time.Now(), time.Minute, limit); err == nil {
			t.Fatalf("Claim limit=%d succeeded, want validation error", limit)
		}
	}
	for _, leaseDuration := range []time.Duration{0, -time.Second} {
		if _, err := repository.Claim(ctx, "worker", time.Now(), leaseDuration, 1); err == nil {
			t.Fatalf("Claim lease duration=%s succeeded, want validation error", leaseDuration)
		}
	}
}
