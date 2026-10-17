ALTER TABLE telegram_sources
    ADD COLUMN IF NOT EXISTS observed_lifetime_hours DOUBLE PRECISION NOT NULL DEFAULT 168,
    ADD COLUMN IF NOT EXISTS lifetime_samples INTEGER NOT NULL DEFAULT 0 CHECK (lifetime_samples >= 0);

ALTER TABLE telegram_posts
    ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS inactive_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS superseded_by_channel_id BIGINT,
    ADD COLUMN IF NOT EXISTS superseded_by_message_id INTEGER;

UPDATE telegram_posts SET last_seen_at = updated_at WHERE last_seen_at IS NULL OR last_seen_at > NOW();

CREATE TABLE IF NOT EXISTS telegram_listing_events (
    channel_id BIGINT NOT NULL,
    message_id INTEGER NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('edited', 'deleted', 'explicit_text', 'superseded')),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, message_id, event_type)
);

CREATE INDEX IF NOT EXISTS idx_telegram_listing_events_observed
    ON telegram_listing_events (observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_telegram_posts_verification
    ON telegram_posts (channel_id, posted_at DESC, message_id)
    WHERE deleted = FALSE;
