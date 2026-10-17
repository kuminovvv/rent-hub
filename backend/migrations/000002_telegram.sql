CREATE TABLE IF NOT EXISTS telegram_sources (
    channel_id BIGINT PRIMARY KEY,
    username TEXT NOT NULL,
    title TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS telegram_posts (
    channel_id BIGINT NOT NULL,
    message_id INTEGER NOT NULL,
    source_username TEXT NOT NULL,
    source_title TEXT NOT NULL,
    source_url TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    media JSONB,
    posted_at TIMESTAMPTZ NOT NULL,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, message_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_posts_active_posted
    ON telegram_posts (posted_at DESC, channel_id, message_id)
    WHERE deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_telegram_posts_source
    ON telegram_posts (source_username, posted_at DESC)
    WHERE deleted = FALSE;
