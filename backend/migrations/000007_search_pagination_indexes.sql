CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Stable keyset ordering for the active feed. The partial predicate matches
-- the hot-path query exactly and avoids scanning deleted Telegram history.
CREATE INDEX IF NOT EXISTS idx_telegram_posts_active_cursor
    ON telegram_posts (posted_at DESC, channel_id DESC, message_id DESC)
    WHERE deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_telegram_posts_active_text_cursor
    ON telegram_posts (posted_at DESC, channel_id DESC, message_id DESC)
    WHERE deleted = FALSE AND BTRIM(text) <> '';

CREATE INDEX IF NOT EXISTS idx_telegram_posts_source_lower
    ON telegram_posts (LOWER(source_username), posted_at DESC, channel_id DESC, message_id DESC)
    WHERE deleted = FALSE;

-- Substring search is part of the product UX, so trigram GIN indexes are a
-- better match than B-tree indexes for ILIKE '%query%'.
CREATE INDEX IF NOT EXISTS idx_telegram_posts_text_trgm
    ON telegram_posts USING GIN (text gin_trgm_ops)
    WHERE deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_telegram_posts_source_title_trgm
    ON telegram_posts USING GIN (source_title gin_trgm_ops)
    WHERE deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_telegram_posts_source_username_trgm
    ON telegram_posts USING GIN (source_username gin_trgm_ops)
    WHERE deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_metro_trgm
    ON telegram_post_facts USING GIN (metro gin_trgm_ops)
    WHERE kind = 'offer' AND metro <> '';
