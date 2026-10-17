package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeocodeAcceptsOnlyExactHighConfidenceSPBBuilding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/geocode/search" {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("text"); got != "Санкт-Петербург, Невский проспект, 12, Санкт-Петербург, Россия" {
			t.Errorf("unexpected geocoding text %q", got)
		}
		if r.URL.Query().Get("filter") != "countrycode:ru" || r.URL.Query().Get("format") != "json" {
			t.Errorf("missing country or response format restriction")
		}
		if r.URL.Query().Get("apiKey") != "test-key" {
			t.Errorf("fake API key was not supplied")
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{
			validResult("12", 0.99, 0.99, 0.99, 59.9343, 30.3351),
		}})
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	outcome, err := client.Geocode(context.Background(), "Санкт-Петербург, Невский проспект, 12")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "success" || outcome.Coordinate == nil {
		t.Fatalf("expected accepted coordinates, got %+v", outcome)
	}
	if outcome.Coordinate.Latitude != 59.9343 || outcome.Coordinate.Longitude != 30.3351 {
		t.Fatalf("unexpected coordinates: %+v", outcome.Coordinate)
	}
}

func TestGeocodeAcceptsExactAmenityWithBuildingLevelConfidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		result := validResult("45", 1, 1, 1, 59.9338332, 30.4018781)
		result["street"] = "Малоохтинский проспект"
		result["result_type"] = "amenity"
		result["rank"].(map[string]any)["confidence_building_level"] = 1.0
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{result}})
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	outcome, err := client.Geocode(context.Background(), "Малоохтинский проспект, 45, Санкт-Петербург")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "success" || outcome.Coordinate == nil || outcome.Coordinate.Latitude != 59.9338332 || outcome.Coordinate.Longitude != 30.4018781 {
		t.Fatalf("expected exact amenity coordinates, got %+v", outcome)
	}
}

func TestGeocodeRejectsWrongHouseConfidenceCityAndBounds(t *testing.T) {
	amenityWithoutBuildingConfidence := validResult("12", .99, .99, .99, 59.9343, 30.3351)
	amenityWithoutBuildingConfidence["result_type"] = "amenity"
	amenityWithoutBuildingConfidence["rank"].(map[string]any)["confidence_building_level"] = 0.0
	tests := []struct {
		name   string
		result map[string]any
	}{
		{"wrong house", validResult("14", .99, .99, .99, 59.9343, 30.3351)},
		{"low confidence", validResult("12", .8, .99, .99, 59.9343, 30.3351)},
		{"wrong city", modifyResult(modifyResult(validResult("12", .99, .99, .99, 59.9343, 30.3351), "city", "Москва"), "state", "Московская область")},
		{"wrong country", modifyResult(validResult("12", .99, .99, .99, 59.9343, 30.3351), "country_code", "fi")},
		{"wrong street", modifyResult(validResult("12", .99, .99, .99, 59.9343, 30.3351), "street", "Литейный проспект")},
		{"street match only", modifyResult(validResult("12", .99, .99, .99, 59.9343, 30.3351), "result_type", "street")},
		{"amenity without building confidence", amenityWithoutBuildingConfidence},
		{"outside SPB", validResult("12", .99, .99, .99, 55.7558, 37.6173)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := validateCandidates("Санкт-Петербург, Невский проспект, 12", "12", geoapifyResponse{
				Results: []geoapifyFeature{decodeResult(t, test.result)},
			})
			if outcome.Status != "rejected" || outcome.Coordinate != nil {
				t.Fatalf("expected rejection, got %+v", outcome)
			}
		})
	}
}

func TestGeocodeDoesNotCallAPIWithoutExplicitHouseNumber(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	client := testClient(t, server.URL)

	outcome, err := client.Geocode(context.Background(), "Санкт-Петербург, Невский проспект")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "rejected" || calls.Load() != 0 {
		t.Fatalf("expected local rejection without API call; outcome=%+v calls=%d", outcome, calls.Load())
	}
}

func TestGeocodeRetries429And5xx(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call <= 2 {
			w.WriteHeader([]int{http.StatusTooManyRequests, http.StatusBadGateway}[call-1])
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{
			validResult("12", .99, .99, .99, 59.9343, 30.3351),
		}})
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	client.retries = 3
	client.backoff = func(int) time.Duration { return 0 }

	outcome, err := client.Geocode(context.Background(), "Невский проспект, 12")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "success" || calls.Load() != 3 {
		t.Fatalf("expected 3 attempts and success, outcome=%+v calls=%d", outcome, calls.Load())
	}
}

func TestGeocodeNeverLeaksAPIKeyInTransportErrors(t *testing.T) {
	client := newClient("do-not-log-this-key", "http://127.0.0.1:1/path", &http.Client{Timeout: time.Millisecond}, 1)
	outcome, err := client.Geocode(context.Background(), "Невский проспект, 12")
	if err == nil || outcome.Status != "" {
		t.Fatalf("expected transport error, outcome=%+v err=%v", outcome, err)
	}
	if strings.Contains(err.Error(), "do-not-log-this-key") {
		t.Fatalf("transport error leaks API key: %v", err)
	}
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	if _, err := NewClient(" \t "); err == nil || !strings.Contains(err.Error(), "GEOAPIFY_API_KEY") {
		t.Fatalf("expected required key error, got %v", err)
	}
}

