package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"spb-rent/backend/internal/repository/postgres"
	"spb-rent/backend/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	batchSize := 500
	if raw := os.Getenv("CATALOG_REBUILD_BATCH_SIZE"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 5000 {
			log.Fatal("CATALOG_REBUILD_BATCH_SIZE must be an integer from 1 to 5000")
		}
		batchSize = value
	}
	pool, err := storage.NewPostgres(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	processed, err := postgres.NewTelegramRepository(pool).RebuildListingFacts(ctx, batchSize)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Rebuilt listing facts for %d Telegram posts.\n", processed)
}
