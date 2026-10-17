package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"spb-rent/backend/internal/domain"
)

type ListingRepository struct {
	pool *pgxpool.Pool
}

func NewListingRepository(pool *pgxpool.Pool) *ListingRepository {
	return &ListingRepository{pool: pool}
}

func (r *ListingRepository) List(ctx context.Context, filter domain.ListingFilter) (domain.ListingPage, error) {
	query := `
		SELECT
			id::text, title, price_monthly, rooms, area_sqm, floor, floors_total,
			district, metro, walk_minutes, commission, allows_pets, furnished,
			source_name, source_url, image_url, published_at,
			COUNT(*) OVER() AS total_count
		FROM listings
		WHERE is_active = TRUE
	`
	args := make([]any, 0, 6)
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if value := strings.TrimSpace(filter.Query); value != "" {
		p := arg("%" + value + "%")
		query += fmt.Sprintf(" AND (title ILIKE %s OR district ILIKE %s OR metro ILIKE %s)", p, p, p)
	}
	if filter.Rooms != nil {
		query += " AND rooms = " + arg(*filter.Rooms)
	}
	if filter.MaxPrice != nil {
		query += " AND price_monthly <= " + arg(*filter.MaxPrice)
	}
	if filter.NoCommission {
		query += " AND commission = FALSE"
	}

	query += " ORDER BY published_at DESC"
	query += " LIMIT " + arg(filter.Limit)
	query += " OFFSET " + arg(filter.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.ListingPage{}, fmt.Errorf("list listings: %w", err)
	}
	defer rows.Close()

	page := domain.ListingPage{
		Items:  make([]domain.Listing, 0, filter.Limit),
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	for rows.Next() {
		var item domain.Listing
		var total int
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.PriceMonthly,
			&item.Rooms,
			&item.AreaSQM,
			&item.Floor,
			&item.FloorsTotal,
			&item.District,
			&item.Metro,
			&item.WalkMinutes,
			&item.Commission,
			&item.AllowsPets,
			&item.Furnished,
			&item.SourceName,
			&item.SourceURL,
			&item.ImageURL,
			&item.PublishedAt,
			&total,
		); err != nil {
			return domain.ListingPage{}, fmt.Errorf("scan listing: %w", err)
		}
		page.Total = total
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ListingPage{}, fmt.Errorf("iterate listings: %w", err)
	}

	return page, nil
}

func (r *ListingRepository) GetByID(ctx context.Context, id string) (domain.Listing, error) {
	const query = `
		SELECT
			id::text, title, price_monthly, rooms, area_sqm, floor, floors_total,
			district, metro, walk_minutes, commission, allows_pets, furnished,
			source_name, source_url, image_url, published_at
		FROM listings
		WHERE id = $1 AND is_active = TRUE
	`

	var item domain.Listing
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.Title,
		&item.PriceMonthly,
		&item.Rooms,
		&item.AreaSQM,
		&item.Floor,
		&item.FloorsTotal,
		&item.District,
		&item.Metro,
		&item.WalkMinutes,
		&item.Commission,
		&item.AllowsPets,
		&item.Furnished,
		&item.SourceName,
		&item.SourceURL,
		&item.ImageURL,
		&item.PublishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Listing{}, repositoryNotFoundError{}
		}
		return domain.Listing{}, fmt.Errorf("get listing: %w", err)
	}
	return item, nil
}

type repositoryNotFoundError struct{}

func (repositoryNotFoundError) Error() string { return "listing not found" }

func IsNotFound(err error) bool {
	var target repositoryNotFoundError
	return errors.As(err, &target)
}
