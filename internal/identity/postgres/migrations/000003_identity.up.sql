CREATE TABLE identity_users (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('active', 'merged', 'disabled')),
    merged_into_user_id UUID REFERENCES identity_users(id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((status = 'merged') = (merged_into_user_id IS NOT NULL)),
    CHECK (merged_into_user_id IS NULL OR merged_into_user_id <> id)
);

CREATE TABLE identity_identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity_users(id),
    kind TEXT NOT NULL CHECK (kind IN ('email', 'wechat_mini')),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    union_id TEXT,
    verified_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (kind, issuer, subject)
);
CREATE INDEX identity_identities_user_id_idx ON identity_identities(user_id);

CREATE TABLE identity_sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity_users(id),
    client_kind TEXT NOT NULL CHECK (client_kind IN ('web', 'wechat_mini', 'app')),
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT
);
CREATE INDEX identity_sessions_active_user_idx
    ON identity_sessions(user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE identity_refresh_tokens (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES identity_sessions(id),
    token_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    replacement_token_id UUID REFERENCES identity_refresh_tokens(id),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX identity_refresh_tokens_session_idx ON identity_refresh_tokens(session_id);

CREATE TABLE identity_account_merges (
    id UUID PRIMARY KEY,
    primary_user_id UUID NOT NULL REFERENCES identity_users(id),
    secondary_user_id UUID NOT NULL UNIQUE REFERENCES identity_users(id),
    initiated_by_session_id UUID NOT NULL REFERENCES identity_sessions(id),
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (primary_user_id <> secondary_user_id)
);
