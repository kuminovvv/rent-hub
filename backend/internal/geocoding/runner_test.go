package geocoding

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type fakeStore struct {
	addresses []string
	limit     int
	saved     []string
	results   []Outcome
	cached    int64
	calls     []string
	failures  []string
	cleared   []string
}

func (s *fakeStore) ApplyCachedSuccesses(context.Context) (int64, error) {
	s.calls = append(s.calls, "apply-cached")
	return s.cached, nil
}

func (s *fakeStore) PendingAddresses(_ context.Context, limit int) ([]string, error) {
	s.calls = append(s.calls, "pending")
	s.limit = limit
	return s.addresses, nil
}

func (s *fakeStore) SaveOutcome(_ context.Context, address string, outcome Outcome) error {
	s.saved = append(s.saved, address)
	s.results = append(s.results, outcome)
	return nil
}

func (s *fakeStore) RecordFailure(_ context.Context, address, _ string, _ time.Duration) error {
	s.failures = append(s.failures, address)
	return nil
}

func (s *fakeStore) ClearFailure(_ context.Context, address string) error {
	s.cleared = append(s.cleared, address)
	return nil
}

type fakeGeocoder struct {
	addresses []string
	result    Outcome
	err       error
	errs      []error
}

func (g *fakeGeocoder) Geocode(_ context.Context, address string) (Outcome, error) {
	g.addresses = append(g.addresses, address)
	if len(g.errs) > 0 {
		err := g.errs[0]
		g.errs = g.errs[1:]
		return g.result, err
	}
	return g.result, g.err
}

func TestRunnerCachesOnlyDefinitiveResults(t *testing.T) {
	store := &fakeStore{addresses: []string{"Невский проспект, 12", "улица Гоголя, 5"}, cached: 2}
	geocoder := &fakeGeocoder{result: Outcome{Status: "not_found"}}
	runner := &Runner{Geocoder: geocoder, Store: store, BatchSize: 2}

	stats, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Addresses != 2 || stats.NotFound != 2 || !stats.Remaining || stats.CachedApplied != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if store.limit != 2 || !reflect.DeepEqual(geocoder.addresses, store.addresses) || !reflect.DeepEqual(store.saved, store.addresses) {
		t.Fatalf("unexpected job flow: limit=%d sent=%v saved=%v", store.limit, geocoder.addresses, store.saved)
	}
	if !reflect.DeepEqual(store.calls[:2], []string{"apply-cached", "pending"}) {
		t.Fatalf("cache must be applied before querying new addresses, calls=%v", store.calls)
	}
}

func TestRunnerAppliesCachedSuccessesBeforeNewAddressesWithoutResendingThem(t *testing.T) {
	store := &fakeStore{cached: 3}
	geocoder := &fakeGeocoder{}
	runner := &Runner{Geocoder: geocoder, Store: store}

	stats, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.CachedApplied != 3 || len(geocoder.addresses) != 0 {
		t.Fatalf("expected cached coordinates to be applied without API requests, stats=%+v sent=%v", stats, geocoder.addresses)
	}
	if !reflect.DeepEqual(store.calls, []string{"apply-cached", "pending"}) {
		t.Fatalf("unexpected store call order: %v", store.calls)
	}
}

func TestRunnerLeavesTransientErrorsUncachedAndContinuesBatch(t *testing.T) {
	store := &fakeStore{addresses: []string{"Невский проспект, 12", "улица Гоголя, 5"}}
	geocoder := &fakeGeocoder{result: Outcome{Status: "not_found"}, errs: []error{errors.New("provider unavailable"), nil}}
	runner := &Runner{Geocoder: geocoder, Store: store, ErrorBackoff: func(int) time.Duration { return 0 }}

	stats, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 || stats.NotFound != 1 || stats.Addresses != 1 || !stats.Remaining {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if !reflect.DeepEqual(store.saved, []string{"улица Гоголя, 5"}) {
		t.Fatalf("transient result should remain uncached while later address succeeds, saved=%v", store.saved)
	}
	if !reflect.DeepEqual(store.failures, []string{"Невский проспект, 12"}) || !reflect.DeepEqual(store.cleared, []string{"улица Гоголя, 5"}) {
		t.Fatalf("unexpected failure lifecycle: failed=%v cleared=%v", store.failures, store.cleared)
	}
}

func TestRunnerCircuitBreakerStopsAfterConsecutiveTransientErrors(t *testing.T) {
	store := &fakeStore{addresses: []string{"a 1", "b 2", "c 3", "d 4"}}
	geocoder := &fakeGeocoder{err: errors.New("provider unavailable")}
	runner := &Runner{Geocoder: geocoder, Store: store, MaxConsecutiveErrors: 2, ErrorBackoff: func(int) time.Duration { return 0 }}

	stats, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 2 || !stats.Remaining || len(geocoder.addresses) != 2 || len(store.saved) != 0 {
		t.Fatalf("breaker did not stop after threshold: stats=%+v sent=%v saved=%v", stats, geocoder.addresses, store.saved)
	}
}

func TestRunnerStopsOnPermanentProviderError(t *testing.T) {
	store := &fakeStore{addresses: []string{"Невский проспект, 12", "улица Гоголя, 5"}}
	geocoder := &fakeGeocoder{err: fmt.Errorf("%w: bad credentials", ErrPermanentProvider)}
	runner := &Runner{Geocoder: geocoder, Store: store}

	_, err := runner.RunOnce(context.Background())
	if !errors.Is(err, ErrPermanentProvider) {
		t.Fatalf("err=%v, want permanent provider error", err)
	}
	if len(geocoder.addresses) != 1 || len(store.failures) != 0 {
		t.Fatalf("permanent provider error must stop without address retry state: sent=%v failures=%v", geocoder.addresses, store.failures)
	}
}

func TestRunnerStopsOnDailyLimitWithoutCachingPendingAddress(t *testing.T) {
	store := &fakeStore{addresses: []string{"Невский проспект, 12"}}
	geocoder := &fakeGeocoder{err: ErrDailyLimit}
	runner := &Runner{Geocoder: geocoder, Store: store}

	stats, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Remaining || stats.Addresses != 0 || len(store.saved) != 0 {
		t.Fatalf("daily limit must leave address pending, stats=%+v saved=%v", stats, store.saved)
	}
}
