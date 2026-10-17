package repository

import (
	"context"

	"spb-rent/backend/internal/domain"
)

type ListingRepository interface {
	List(ctx context.Context, filter domain.ListingFilter) (domain.ListingPage, error)
	GetByID(ctx context.Context, id string) (domain.Listing, error)
}
