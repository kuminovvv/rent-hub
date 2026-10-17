package geocoding

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Geocoder interface {
	Geocode(context.Context, string) (Outcome, error)
}

type Store interface {
	ApplyCachedSuccesses(context.Context) (int64, error)
	PendingAddresses(context.Context, int) ([]string, error)
	SaveOutcome(context.Context, string, Outcome) error
	RecordFailure(context.Context, string, string, time.Duration) error
	ClearFailure(context.Context, string) error
}

type Runner struct {
	Geocoder     Geocoder
	Store        Store
	BatchSize    int
	RateInterval time.Duration
	Now          func() time.Time
	// MaxConsecutiveErrors acts as a simple circuit breaker for transient
	// provider failures. A zero value uses 3. The current address is left
	// pending and later addresses in the batch are attempted until the breaker
	// opens.
	MaxConsecutiveErrors int
	ErrorBackoff         func(int) time.Duration
}

type Stats struct {
	CachedApplied int64
	Addresses     int
	Located       int
	NotFound      int
	Rejected      int
	Failed        int
	Remaining     bool
}

func (r *Runner) RunOnce(ctx context.Context) (Stats, error) {
	if r.Geocoder == nil || r.Store == nil {
		return Stats{}, errors.New("geocoder and store are required")
	}
	cachedApplied, err := r.Store.ApplyCachedSuccesses(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("apply cached geocoding successes: %w", err)
	}
	batchSize := r.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	addresses, err := r.Store.PendingAddresses(ctx, batchSize)
	if err != nil {
		return Stats{}, fmt.Errorf("load pending addresses: %w", err)
	}
	stats := Stats{CachedApplied: cachedApplied, Remaining: len(addresses) == batchSize}
	maxErrors := r.MaxConsecutiveErrors
	if maxErrors <= 0 {
		maxErrors = 3
	}
	errorBackoff := r.ErrorBackoff
	if errorBackoff == nil {
		errorBackoff = func(attempt int) time.Duration { return jitteredBackoff(max(attempt-1, 0)) }
	}
	consecutiveErrors := 0
	for index, address := range addresses {
		if index > 0 && r.RateInterval > 0 {
			if err := wait(ctx, r.RateInterval); err != nil {
				return stats, err
			}
		}
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		outcome, err := r.Geocoder.Geocode(ctx, address)
		if errors.Is(err, ErrDailyLimit) {
			stats.Remaining = true
			return stats, nil
		}
		if errors.Is(err, ErrPermanentProvider) {
			return stats, err
		}
		if err != nil {
			// Do not cache transient service errors. Continue with other addresses
			// so one provider/network failure does not abort the whole batch.
			stats.Failed++
			stats.Remaining = true
			consecutiveErrors++
			delay := errorBackoff(consecutiveErrors)
			if recordErr := r.Store.RecordFailure(ctx, address, err.Error(), delay); recordErr != nil {
				return stats, fmt.Errorf("record geocoding failure: %w", recordErr)
			}
			if consecutiveErrors >= maxErrors {
				// Provider is probably degraded. Stop this pass without turning the
				// periodic worker unhealthy; the next cycle will retry pending rows.
				return stats, nil
			}
			if delay > 0 {
				if waitErr := wait(ctx, delay); waitErr != nil {
					return stats, waitErr
				}
			}
			continue
		}
		consecutiveErrors = 0
		if err := r.Store.SaveOutcome(ctx, address, outcome); err != nil {
			return stats, fmt.Errorf("save geocoding outcome: %w", err)
		}
		if err := r.Store.ClearFailure(ctx, address); err != nil {
			return stats, fmt.Errorf("clear geocoding failure: %w", err)
		}
		stats.Addresses++
		switch outcome.Status {
		case "success":
			stats.Located++
		case "not_found":
			stats.NotFound++
		case "rejected":
			stats.Rejected++
		default:
			return stats, fmt.Errorf("geocoder returned an invalid status")
		}
	}
	return stats, nil
}

func (r *Runner) Run(ctx context.Context, interval time.Duration, onCycle func(Stats, error)) error {
	if interval <= 0 {
		return errors.New("geocode cycle interval must be positive")
	}
	for {
		stats, err := r.RunOnce(ctx)
		if onCycle != nil {
			onCycle(stats, err)
		}
		if err != nil {
			// Keep the periodic job alive on transient database/provider errors.
			if waitErr := wait(ctx, interval); waitErr != nil {
				return nil
			}
			continue
		}
		if err := wait(ctx, interval); err != nil {
			return nil
		}
	}
}
