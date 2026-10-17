CREATE TABLE IF NOT EXISTS geocode_failures (
    address_key TEXT PRIMARY KEY,
    address TEXT NOT NULL,
    failure_count INTEGER NOT NULL DEFAULT 1 CHECK (failure_count > 0),
    last_error TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_geocode_failures_retry
    ON geocode_failures (next_retry_at);
