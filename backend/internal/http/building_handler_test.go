package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"spb-rent/backend/internal/buildings"
)

type fakeBuildingReader struct {
	info    *buildings.Info
	calls   int
	channel int64
	message int
}

func (f *fakeBuildingReader) GetForPost(_ context.Context, channelID int64, messageID int) (*buildings.Info, error) {
	f.calls++
	f.channel = channelID
	f.message = messageID
	return f.info, nil
}

func TestBuildingRouteRequiresAPIKey(t *testing.T) {
	reader := &fakeBuildingReader{}
	router := chi.NewRouter()
	RegisterBuildingRoutes(router, "secret", reader)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/telegram/building/123/456", nil))
	if response.Code != http.StatusUnauthorized || reader.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, reader.calls)
	}
}

func TestBuildingRouteReturnsBuilding(t *testing.T) {
	year := 1971
	reader := &fakeBuildingReader{info: &buildings.Info{Source: buildings.SourceFRT, HouseGUID: "guid", Address: "addr", BuiltYear: &year}}
	router := chi.NewRouter()
	RegisterBuildingRoutes(router, "secret", reader)
	request := httptest.NewRequest(http.MethodGet, "/telegram/building/123/456", nil)
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.calls != 1 || reader.channel != 123 || reader.message != 456 {
		t.Fatalf("status=%d calls=%d", response.Code, reader.calls)
	}
	var info buildings.Info
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.BuiltYear == nil || *info.BuiltYear != 1971 {
		t.Fatalf("unexpected response: %+v", info)
	}
}

func TestBuildingRouteReturnsNullWhenUnmatched(t *testing.T) {
	router := chi.NewRouter()
	RegisterBuildingRoutes(router, "secret", &fakeBuildingReader{})
	request := httptest.NewRequest(http.MethodGet, "/telegram/building/123/456", nil)
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "null\n" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
