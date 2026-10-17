package postgres

import (
	"testing"

	"spb-rent/backend/internal/domain"
)

func TestApplyReadTimePriceFallback(t *testing.T) {
	facts := domain.ListingFacts{Kind: "offer"}
	applyReadTimePriceFallback(&facts, "Сдаю комнату. Цена 15т.р без к.у, с коммуналкой выходит ещё около 2-3т.р; залог 50%")
	if facts.PriceRub == nil || *facts.PriceRub != 15000 {
		t.Fatalf("PriceRub = %v, want 15000", facts.PriceRub)
	}

	existing := 42000
	facts.PriceRub = &existing
	applyReadTimePriceFallback(&facts, "Цена 15т.р")
	if facts.PriceRub == nil || *facts.PriceRub != existing {
		t.Fatalf("existing PriceRub changed to %v", facts.PriceRub)
	}
}
