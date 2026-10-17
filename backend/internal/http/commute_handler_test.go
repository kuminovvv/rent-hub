package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const validCommuteJSON = `{"origin":{"latitude":59.93,"longitude":30.33},"destination":{"latitude":60.01,"longitude":30.29},"mode":"approximated_transit"}`

func TestCommuteRouteUsesGeoapifyAndReturnsSummary(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/routing" || r.URL.Query().Get("mode") != "approximated_transit" || r.URL.Query().Get("format") != "json" {
			t.Errorf("unexpected Geoapify request: %s", r.URL.String())
		}
		if got := r.URL.Query().Get("waypoints"); got != "59.930000,30.330000|60.010000,30.290000" {
			t.Errorf("waypoints = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{
			"distance": 12400, "time": 2520,
			"legs": []any{map[string]any{"steps": []any{
				map[string]any{"time": 300, "instruction": map[string]string{"type": "TransitConnectionStart"}},
				map[string]any{"time": 0, "instruction": map[string]string{"type": "TransitTransfer"}},
			}}},
		}}})
	}))
	defer provider.Close()
	handler := newCommuteHandler("test-key", 5, 5, provider.URL+"/v1/routing", provider.URL+"/v1/geocode/search", provider.Client())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/commute", strings.NewReader(validCommuteJSON))
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	handler.HandleRoute(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got commuteResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DistanceM != 12400 || got.DurationS != 2520 || got.Transfers == nil || *got.Transfers != 1 || got.WalkingS == nil || *got.WalkingS != 300 || !got.Approximate {
		t.Fatalf("unexpected route summary: %+v", got)
	}
}

func TestCommuteGeocodeUsesGeoapify(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/geocode/search" || r.URL.Query().Get("apiKey") != "test-key" {
			t.Errorf("unexpected geocode request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{
			"housenumber": "1", "street": "Невский проспект", "city": "Санкт-Петербург", "country_code": "ru", "result_type": "building", "lat": 59.9343, "lon": 30.3351,
			"rank": map[string]float64{"confidence": 0.99, "confidence_city_level": 0.99, "confidence_street_level": 0.99, "confidence_building_level": 0.99},
		}}})
	}))
	defer provider.Close()
	handler := newCommuteHandler("test-key", 5, 5, provider.URL+"/v1/routing", provider.URL+"/v1/geocode/search", provider.Client())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/commute/geocode", strings.NewReader(`{"address":"Невский проспект, 1"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	handler.HandleGeocode(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got geocodeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Address != "Невский проспект, 1" || got.Latitude != 59.9343 || got.Longitude != 30.3351 {
		t.Fatalf("unexpected geocode result: %+v", got)
	}
}

func TestCommuteAutocompleteReturnsExactSaintPetersburgAddress(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/geocode/autocomplete" || r.URL.Query().Get("apiKey") != "test-key" || r.URL.Query().Get("filter") != "countrycode:ru" {
			t.Errorf("unexpected autocomplete request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
			map[string]any{"formatted": "Бизнес-центр Eightedges, Малоохтинский проспект 45, Санкт-Петербург, Россия", "housenumber": "45", "street": "Малоохтинский проспект", "city": "Санкт-Петербург", "country_code": "ru", "result_type": "amenity", "lat": 59.93, "lon": 30.42},
			map[string]any{"formatted": "Ленинградская улица, 45, Москва, Россия", "housenumber": "45", "city": "Москва", "country_code": "ru", "result_type": "building", "lat": 55.75, "lon": 37.61},
			map[string]any{"housenumber": "46", "street": "Малоохтинский проспект", "city": "Санкт-Петербург", "country_code": "ru", "result_type": "building", "lat": 59.93, "lon": 30.42},
			map[string]any{"housenumber": "45", "street": "Невский проспект", "city": "Санкт-Петербург", "country_code": "ru", "result_type": "building", "lat": 59.93, "lon": 30.33},
			map[string]any{"street": "Малоохтинский проспект", "city": "Санкт-Петербург", "country_code": "ru", "result_type": "street", "lat": 59.93, "lon": 30.42},
		}})
	}))
	defer provider.Close()
	handler := newCommuteHandler("test-key", 5, 5, provider.URL+"/v1/routing", provider.URL+"/v1/geocode/search", provider.Client())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/commute/autocomplete", strings.NewReader(`{"query":"Малоохтинский просп., 45, Санкт-Петербург"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	handler.HandleAutocomplete(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got struct {
		Suggestions []addressSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Suggestions) != 1 || got.Suggestions[0].Address != "Малоохтинский проспект, 45, Санкт-Петербург" {
		t.Fatalf("unexpected suggestions: %+v", got.Suggestions)
	}
	if strings.Contains(recorder.Body.String(), "test-key") {
		t.Fatal("response leaked Geoapify API key")
	}
}

func TestCommuteRejectsWhenGeoapifyKeyMissing(t *testing.T) {
	handler := NewCommuteHandler("", 5, 5)
	req := httptest.NewRequest(http.MethodPost, "/commute", strings.NewReader(validCommuteJSON))
	recorder := httptest.NewRecorder()
	handler.HandleRoute(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", recorder.Code)
	}
}

func TestCommuteRejectsCoordinatesOutsideSaintPetersburg(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer provider.Close()
	handler := newCommuteHandler("test-key", 5, 5, provider.URL, provider.URL, provider.Client())
	req := httptest.NewRequest(http.MethodPost, "/commute", strings.NewReader(strings.Replace(validCommuteJSON, `"latitude":59.93`, `"latitude":58.93`, 1)))
	recorder := httptest.NewRecorder()
	handler.HandleRoute(recorder, req)
	if recorder.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("status=%d provider calls=%d", recorder.Code, calls)
	}
}

func TestCommuteDailyRateCap(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"distance": 1000, "time": 300}}})
	}))
	defer provider.Close()
	handler := newCommuteHandler("test-key", 1, 10, provider.URL, provider.URL, provider.Client())
	for i, wantStatus := range []int{http.StatusOK, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodPost, "/commute", strings.NewReader(validCommuteJSON))
		req.RemoteAddr = "127.0.0.1:1234"
		recorder := httptest.NewRecorder()
		handler.HandleRoute(recorder, req)
		if recorder.Code != wantStatus {
			t.Fatalf("request %d status=%d, want %d; body=%s", i+1, recorder.Code, wantStatus, recorder.Body.String())
		}
	}
}

func TestCommuteEndpointsRequireTelegramAPIKey(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"distance": 1000, "time": 300}}})
	}))
	defer provider.Close()
	router := chi.NewRouter()
	RegisterTelegramRoutes(router, "secret", t.TempDir(), &fakeTelegramReader{}, newCommuteHandler("geo-key", 5, 5, provider.URL, provider.URL, provider.Client()))
	req := httptest.NewRequest(http.MethodPost, "/telegram/commute", strings.NewReader(validCommuteJSON))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/telegram/commute", strings.NewReader(validCommuteJSON))
	req.Header.Set("X-API-Key", "secret")
	req.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorized endpoint status=%d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
}
