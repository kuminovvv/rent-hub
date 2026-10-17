package listingai

import (
	"context"
	"sync"
	"testing"
	"time"

	"spb-rent/backend/internal/domain"
)

type concurrentTestStore struct {
	posts []domain.TelegramPost
	mu    sync.Mutex
	saved int
}

func (s *concurrentTestStore) ListUnprocessedListingPosts(context.Context, int) ([]domain.TelegramPost, error) {
	return s.posts, nil
}

func (s *concurrentTestStore) SaveListingFacts(context.Context, domain.TelegramPost, domain.ListingFacts) error {
	s.mu.Lock()
	s.saved++
	s.mu.Unlock()
	return nil
}

type blockingExtractor struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingExtractor) Extract(ctx context.Context, _ string) (domain.ListingFacts, error) {
	e.started <- struct{}{}
	select {
	case <-e.release:
		return domain.ListingFacts{}, nil
	case <-ctx.Done():
		return domain.ListingFacts{}, ctx.Err()
	}
}

func TestProcessPendingConcurrentUsesBoundedWorkerPool(t *testing.T) {
	store := &concurrentTestStore{posts: []domain.TelegramPost{
		{ChannelID: 1, MessageID: 1}, {ChannelID: 1, MessageID: 2},
		{ChannelID: 1, MessageID: 3}, {ChannelID: 1, MessageID: 4},
	}}
	extractor := &blockingExtractor{started: make(chan struct{}, 4), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := ProcessPendingConcurrent(context.Background(), store, extractor, 4, 3)
		done <- err
	}()

	for range 3 {
		select {
		case <-extractor.started:
		case <-time.After(time.Second):
			t.Fatal("expected three workers to start concurrently")
		}
	}
	select {
	case <-extractor.started:
		t.Fatal("more than three extraction jobs ran concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	close(extractor.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent processing did not finish")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.saved != 4 {
		t.Fatalf("saved %d posts, want 4", store.saved)
	}
}
