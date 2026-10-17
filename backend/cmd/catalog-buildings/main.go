package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"spb-rent/backend/internal/buildings"
	"spb-rent/backend/internal/storage"
)

const advisoryLockID int64 = 82481643271824

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 || os.Args[1] != "once" {
		return errors.New("usage: catalog-buildings once")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	url := strings.TrimSpace(os.Getenv("BUILDING_CATALOG_URL"))
	if url == "" {
		url = buildings.DefaultFRTURL
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
		return fmt.Errorf("acquire building importer advisory lock: %w", err)
	}
	if !locked {
		return errors.New("another catalog-buildings process already holds the database lock")
	}
	defer func() { _, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockID) }()
	stats, err := (&buildings.Importer{URL: url, Store: buildings.NewPostgresStore(pool)}).Run(ctx)
	if err != nil {
		return err
	}
	log.Printf("building catalog refresh complete: rows=%d buildings=%d duplicate_rows=%d ambiguous_keys=%d listings=%d matched=%d", stats.RowsRead, stats.Buildings, stats.DuplicateRows, stats.AmbiguousKeys, stats.ListingsSeen, stats.ListingsMatched)
	return nil
}
