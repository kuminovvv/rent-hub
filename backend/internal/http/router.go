package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"spb-rent/backend/internal/buildings"
	"spb-rent/backend/internal/config"
	"spb-rent/backend/internal/geocoding"
	"spb-rent/backend/internal/repository/postgres"
	"spb-rent/backend/internal/service"
)

func NewRouter(cfg config.Config, pool *pgxpool.Pool, listings *service.ListingService) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	handler := NewListingHandler(listings)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := pool.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "down"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/listings", handler.List)
		r.Get("/listings/{id}", handler.Get)
		commute := NewCommuteHandler(cfg.GeoapifyAPIKey, cfg.GeoapifyDailyLimit, cfg.GeoapifyMinuteLimit)
		geoapifyBudget := geocoding.NewPostgresStore(pool)
		commute.SetGlobalReservation(func(ctx context.Context) error {
			return geoapifyBudget.ReserveRequest(ctx, cfg.GeoapifyGlobalLimit)
		})
		RegisterTelegramRoutes(r, cfg.TelegramAPIKey, cfg.TelegramMediaDir, postgres.NewTelegramRepository(pool), commute)
		RegisterBuildingRoutes(r, cfg.TelegramAPIKey, buildings.NewPostgresStore(pool))
	})

	return r
}

func RegisterTelegramRoutes(r chi.Router, apiKey, mediaDir string, reader TelegramReader, commuteHandlers ...*CommuteHandler) {
	handler := NewTelegramHandler(reader, mediaDir)
	var commute *CommuteHandler
	if len(commuteHandlers) > 0 {
		commute = commuteHandlers[0]
	}
	r.Route("/telegram", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler { return telegramAPIKeyMiddleware(apiKey, next) })
		r.Get("/posts", handler.ListPosts)
		r.Get("/map", handler.ListMapPosts)
		r.Get("/sources", handler.ListSources)
		r.Get("/media/{photoID}", handler.GetMedia)
		r.Route("/commute", func(r chi.Router) {
			if commute == nil {
				r.Post("/", func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
				})
				r.Post("/geocode", func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
				})
				r.Post("/autocomplete", func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
				})
				return
			}
			r.Post("/", commute.HandleRoute)
			r.Post("/geocode", commute.HandleGeocode)
			r.Post("/autocomplete", commute.HandleAutocomplete)
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
