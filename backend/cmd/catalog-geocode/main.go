package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"spb-rent/backend/internal/geocoding"
	"spb-rent/backend/internal/storage"
)

const advisoryLockID int64 = 82481643271823

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "run" && os.Args[1] != "once") {
		return errors.New("usage: catalog-geocode run|once")
	}
	apiKey := strings.TrimSpace(os.Getenv("GEOAPIFY_API_KEY"))
	if apiKey == "" {
		return errors.New("GEOAPIFY_API_KEY is required; no geocoding request was sent")
	}
	client, err := geocoding.NewClient(apiKey)
	if err != nil {
		return err
	}
	batchSize, err := intEnv("GEOCODE_MAX_PER_CYCLE", 100, 1, 1000)
	if err != nil {
		return err
	}
	dailyLimit, err := intEnv("GEOAPIFY_DAILY_LIMIT", 2500, 1, 3000)
	if err != nil {
		return err
	}
	rateLimit, err := timeEnv("GEOCODE_RATE_LIMIT", time.Second)
	if err != nil {
		return err
	}
	interval, err := timeEnv("GEOCODE_INTERVAL", 10*time.Minute)
	if err != nil {
		return err
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := storage.NewPostgres(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire process lock connection: %w", err)
	}
	defer lockConn.Release()
	var locked bool
	if err := lockConn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockID).Scan(&locked); err != nil {
		return fmt.Errorf("acquire geocoder advisory lock: %w", err)
	}
	if !locked {
		return errors.New("another catalog-geocode process already holds the database lock")
	}
	defer func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockID)
	}()

	store := geocoding.NewPostgresStore(pool)
	client.SetRequestReservation(func(ctx context.Context) error {
		return store.ReserveRequest(ctx, dailyLimit)
	})
	runner := &geocoding.Runner{
		Geocoder: client, Store: store, BatchSize: batchSize,
		RateInterval: rateLimit,
	}

	if os.Args[1] == "once" {
		stats, err := runner.RunOnce(ctx)
		logStats("one-time pass", stats, err)
		return err
	}
	return runner.Run(ctx, interval, func(stats geocoding.Stats, err error) {
		logStats("periodic pass", stats, err)
	})
}

func logStats(label string, stats geocoding.Stats, err error) {
	if err != nil {
		log.Printf("%s failed: %v", label, err)
		return
	}
	log.Printf("%s complete: cache_applied=%d checked=%d located=%d not_found=%d rejected=%d failed=%d more_pending=%t",
		label, stats.CachedApplied, stats.Addresses, stats.Located, stats.NotFound, stats.Rejected, stats.Failed, stats.Remaining)
}

func intEnv(name string, fallback, minimum, maximum int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func timeEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}
