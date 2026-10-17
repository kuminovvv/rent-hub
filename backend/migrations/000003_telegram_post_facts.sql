CREATE TABLE IF NOT EXISTS telegram_post_facts (
    channel_id BIGINT NOT NULL,
    message_id INTEGER NOT NULL,
    kind TEXT NOT NULL DEFAULT 'unknown',
    building_type TEXT NOT NULL DEFAULT 'unknown',
    rooms INTEGER,
    price_rub INTEGER,
    metro TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, message_id),
    FOREIGN KEY (channel_id, message_id)
        REFERENCES telegram_posts (channel_id, message_id) ON DELETE CASCADE,
    CHECK ((latitude IS NULL) = (longitude IS NULL)),
    CHECK (latitude IS NULL OR (latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180))
);

CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_kind
    ON telegram_post_facts (kind, channel_id, message_id);
CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_price
    ON telegram_post_facts (price_rub) WHERE kind = 'offer' AND price_rub IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_rooms
    ON telegram_post_facts (rooms) WHERE kind = 'offer' AND rooms IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_building_type
    ON telegram_post_facts (building_type) WHERE kind = 'offer';
CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_geo
    ON telegram_post_facts (latitude, longitude) WHERE kind = 'offer' AND latitude IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_metro
    ON telegram_post_facts (LOWER(metro)) WHERE kind = 'offer' AND metro <> '';
