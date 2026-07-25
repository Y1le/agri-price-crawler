CREATE TABLE ingestion_batches (
    id UUID PRIMARY KEY,
    source_name TEXT NOT NULL,
    business_date DATE NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    origin TEXT NOT NULL CHECK (origin IN ('scheduled', 'repair')),
    state TEXT NOT NULL CHECK (state IN ('running', 'failed', 'validated', 'published', 'superseded')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    source_expected_count INTEGER,
    fetched_count INTEGER NOT NULL DEFAULT 0 CHECK (fetched_count >= 0),
    accepted_count INTEGER NOT NULL DEFAULT 0 CHECK (accepted_count >= 0),
    rejected_count INTEGER NOT NULL DEFAULT 0 CHECK (rejected_count >= 0),
    failure_code TEXT,
    failure_detail TEXT,
    published_by_job_id BIGINT,
    replaces_batch_id UUID REFERENCES ingestion_batches(id),
    repair_reason TEXT,
    repaired_by TEXT,
    UNIQUE (source_name, business_date, revision),
    CHECK (
        (origin = 'scheduled' AND repair_reason IS NULL AND repaired_by IS NULL)
        OR (origin = 'repair' AND repair_reason IS NOT NULL AND repaired_by IS NOT NULL AND replaces_batch_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX ingestion_batches_one_published_source_date_idx
    ON ingestion_batches (source_name, business_date)
    WHERE state = 'published';

CREATE INDEX ingestion_batches_source_date_revision_idx
    ON ingestion_batches (source_name, business_date, revision DESC);

CREATE TABLE ingestion_raw_records (
    id BIGSERIAL PRIMARY KEY,
    batch_id UUID NOT NULL REFERENCES ingestion_batches(id),
    source_record_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    payload_hash BYTEA NOT NULL CHECK (octet_length(payload_hash) = 32),
    observed_at TIMESTAMPTZ NOT NULL,
    validation_state TEXT NOT NULL DEFAULT 'pending' CHECK (validation_state IN ('pending', 'accepted', 'rejected')),
    reject_code TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (batch_id, source_record_key)
);

CREATE INDEX ingestion_raw_records_expires_at_idx
    ON ingestion_raw_records (expires_at);

CREATE INDEX ingestion_raw_records_pending_batch_id_idx
    ON ingestion_raw_records (batch_id, id)
    WHERE validation_state = 'pending';

CREATE TABLE ingestion_product_mappings (
    id BIGSERIAL PRIMARY KEY,
    source_name TEXT NOT NULL,
    source_category_id TEXT NOT NULL,
    source_breed_id TEXT NOT NULL,
    product_id UUID NOT NULL REFERENCES pricing_products(id),
    source_category_name TEXT NOT NULL DEFAULT '',
    source_breed_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_name, source_category_id, source_breed_id)
);

CREATE TABLE ingestion_region_mappings (
    id BIGSERIAL PRIMARY KEY,
    source_name TEXT NOT NULL,
    source_province_id TEXT NOT NULL DEFAULT '',
    source_city_id TEXT NOT NULL DEFAULT '',
    source_district_id TEXT NOT NULL DEFAULT '',
    source_area_key TEXT NOT NULL DEFAULT '',
    region_id UUID NOT NULL REFERENCES pricing_regions(id),
    source_region_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_name, source_province_id, source_city_id, source_district_id, source_area_key)
);
