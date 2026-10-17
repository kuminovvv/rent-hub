package geocoding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) ApplyCachedSuccesses(ctx context.Context) (int64, error) {
	const query = `
		UPDATE telegram_post_facts f
		SET latitude = c.latitude, longitude = c.longitude, updated_at = NOW()
		FROM geocode_cache c
		WHERE f.kind = 'offer'
		  AND f.address_key IS NOT NULL
		  AND f.address_key <> ''
		  AND f.latitude IS NULL
		  AND f.longitude IS NULL
		  AND c.status = 'success'
		  AND c.latitude IS NOT NULL
		  AND c.longitude IS NOT NULL
		  AND c.address_key = f.address_key
	`
	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("apply cached coordinates to offers: %w", err)
	}
	return result.RowsAffected(), nil
}

func (s *PostgresStore) PendingAddresses(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, errors.New("pending address limit must be positive")
	}
	const query = `
		SELECT DISTINCT btrim(f.address) AS address
		FROM telegram_post_facts f
		WHERE f.kind = 'offer'
		  AND f.address_key IS NOT NULL
		  AND f.address_key <> ''
		  AND f.latitude IS NULL
		  AND f.longitude IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM geocode_cache c
		      WHERE c.address_key = f.address_key
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM geocode_failures gf
		      WHERE gf.address_key = f.address_key
		        AND gf.next_retry_at > NOW()
		  )
		ORDER BY address
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending geocoding addresses: %w", err)
	}
	defer rows.Close()

	addresses := make([]string, 0, limit)
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, fmt.Errorf("scan pending address: %w", err)
		}
		addresses = append(addresses, address)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending addresses: %w", err)
	}
	return addresses, nil
}

func (s *PostgresStore) RecordFailure(ctx context.Context, address, message string, retryAfter time.Duration) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errors.New("address is required")
	}
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		message = message[:1000]
	}
	seconds := retryAfter.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	const query = `
		INSERT INTO geocode_failures (address_key, address, failure_count, last_error, next_retry_at, updated_at)
		VALUES (lower(btrim($1)), btrim($1), 1, $2, NOW() + ($3 * interval '1 second'), NOW())
		ON CONFLICT (address_key) DO UPDATE SET
			address = EXCLUDED.address,
			failure_count = geocode_failures.failure_count + 1,
			last_error = EXCLUDED.last_error,
			next_retry_at = EXCLUDED.next_retry_at,
			updated_at = NOW()
	`
	if _, err := s.pool.Exec(ctx, query, address, message, seconds); err != nil {
		return fmt.Errorf("record geocoding failure: %w", err)
	}
	return nil
}

func (s *PostgresStore) ClearFailure(ctx context.Context, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM geocode_failures WHERE address_key = lower(btrim($1))`, address); err != nil {
		return fmt.Errorf("clear geocoding failure: %w", err)
	}
	return nil
}

func (s *PostgresStore) SaveOutcome(ctx context.Context, address string, outcome Outcome) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errors.New("address is required")
	}
	if outcome.Status != "success" && outcome.Status != "not_found" && outcome.Status != "rejected" {
		return errors.New("invalid geocoding outcome status")
	}
	if outcome.Status == "success" {
		if outcome.Coordinate == nil || !validStoredCoordinate(*outcome.Coordinate) {
			return errors.New("successful geocoding outcome requires valid coordinates")
		}
	} else if outcome.Coordinate != nil {
		return errors.New("unsuccessful geocoding outcome must not contain coordinates")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin geocode outcome transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var latitude, longitude any
	var confidence any
	if outcome.Coordinate != nil {
		latitude = outcome.Coordinate.Latitude
		longitude = outcome.Coordinate.Longitude
		confidence = outcome.Coordinate.Confidence
	}
	const cacheQuery = `
		INSERT INTO geocode_cache (address_key, status, latitude, longitude, confidence, updated_at)
		VALUES (lower(btrim($1)), $2, $3, $4, $5, NOW())
		ON CONFLICT (address_key) DO UPDATE SET
			status = EXCLUDED.status,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			confidence = EXCLUDED.confidence,
			updated_at = NOW()
	`
	if _, err := tx.Exec(ctx, cacheQuery, address, outcome.Status, latitude, longitude, confidence); err != nil {
		return fmt.Errorf("cache geocoding outcome: %w", err)
	}
	if outcome.Coordinate != nil {
		const updateQuery = `
			UPDATE telegram_post_facts
			SET latitude = $1, longitude = $2, updated_at = NOW()
			WHERE kind = 'offer'
			  AND latitude IS NULL
			  AND longitude IS NULL
			  AND address_key = lower(btrim($3))
		`
		if _, err := tx.Exec(ctx, updateQuery, outcome.Coordinate.Latitude, outcome.Coordinate.Longitude, address); err != nil {
			return fmt.Errorf("update offer coordinates: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit geocode outcome: %w", err)
	}
	return nil
}

func (s *PostgresStore) ReserveRequest(ctx context.Context, dailyLimit int) error {
	if dailyLimit <= 0 {
		return ErrDailyLimit
	}
	const query = `
		INSERT INTO geocode_daily_usage (request_date, request_count)
		VALUES ((NOW() AT TIME ZONE 'UTC')::date, 1)
		ON CONFLICT (request_date) DO UPDATE
		SET request_count = geocode_daily_usage.request_count + 1
		WHERE geocode_daily_usage.request_count < $1
		RETURNING request_count
	`
	var count int
	if err := s.pool.QueryRow(ctx, query, dailyLimit).Scan(&count); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDailyLimit
		}
		return fmt.Errorf("reserve Geoapify request budget: %w", err)
	}
	return nil
}

func validStoredCoordinate(coordinate Coordinate) bool {
	return coordinate.Latitude >= 59.55 && coordinate.Latitude <= 60.35 &&
		coordinate.Longitude >= 29.25 && coordinate.Longitude <= 31.35 &&
		coordinate.Confidence >= 0.95
}
