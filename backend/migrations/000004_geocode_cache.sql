CREATE TABLE IF NOT EXISTS geocode_cache (
    address_key TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('success', 'not_found', 'rejected')),
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    confidence DOUBLE PRECISION,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (status = 'success' AND latitude IS NOT NULL AND longitude IS NOT NULL AND confidence IS NOT NULL)
        OR
        (status <> 'success' AND latitude IS NULL AND longitude IS NULL AND confidence IS NULL)
    ),
    CHECK (latitude IS NULL OR (latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180)),
    CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1)
);

CREATE TABLE IF NOT EXISTS geocode_daily_usage (
    request_date DATE PRIMARY KEY,
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0)
);