func TestExtractHouseAndStreet(t *testing.T) {
	tests := []struct {
		address string
		house   string
		street  string
	}{
		{"Санкт-Петербург, ул. Гоголя, д. 10 к. 2", "10к2", "гоголя"},
		{"Невский проспект, 12, Санкт-Петербург", "12", "невский"},
		{"ул. Пушкина, дом 14", "14", "пушкина"},
		{"1-й Предпортовый проезд, 1с1", "1с1", "1йпредпортовыйпроезд"},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			house, ok := extractHouseNumber(test.address)
			if !ok || house != test.house {
				t.Fatalf("house=%q ok=%t, expected %q", house, ok, test.house)
			}
			if street := normalizeStreet(extractStreet(test.address)); street != test.street {
				t.Fatalf("street=%q, expected %q", street, test.street)
			}
		})
	}
}

func TestBuildingSuffixSIsExactAndDoesNotMatchPlainHouse(t *testing.T) {
	addressWithBuilding := "1-й Предпортовый проезд, 1с1"
	house, ok := extractHouseNumber(addressWithBuilding)
	if !ok || house != "1с1" {
		t.Fatalf("extractHouseNumber = %q, %t; want 1с1, true", house, ok)
	}
	if normalized, ok := normalizeHouseNumber("1 с1"); !ok || normalized != "1с1" {
		t.Fatalf("normalizeHouseNumber = %q, %t; want 1с1, true", normalized, ok)
	}

	response := geoapifyResponse{Results: []geoapifyFeature{{
		HouseNumber: "1 с1", Street: "1-й Предпортовый проезд",
		City: "Санкт-Петербург", State: "Санкт-Петербург", CountryCode: "ru",
		Latitude: 59.85, Longitude: 30.25, ResultType: "building",
		Rank: geoapifyRank{Confidence: 1, CityConfidence: 1, StreetConfidence: 1, BuildingConfidence: 1},
	}}}
	accepted := validateCandidates(addressWithBuilding, house, response)
	if accepted.Status != "success" || accepted.Coordinate == nil {
		t.Fatalf("exact 1с1 building should be accepted, got %+v", accepted)
	}

	plainHouse := validateCandidates("1-й Предпортовый проезд, 1", "1", response)
	if plainHouse.Status != "rejected" || plainHouse.Coordinate != nil {
		t.Fatalf("plain house 1 must not match provider's 1с1 building, got %+v", plainHouse)
	}
}

func testClient(t *testing.T, endpoint string) *GeoapifyClient {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/v1/geocode/search"
	client := newClient("test-key", u.String(), &http.Client{Timeout: time.Second}, 3)
	client.backoff = func(int) time.Duration { return 0 }
	return client
}

func validResult(house string, confidence, cityConfidence, streetConfidence, lat, lon float64) map[string]any {
	return map[string]any{
		"housenumber":  house,
		"street":       "Невский проспект",
		"city":         "Санкт-Петербург",
		"state":        "Санкт-Петербург",
		"country_code": "ru",
		"lat":          lat,
		"lon":          lon,
		"result_type":  "building",
		"rank": map[string]any{
			"confidence":                confidence,
			"confidence_city_level":     cityConfidence,
			"confidence_street_level":   streetConfidence,
			"confidence_building_level": .99,
		},
	}
}

func modifyResult(result map[string]any, key string, value any) map[string]any {
	result[key] = value
	return result
}

func decodeResult(t *testing.T, value map[string]any) geoapifyFeature {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result geoapifyFeature
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDailyLimitErrorIsNotCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	client.reserve = func(context.Context) error { return ErrDailyLimit }
	_, err := client.Geocode(context.Background(), "Невский проспект, 12")
	if !errors.Is(err, ErrDailyLimit) || calls.Load() != 0 {
		t.Fatalf("expected daily limit before request, calls=%d err=%v", calls.Load(), err)
	}
}

func TestCandidateMustContainStreetAndHouseNumber(t *testing.T) {
	result := validResult("12", .99, .99, .99, 59.9343, 30.3351)
	feature := decodeResult(t, result)
	if validCandidate("Санкт-Петербург, 12", "12", feature) {
		t.Fatal("candidate accepted without a street name in source address")
	}
	if validCandidate("Санкт-Петербург, Невский проспект, 12", "11", feature) {
		t.Fatal("candidate accepted with a different requested house number")
	}
}

func TestQueryContainsOnlyAddressAndContext(t *testing.T) {
	secretMarker := "private-full-post-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if strings.Contains(query.Encode(), secretMarker) {
			t.Error("full post body was included in geocoding request")
		}
		if query.Get("text") != "Невский проспект, 12, Санкт-Петербург, Россия" {
			t.Errorf("unexpected text parameter: %s", query.Get("text"))
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	if _, err := client.Geocode(context.Background(), "Невский проспект, 12"); err != nil {
		t.Fatal(fmt.Errorf("geocode: %w", err))
	}
}
