package listingai

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"spb-rent/backend/internal/domain"
)

type Store interface {
	ListUnprocessedListingPosts(context.Context, int) ([]domain.TelegramPost, error)
	SaveListingFacts(context.Context, domain.TelegramPost, domain.ListingFacts) error
}

type Extractor interface {
	Extract(context.Context, string) (domain.ListingFacts, error)
}

// ProcessPending extracts a bounded batch. Failed posts remain pending and can
// be retried during the next sync cycle; successful posts are content-hash tracked.
func ProcessPending(ctx context.Context, store Store, extractor Extractor, limit int) (int, error) {
	return ProcessPendingConcurrent(ctx, store, extractor, limit, 1)
}

// ProcessPendingConcurrent extracts a bounded batch with a bounded worker pool.
// Each post remains idempotent because SaveListingFacts content-hash tracks the
// source post; failed jobs stay pending for a later pass.
func ProcessPendingConcurrent(ctx context.Context, store Store, extractor Extractor, limit, workers int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, fmt.Errorf("QWEN_MAX_PER_CYCLE must be between 1 and 500")
	}
	if workers < 1 || workers > 32 {
		return 0, fmt.Errorf("QWEN_WORKERS must be between 1 and 32")
	}
	posts, err := store.ListUnprocessedListingPosts(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(posts) == 0 {
		return 0, nil
	}
	if workers > len(posts) {
		workers = len(posts)
	}

	jobs := make(chan domain.TelegramPost)
	var processed atomic.Int64
	var failures atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for post := range jobs {
				if ctx.Err() != nil {
					return
				}
				facts, err := extractor.Extract(ctx, post.Text)
				if err == nil {
					err = store.SaveListingFacts(ctx, post, facts)
				}
				if err != nil {
					failures.Add(1)
					log.Printf("Qwen structuring failed for Telegram post %d/%d: %v", post.ChannelID, post.MessageID, err)
					continue
				}
				processed.Add(1)
			}
		}()
	}

	for _, post := range posts {
		select {
		case jobs <- post:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return int(processed.Load()), ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return int(processed.Load()), err
	}
	if failed := failures.Load(); failed > 0 {
		return int(processed.Load()), fmt.Errorf("Qwen structuring failed for %d of %d pending posts", failed, len(posts))
	}
	return int(processed.Load()), nil
}
