package service

import (
	"context"

	"spb-rent/backend/internal/domain"
	"spb-rent/backend/internal/repository"
)

type ListingService struct {
	repo repository.ListingRepository
}

func NewListingService(repo repository.ListingRepository) *ListingService {
	return &ListingService{repo: repo}
}

func (s *ListingService) List(ctx context.Context, filter domain.ListingFilter) (domain.ListingPage, error) {
	if filter.Limit <= 0 {
		filter.Limit = 12
	}
	if filter.Limit > 50 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return s.repo.List(ctx, filter)
}

func (s *ListingService) GetByID(ctx context.Context, id string) (domain.Listing, error) {
	return s.repo.GetByID(ctx, id)
}
