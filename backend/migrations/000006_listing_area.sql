ALTER TABLE telegram_post_facts
    ADD COLUMN IF NOT EXISTS area_sqm NUMERIC(7,2);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'telegram_post_facts_area_sqm_range'
    ) THEN
        ALTER TABLE telegram_post_facts
            ADD CONSTRAINT telegram_post_facts_area_sqm_range
            CHECK (area_sqm IS NULL OR area_sqm BETWEEN 5 AND 500);
    END IF;
END $$;

-- Re-extract existing posts once so the new structured field is populated.
-- Posts without text remain excluded from AI processing.
UPDATE telegram_post_facts f
SET ai_text_hash = ''
FROM telegram_posts p
WHERE p.channel_id = f.channel_id
  AND p.message_id = f.message_id
  AND BTRIM(p.text) <> '';
