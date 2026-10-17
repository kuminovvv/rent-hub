ALTER TABLE telegram_post_facts
    ADD COLUMN IF NOT EXISTS ai_text_hash TEXT NOT NULL DEFAULT '';
