ALTER TABLE telegram_posts
    ADD COLUMN IF NOT EXISTS has_text BOOLEAN
    GENERATED ALWAYS AS (BTRIM(text) <> '') STORED;

ALTER TABLE telegram_post_facts
    ADD COLUMN IF NOT EXISTS address_key TEXT
    GENERATED ALWAYS AS (LOWER(BTRIM(address))) STORED;

DROP INDEX IF EXISTS idx_telegram_posts_active_text_cursor;
CREATE INDEX IF NOT EXISTS idx_telegram_posts_active_text_cursor
    ON telegram_posts (posted_at DESC, channel_id DESC, message_id DESC)
    WHERE deleted = FALSE AND has_text;

CREATE INDEX IF NOT EXISTS idx_telegram_post_facts_pending_geocode
    ON telegram_post_facts (address_key)
    WHERE kind = 'offer'
      AND address_key IS NOT NULL
      AND address_key <> ''
      AND latitude IS NULL
      AND longitude IS NULL;
