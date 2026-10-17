package postgres

import (
	"context"
	"fmt"

	"spb-rent/backend/internal/listingextract"
)

// RebuildListingFacts refreshes derived catalog fields in bounded keyset pages.
// Geocoded coordinates survive when the explicitly extracted address is unchanged.
func (r *TelegramRepository) RebuildListingFacts(ctx context.Context, batchSize int) (int, error) {
	if batchSize < 1 || batchSize > 5000 {
		return 0, fmt.Errorf("batch size must be between 1 and 5000")
	}
	var afterChannelID int64
	var afterMessageID int
	firstPage := true
	processed := 0
	for {
		query := `
			SELECT channel_id, message_id, text
			FROM telegram_posts
			WHERE ($1::boolean OR (channel_id, message_id) > ($2, $3))
			ORDER BY channel_id, message_id
			LIMIT $4`
		rows, err := r.pool.Query(ctx, query, firstPage, afterChannelID, afterMessageID, batchSize)
		if err != nil {
			return processed, fmt.Errorf("read catalog rebuild batch: %w", err)
		}
		type sourcePost struct {
			channelID int64
			messageID int
			text      string
		}
		batch := make([]sourcePost, 0, batchSize)
		for rows.Next() {
			var post sourcePost
			if err := rows.Scan(&post.channelID, &post.messageID, &post.text); err != nil {
				rows.Close()
				return processed, fmt.Errorf("scan catalog rebuild post: %w", err)
			}
			batch = append(batch, post)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return processed, fmt.Errorf("iterate catalog rebuild batch: %w", err)
		}
		rows.Close()
		if len(batch) == 0 {
			return processed, nil
		}

		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return processed, fmt.Errorf("begin catalog rebuild batch: %w", err)
		}
		for _, post := range batch {
			facts := listingextract.Extract(post.text)
			if err := upsertListingFacts(ctx, tx, post.channelID, post.messageID, facts); err != nil {
				_ = tx.Rollback(ctx)
				return processed, fmt.Errorf("rebuild facts for %d/%d: %w", post.channelID, post.messageID, err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return processed, fmt.Errorf("commit catalog rebuild batch: %w", err)
		}
		processed += len(batch)
		last := batch[len(batch)-1]
		afterChannelID, afterMessageID, firstPage = last.channelID, last.messageID, false
	}
}
