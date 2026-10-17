package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"spb-rent/backend/internal/domain"
)

type TelegramReader interface {
	ListPosts(ctx context.Context, filter domain.TelegramPostFilter) (domain.TelegramPostPage, error)
	ListMapPosts(ctx context.Context, filter domain.TelegramPostFilter) (domain.TelegramMapPage, error)
	ListSources(ctx context.Context) ([]domain.TelegramSource, error)
}

type TelegramHandler struct {
	reader   TelegramReader
	mediaDir string
}

func NewTelegramHandler(reader TelegramReader, mediaDir string) *TelegramHandler {
	return &TelegramHandler{reader: reader, mediaDir: mediaDir}
}

func (h *TelegramHandler) GetMedia(w http.ResponseWriter, r *http.Request) {
	photoID, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil || photoID <= 0 {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.mediaDir, fmt.Sprintf("%d.jpg", photoID))
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf("\"photo-%d-%x-%x\"", photoID, info.Size(), info.ModTime().UnixNano())
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

func (h *TelegramHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePostFilter(r.URL.Query(), true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	page, err := h.reader.ListPosts(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed_to_load_telegram_posts"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *TelegramHandler) ListMapPosts(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePostFilter(r.URL.Query(), false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	page, err := h.reader.ListMapPosts(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed_to_load_telegram_map"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func parsePostFilter(query url.Values, paginate bool) (domain.TelegramPostFilter, error) {
	filter := domain.TelegramPostFilter{
		Source:  strings.TrimSpace(query.Get("source")),
		Query:   strings.TrimSpace(query.Get("q")),
		HasText: query.Get("has_text") == "true",
		Metro:   strings.TrimSpace(query.Get("metro")),
	}
	if raw := strings.TrimSpace(query.Get("channel_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value == 0 {
			return filter, fmt.Errorf("invalid_channel_id")
		}
		filter.ChannelID = value
	}
	if raw := strings.TrimSpace(query.Get("message_id")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return filter, fmt.Errorf("invalid_message_id")
		}
		filter.MessageID = value
	}
	if (filter.ChannelID == 0) != (filter.MessageID == 0) {
		return filter, fmt.Errorf("channel_id_and_message_id_required_together")
	}
	if raw := strings.TrimSpace(query.Get("offer_only")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return filter, fmt.Errorf("invalid_offer_only")
		}
		filter.OfferOnly = value
	}
	filter.BuildingType = strings.TrimSpace(query.Get("building_type"))
	if filter.BuildingType != "" && filter.BuildingType != "new" && filter.BuildingType != "secondary" {
		return filter, fmt.Errorf("invalid_building_type")
	}
	for _, rawValue := range query["rooms"] {
		raw := strings.TrimSpace(rawValue)
		if raw == "" {
			continue
		}
		if raw == "4plus" {
			filter.RoomsPlus = true
		} else {
			rooms, err := strconv.Atoi(raw)
			if err != nil || rooms < 0 || rooms > 3 {
				return filter, fmt.Errorf("invalid_rooms")
			}
			found := false
			for _, selected := range filter.Rooms {
				if selected == rooms {
					found = true
					break
				}
			}
			if !found {
				filter.Rooms = append(filter.Rooms, rooms)
			}
		}
	}
	if filter.PriceMin, _ = optionalPositiveInt(query.Get("price_min")); strings.TrimSpace(query.Get("price_min")) != "" && filter.PriceMin == nil {
		return filter, fmt.Errorf("invalid_price_min")
	}
	if filter.PriceMax, _ = optionalPositiveInt(query.Get("price_max")); strings.TrimSpace(query.Get("price_max")) != "" && filter.PriceMax == nil {
		return filter, fmt.Errorf("invalid_price_max")
	}
	if filter.PriceMin != nil && filter.PriceMax != nil && *filter.PriceMin > *filter.PriceMax {
		return filter, fmt.Errorf("invalid_price_range")
	}
	if len(filter.Metro) > 100 {
		return filter, fmt.Errorf("invalid_metro")
	}
	var err error
	if filter.Since, err = optionalTime(query.Get("since")); err != nil {
		return filter, fmt.Errorf("invalid_since")
	}
	if filter.Until, err = optionalTime(query.Get("until")); err != nil {
		return filter, fmt.Errorf("invalid_until")
	}
	if filter.Since != nil && filter.Until != nil && filter.Since.After(*filter.Until) {
		return filter, fmt.Errorf("invalid_date_range")
	}
	if !paginate {
		if raw := strings.TrimSpace(query.Get("bbox")); raw != "" {
			bounds, err := parseMapBounds(raw)
			if err != nil {
				return filter, fmt.Errorf("invalid_bbox")
			}
			filter.MapBounds = bounds
		}
		if raw := strings.TrimSpace(query.Get("zoom")); raw != "" {
			filter.MapZoom, err = strconv.Atoi(raw)
			if err != nil || filter.MapZoom < 0 || filter.MapZoom > 22 {
				return filter, fmt.Errorf("invalid_zoom")
			}
		}
	}
	if paginate {
		filter.Limit = 50
		if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
			filter.Limit, err = strconv.Atoi(raw)
			if err != nil || filter.Limit < 1 {
				return filter, fmt.Errorf("invalid_limit")
			}
		}
		if filter.Limit > 100 {
			filter.Limit = 100
		}
		filter.Offset = 0
		if raw := strings.TrimSpace(query.Get("offset")); raw != "" {
			filter.Offset, err = strconv.Atoi(raw)
			if err != nil || filter.Offset < 0 {
				return filter, fmt.Errorf("invalid_offset")
			}
		}
		if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
			cursor, err := parsePostCursor(raw)
			if err != nil {
				return filter, fmt.Errorf("invalid_cursor")
			}
			if filter.Offset != 0 {
				return filter, fmt.Errorf("cursor_and_offset_are_mutually_exclusive")
			}
			filter.Cursor = cursor
		}
	}
	return filter, nil
}

func parseMapBounds(raw string) (*domain.TelegramMapBounds, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("bbox must contain four values")
	}
	values := make([]float64, 4)
	for index, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	west, south, east, north := values[0], values[1], values[2], values[3]
	if west < -180 || east > 180 || south < -90 || north > 90 || west >= east || south >= north {
		return nil, fmt.Errorf("bbox out of range")
	}
	return &domain.TelegramMapBounds{West: west, South: south, East: east, North: north}, nil
}

func parsePostCursor(raw string) (*domain.TelegramPostCursor, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid cursor")
	}
	micros, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || micros <= 0 {
		return nil, fmt.Errorf("invalid cursor time")
	}
	channelID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || channelID == 0 {
		return nil, fmt.Errorf("invalid cursor channel")
	}
	messageID, err := strconv.Atoi(parts[2])
	if err != nil || messageID <= 0 {
		return nil, fmt.Errorf("invalid cursor message")
	}
	return &domain.TelegramPostCursor{
		PostedAt:  time.UnixMicro(micros).UTC(),
		ChannelID: channelID,
		MessageID: messageID,
	}, nil
}

func optionalPositiveInt(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 2_000_000 {
		return nil, fmt.Errorf("invalid positive integer")
	}
	return &value, nil
}

func (h *TelegramHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.reader.ListSources(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed_to_load_telegram_sources"})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func telegramAPIKeyMiddleware(configuredKey string, next http.Handler) http.Handler {
	key := strings.TrimSpace(configuredKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-API-Key")
		if key == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram_api_key_not_configured"})
			return
		}
		if len(provided) != len(key) || subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func optionalTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
