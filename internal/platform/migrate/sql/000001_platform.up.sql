CREATE TABLE platform_jobs (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL,
    business_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'succeeded', 'dead')),
    run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    lock_owner TEXT,
    locked_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, business_key)
);

CREATE INDEX platform_jobs_pending_run_at_id_idx
    ON platform_jobs (run_at, id)
    WHERE state = 'pending';

CREATE TABLE platform_outbox (
    id BIGSERIAL PRIMARY KEY,
    topic TEXT NOT NULL,
    business_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    UNIQUE (topic, business_key)
);

CREATE INDEX platform_outbox_unpublished_created_at_id_idx
    ON platform_outbox (created_at, id)
    WHERE published_at IS NULL;
