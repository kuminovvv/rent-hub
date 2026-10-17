CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS listings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    price_monthly INTEGER NOT NULL CHECK (price_monthly > 0),
    rooms INTEGER NOT NULL CHECK (rooms >= 0),
    area_sqm NUMERIC(7,2) NOT NULL CHECK (area_sqm > 0),
    floor INTEGER NOT NULL CHECK (floor > 0),
    floors_total INTEGER NOT NULL CHECK (floors_total >= floor),
    district TEXT NOT NULL,
    metro TEXT NOT NULL,
    walk_minutes INTEGER NOT NULL DEFAULT 0 CHECK (walk_minutes >= 0),
    commission BOOLEAN NOT NULL DEFAULT FALSE,
    allows_pets BOOLEAN NOT NULL DEFAULT FALSE,
    furnished BOOLEAN NOT NULL DEFAULT TRUE,
    source_name TEXT NOT NULL,
    source_url TEXT NOT NULL,
    image_url TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_listings_active_published
    ON listings (is_active, published_at DESC);

CREATE INDEX IF NOT EXISTS idx_listings_price
    ON listings (price_monthly)
    WHERE is_active = TRUE;
