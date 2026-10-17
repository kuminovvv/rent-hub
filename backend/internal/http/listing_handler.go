package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"spb-rent/backend/internal/domain"
	"spb-rent/backend/internal/repository/postgres"
	"spb-rent/backend/internal/service"
)

type ListingHandler struct {
	service *service.ListingService
}

func NewListingHandler(service *service.ListingService) *ListingHandler {
	return &ListingHandler{service: service}
}

func (h *ListingHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := domain.ListingFilter{
		Query:        strings.TrimSpace(query.Get("q")),
		NoCommission: query.Get("no_commission") == "true",
		Limit:        intParam(query.Get("limit"), 12),
		Offset:       intParam(query.Get("offset"), 0),
	}

	if value := optionalInt(query.Get("rooms")); value != nil {
		filter.Rooms = value
	}
	if value := optionalInt(query.Get("max_price")); value != nil {
		filter.MaxPrice = value
	}

	page, err := h.service.List(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed_to_load_listings"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *ListingHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if postgres.IsNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "listing_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed_to_load_listing"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func optionalInt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

func intParam(raw string, fallback int) int {
	value := optionalInt(raw)
	if value == nil {
		return fallback
	}
	return *value
}
