package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"spb-rent/backend/internal/domain"
)

type fakeTelegramReader struct {
	postCalls   int
	mapCalls    int
	sourceCalls int
	lastFilter  domain.TelegramPostFilter
}

func (f *fakeTelegramReader) ListPosts(_ context.Context, filter domain.TelegramPostFilter) (domain.TelegramPostPage, error) {
	f.postCalls++
	f.lastFilter = filter
	return domain.TelegramPostPage{Items: []domain.TelegramPost{}, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (f *fakeTelegramReader) ListMapPosts(_ context.Context, filter domain.TelegramPostFilter) (domain.TelegramMapPage, error) {
	f.mapCalls++
	f.lastFilter = filter
	return domain.TelegramMapPage{Items: []domain.TelegramMapItem{}}, nil
}

func (f *fakeTelegramReader) ListSources(context.Context) ([]domain.TelegramSource, error) {
	f.sourceCalls++
	return []domain.TelegramSource{}, nil
}

func TestTelegramRoutesRejectWhenAPIKeyUnset(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "", t.TempDir(), reader)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/telegram/sources", nil))

	if response.Code != 503 {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if reader.sourceCalls != 0 {
		t.Fatalf("repository called %d times with API key unset", reader.sourceCalls)
	}
}

func TestTelegramRoutesAcceptConfiguredAPIKey(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)

	request := httptest.NewRequest("GET", "/telegram/posts?limit=20", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if reader.postCalls != 1 {
		t.Fatalf("repository called %d times, want 1", reader.postCalls)
	}
}

func TestTelegramPostsParseFiltersAndClampLimit(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
	request := httptest.NewRequest("GET", "/telegram/posts?source=%40rent_spb&q=room&has_text=true&since=2026-10-01T00%3A00%3A00Z&until=2026-10-09T00%3A00%3A00Z&limit=200&offset=3", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	filter := reader.lastFilter
	wantSince := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wantUntil := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if filter.Source != "@rent_spb" || filter.Query != "room" || !filter.HasText || filter.Limit != 100 || filter.Offset != 3 {
		t.Fatalf("unexpected parsed filter: %+v", filter)
	}
	if filter.Since == nil || !filter.Since.Equal(wantSince) || filter.Until == nil || !filter.Until.Equal(wantUntil) {
		t.Fatalf("unexpected date filters: since=%v until=%v", filter.Since, filter.Until)
	}
}

func TestTelegramPostsParseCursor(t *testing.T) {
	wantTime := time.Date(2026, 10, 11, 6, 30, 45, 123456000, time.UTC)
	raw := fmt.Sprintf("%d:%d:%d", wantTime.UnixMicro(), int64(-100123), 456)
	filter, err := parsePostFilter(url.Values{"cursor": []string{raw}, "limit": []string{"24"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if filter.Cursor == nil || !filter.Cursor.PostedAt.Equal(wantTime) || filter.Cursor.ChannelID != -100123 || filter.Cursor.MessageID != 456 {
		t.Fatalf("unexpected cursor: %+v", filter.Cursor)
	}
	if filter.Limit != 24 || filter.Offset != 0 {
		t.Fatalf("unexpected pagination: %+v", filter)
	}
}

func TestTelegramPostsRejectCursorWithOffset(t *testing.T) {
	raw := fmt.Sprintf("%d:%d:%d", time.Now().UTC().UnixMicro(), int64(-100123), 456)
	_, err := parsePostFilter(url.Values{"cursor": []string{raw}, "offset": []string{"1"}}, true)
	if err == nil || err.Error() != "cursor_and_offset_are_mutually_exclusive" {
		t.Fatalf("err=%v, want cursor_and_offset_are_mutually_exclusive", err)
	}
}

func TestTelegramPostsCanFetchOnePostByIdentity(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
	request := httptest.NewRequest("GET", "/telegram/posts?channel_id=-100123&message_id=456&limit=1", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || reader.postCalls != 1 {
		t.Fatalf("status=%d post calls=%d body=%s", response.Code, reader.postCalls, response.Body.String())
	}
	if reader.lastFilter.ChannelID != -100123 || reader.lastFilter.MessageID != 456 || reader.lastFilter.Limit != 1 {
		t.Fatalf("unexpected post identity filter: %+v", reader.lastFilter)
	}
}

func TestTelegramPostsRejectPartialIdentityFilter(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
	request := httptest.NewRequest("GET", "/telegram/posts?channel_id=-100123", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || reader.postCalls != 0 {
		t.Fatalf("status=%d post calls=%d body=%s", response.Code, reader.postCalls, response.Body.String())
	}
}

func TestTelegramCatalogFiltersAndMapRoute(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
	request := httptest.NewRequest("GET", "/telegram/map?offer_only=true&building_type=new&rooms=4plus&price_min=30000&price_max=90000&metro=Пионерская", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || reader.mapCalls != 1 {
		t.Fatalf("status=%d map calls=%d body=%s", response.Code, reader.mapCalls, response.Body.String())
	}
	filter := reader.lastFilter
	if !filter.OfferOnly || filter.BuildingType != "new" || !filter.RoomsPlus || filter.Metro != "Пионерская" ||
		filter.PriceMin == nil || *filter.PriceMin != 30000 || filter.PriceMax == nil || *filter.PriceMax != 90000 {
		t.Fatalf("unexpected catalog filters: %+v", filter)
	}
}

func TestTelegramMapParsesBoundsAndZoom(t *testing.T) {
	filter, err := parsePostFilter(url.Values{
		"bbox": []string{"29.9,59.7,30.7,60.1"},
		"zoom": []string{"12"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if filter.MapBounds == nil || filter.MapBounds.West != 29.9 || filter.MapBounds.South != 59.7 || filter.MapBounds.East != 30.7 || filter.MapBounds.North != 60.1 {
		t.Fatalf("unexpected map bounds: %+v", filter.MapBounds)
	}
	if filter.MapZoom != 12 {
		t.Fatalf("zoom=%d, want 12", filter.MapZoom)
	}
}

func TestTelegramMapRejectsInvalidBounds(t *testing.T) {
	for _, bbox := range []string{"29.9,59.7,29.8,60.1", "29.9,91,30.7,92", "broken"} {
		if _, err := parsePostFilter(url.Values{"bbox": []string{bbox}}, false); err == nil || err.Error() != "invalid_bbox" {
			t.Fatalf("bbox=%q err=%v, want invalid_bbox", bbox, err)
		}
	}
}

func TestTelegramCatalogFiltersMultipleRoomValues(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
	request := httptest.NewRequest("GET", "/telegram/posts?rooms=2&rooms=3&rooms=4plus", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.postCalls != 1 {
		t.Fatalf("status=%d post calls=%d body=%s", response.Code, reader.postCalls, response.Body.String())
	}
	filter := reader.lastFilter
	if len(filter.Rooms) != 2 || filter.Rooms[0] != 2 || filter.Rooms[1] != 3 || !filter.RoomsPlus {
		t.Fatalf("unexpected multiple room filters: %+v", filter)
	}
}

func TestTelegramCatalogFiltersRejectInvalidValues(t *testing.T) {
	for _, query := range []string{
		"building_type=all", "rooms=5", "price_min=0", "price_max=abc", "price_min=90000&price_max=30000", "offer_only=yes",
	} {
		t.Run(query, func(t *testing.T) {
			reader := &fakeTelegramReader{}
			router := chi.NewRouter()
			RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)
			request := httptest.NewRequest("GET", "/telegram/posts?"+query, nil)
			request.Header.Set("X-API-Key", "secret-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 400 || reader.postCalls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, reader.postCalls, response.Body.String())
			}
		})
	}
}

func TestTelegramRoutesRejectInvalidAPIKey(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/telegram/posts", nil))

	if response.Code != 401 {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if reader.postCalls != 0 {
		t.Fatalf("repository called %d times without valid API key", reader.postCalls)
	}
}

func TestTelegramMediaRequiresAPIKey(t *testing.T) {
	reader := &fakeTelegramReader{}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), reader)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/telegram/media/123", nil))

	if response.Code != 401 {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestTelegramMediaServesPrivateJPEG(t *testing.T) {
	mediaDir := t.TempDir()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	jpegBytes := encoded.Bytes()
	if err := os.WriteFile(filepath.Join(mediaDir, "123.jpg"), jpegBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", mediaDir, &fakeTelegramReader{})
	request := httptest.NewRequest("GET", "/telegram/media/123", nil)
	request.Header.Set("X-API-Key", "secret-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("Content-Type = %q, want image/jpeg", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable private cache", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("ETag") == "" {
		t.Fatal("ETag was not set")
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", response.Header().Get("X-Content-Type-Options"))
	}
	if _, err := jpeg.Decode(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("response is not a valid JPEG: %v", err)
	}
	if !bytes.Equal(response.Body.Bytes(), jpegBytes) {
		t.Fatalf("response body did not match stored JPEG")
	}

	conditional := httptest.NewRequest("GET", "/telegram/media/123", nil)
	conditional.Header.Set("X-API-Key", "secret-key")
	conditional.Header.Set("If-None-Match", response.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	router.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", notModified.Code)
	}
	if notModified.Body.Len() != 0 {
		t.Fatalf("304 body length = %d, want 0", notModified.Body.Len())
	}
}

func TestTelegramMediaRejectsInvalidOrMissingIDs(t *testing.T) {
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret-key", t.TempDir(), &fakeTelegramReader{})
	for _, photoID := range []string{"0", "-5", "text", "9223372036854775808", "999"} {
		t.Run(photoID, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/telegram/media/"+photoID, nil)
			request.Header.Set("X-API-Key", "secret-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 404 {
				t.Fatalf("status = %d, want 404", response.Code)
			}
		})
	}
}
