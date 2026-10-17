package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"spb-rent/backend/internal/geocoding"
)

const (
	geoapifyRoutingEndpoint = "https://api.geoapify.com/v1/routing"
	geoapifyGeocodeEndpoint = "https://api.geoapify.com/v1/geocode/search"
	spbMinLatitude          = 59.55
	spbMaxLatitude          = 60.35
	spbMinLongitude         = 29.25
	spbMaxLongitude         = 31.35
)

type commutePoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type commuteRequest struct {
	Origin      commutePoint `json:"origin"`
	Destination commutePoint `json:"destination"`
	Mode        string       `json:"mode"`
}

type commuteResponse struct {
	DistanceM   float64  `json:"distance_m"`
	DurationS   float64  `json:"duration_s"`
	Transfers   *int     `json:"transfers"`
	WalkingS    *float64 `json:"walking_s"`
	Approximate bool     `json:"approximate"`
}

type geocodeRequest struct {
	Address string `json:"address"`
}

type geocodeResponse struct {
	Address   string  `json:"address"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type autocompleteRequest struct {
	Query string `json:"query"`
}

type addressSuggestion struct {
	Address string `json:"address"`
}

type commuteBudget struct {
	mu           sync.Mutex
	dailyLimit   int
	minuteLimit  int
	day          string
	dailyCount   int
	clientCounts map[string]minuteCounter
}

type minuteCounter struct {
	start time.Time
	count int
}

func newCommuteBudget(dailyLimit, minuteLimit int) *commuteBudget {
	return &commuteBudget{dailyLimit: dailyLimit, minuteLimit: minuteLimit, clientCounts: make(map[string]minuteCounter)}
}

func (b *commuteBudget) reserve(client string, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	day := now.UTC().Format("2006-01-02")
	if b.day != day {
		b.day, b.dailyCount = day, 0
	}
	if b.dailyCount >= b.dailyLimit {
		return errDailyCommuteLimit
	}
	counter := b.clientCounts[client]
	if counter.start.IsZero() || now.Sub(counter.start) >= time.Minute || now.Before(counter.start) {
		counter = minuteCounter{start: now}
	}
	if counter.count >= b.minuteLimit {
		return errMinuteCommuteLimit
	}
	counter.count++
	b.clientCounts[client] = counter
	b.dailyCount++
	return nil
}

var (
	errDailyCommuteLimit  = errors.New("daily Geoapify request budget exhausted")
	errMinuteCommuteLimit = errors.New("Geoapify per-client rate limit exceeded")
	autocompleteHouseRE   = regexp.MustCompile(`(?i)^(?:д(?:ом)?\.?\s*)?(\d+[a-zа-яё]?(?:\s*(?:к(?:орп(?:ус)?)?|стр(?:оение)?|с|лит(?:ера)?)\.?\s*\d+[a-zа-яё]?)?(?:/\d+[a-zа-яё]?)?)$`)
)

type CommuteHandler struct {
	apiKey        string
	routingURL    string
	geocodeURL    string
	httpClient    *http.Client
	geocoder      *geocoding.GeoapifyClient
	budget        *commuteBudget
	now           func() time.Time
	globalReserve func(context.Context) error
}

func NewCommuteHandler(apiKey string, dailyLimit, minuteLimit int) *CommuteHandler {
	return newCommuteHandler(apiKey, dailyLimit, minuteLimit, geoapifyRoutingEndpoint, geoapifyGeocodeEndpoint, &http.Client{Timeout: 5 * time.Second})
}

func newCommuteHandler(apiKey string, dailyLimit, minuteLimit int, routingURL, geocodeURL string, client *http.Client) *CommuteHandler {
	if dailyLimit < 1 {
		dailyLimit = 50
	}
	if minuteLimit < 1 {
		minuteLimit = 10
	}
	h := &CommuteHandler{
		apiKey: strings.TrimSpace(apiKey), routingURL: routingURL, geocodeURL: geocodeURL,
		httpClient: client, budget: newCommuteBudget(dailyLimit, minuteLimit), now: time.Now,
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if h.apiKey != "" {
		if geocoder, err := geocoding.NewClientWithEndpoint(h.apiKey, geocodeURL, client); err == nil {
			geocoder.SetRequestReservation(func(ctx context.Context) error {
				return h.reserve(ctx, requestClientKey(ctx))
			})
			h.geocoder = geocoder
		}
	}
	return h
}

func (h *CommuteHandler) SetGlobalReservation(reserve func(context.Context) error) {
	h.globalReserve = reserve
}

func (h *CommuteHandler) reserve(ctx context.Context, client string) error {
	if err := h.budget.reserve(client, h.now()); err != nil {
		return err
	}
	if h.globalReserve != nil {
		if err := h.globalReserve(ctx); err != nil {
			return fmt.Errorf("reserve shared Geoapify budget: %w", err)
		}
	}
	return nil
}

// HandleRoute proxies a bounded public-transit route calculation to Geoapify.
func (h *CommuteHandler) HandleRoute(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.apiKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
		return
	}
	var input commuteRequest
	if err := decodeSmallJSON(r, &input); err != nil || input.Mode != "approximated_transit" || !validCommutePoint(input.Origin) || !validCommutePoint(input.Destination) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_commute_request"})
		return
	}
	if err := h.reserve(r.Context(), clientIdentity(r)); err != nil {
		writeBudgetError(w, err)
		return
	}

	query := url.Values{}
	query.Set("waypoints", fmt.Sprintf("%.6f,%.6f|%.6f,%.6f", input.Origin.Latitude, input.Origin.Longitude, input.Destination.Latitude, input.Destination.Longitude))
	query.Set("mode", "approximated_transit")
	query.Set("format", "json")
	query.Set("details", "instruction_details")
	query.Set("lang", "ru")
	query.Set("apiKey", h.apiKey)
	u, err := url.Parse(h.routingURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_unavailable"})
		return
	}
	u.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_unavailable"})
		return
	}
	request.Header.Set("Accept", "application/json")
	response, err := h.httpClient.Do(request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_unavailable"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route_not_found"})
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_unavailable"})
		return
	}
	var provider struct {
		Results []struct {
			Distance float64 `json:"distance"`
			Time     float64 `json:"time"`
			Legs     []struct {
				Steps []struct {
					Time        float64 `json:"time"`
					Instruction struct {
						Type string `json:"type"`
					} `json:"instruction"`
				} `json:"steps"`
			} `json:"legs"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 512*1024)).Decode(&provider); err != nil || len(provider.Results) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_invalid_response"})
		return
	}
	result := provider.Results[0]
	if !finitePositive(result.Distance) || !finitePositive(result.Time) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "route_provider_invalid_response"})
		return
	}
	transfers := 0
	walking := 0.0
	hasWalking := false
	hasTransitDetails := false
	for _, leg := range result.Legs {
		for _, step := range leg.Steps {
			if strings.HasPrefix(step.Instruction.Type, "Transit") {
				hasTransitDetails = true
			}
			switch step.Instruction.Type {
			case "TransitTransfer":
				transfers++
			case "TransitConnectionStart", "TransitConnectionTransfer", "TransitConnectionDestination", "PostTransitConnectionDestination":
				if step.Instruction.Type == "TransitConnectionTransfer" {
					transfers++
				}
				if finiteNonNegative(step.Time) {
					walking += step.Time
					hasWalking = true
				}
			}
		}
	}
	var walkingValue *float64
	if hasWalking {
		walkingValue = &walking
	}
	var transferValue *int
	if hasTransitDetails {
		transferValue = &transfers
	}
	writeJSON(w, http.StatusOK, commuteResponse{DistanceM: result.Distance, DurationS: result.Time, Transfers: transferValue, WalkingS: walkingValue, Approximate: true})
}

// HandleGeocode resolves a user-entered address only after an explicit request.
func (h *CommuteHandler) HandleGeocode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.apiKey == "" || h.geocoder == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
		return
	}
	var input geocodeRequest
	if err := decodeSmallJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_geocode_request"})
		return
	}
	input.Address = strings.TrimSpace(input.Address)
	if input.Address == "" || len([]rune(input.Address)) > 250 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_geocode_request"})
		return
	}
	ctx := context.WithValue(r.Context(), clientKeyContext{}, clientIdentity(r))
	outcome, err := h.geocoder.Geocode(ctx, input.Address)
	if err != nil {
		if errors.Is(err, errDailyCommuteLimit) || errors.Is(err, errMinuteCommuteLimit) || errors.Is(err, geocoding.ErrDailyLimit) {
			writeBudgetError(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "geocode_provider_unavailable"})
		return
	}
	if outcome.Status != "success" || outcome.Coordinate == nil || !validCommutePoint(commutePoint{Latitude: outcome.Coordinate.Latitude, Longitude: outcome.Coordinate.Longitude}) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "address_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, geocodeResponse{Address: input.Address, Latitude: outcome.Coordinate.Latitude, Longitude: outcome.Coordinate.Longitude})
}

// HandleAutocomplete proxies a small, rate-limited set of Saint Petersburg building suggestions.
func (h *CommuteHandler) HandleAutocomplete(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.apiKey == "" || h.httpClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_not_configured"})
		return
	}
	var input autocompleteRequest
	if err := decodeSmallJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_autocomplete_request"})
		return
	}
	input.Query = strings.TrimSpace(input.Query)
	if len([]rune(input.Query)) < 4 || len([]rune(input.Query)) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_autocomplete_request"})
		return
	}
	ctx := context.WithValue(r.Context(), clientKeyContext{}, clientIdentity(r))
	if err := h.reserve(ctx, requestClientKey(ctx)); err != nil {
		writeBudgetError(w, err)
		return
	}

	endpoint, err := url.Parse(h.geocodeURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "autocomplete_provider_unavailable"})
		return
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/search") + "/autocomplete"
	query := url.Values{}
	query.Set("text", input.Query+", Санкт-Петербург, Россия")
	query.Set("filter", "countrycode:ru")
	query.Set("format", "json")
	query.Set("lang", "ru")
	query.Set("limit", "5")
	query.Set("apiKey", h.apiKey)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "autocomplete_provider_unavailable"})
		return
	}
	request.Header.Set("Accept", "application/json")
	response, err := h.httpClient.Do(request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "autocomplete_provider_unavailable"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "geoapify_rate_limit"})
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "autocomplete_provider_unavailable"})
		return
	}
	var payload struct {
		Results []struct {
			HouseNumber string  `json:"housenumber"`
			Street      string  `json:"street"`
			City        string  `json:"city"`
			State       string  `json:"state"`
			CountryCode string  `json:"country_code"`
			ResultType  string  `json:"result_type"`
			Latitude    float64 `json:"lat"`
			Longitude   float64 `json:"lon"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "autocomplete_provider_unavailable"})
		return
	}
	suggestions := make([]addressSuggestion, 0, len(payload.Results))
	expectedHouse := autocompleteHouseNumber(input.Query)
	expectedStreet := autocompleteQueryStreet(input.Query)
	for _, result := range payload.Results {
		candidateHouse := normalizeAutocompleteHouse(result.HouseNumber)
		candidateStreet := normalizeAutocompleteStreet(result.Street)
		validType := strings.EqualFold(result.ResultType, "building") || strings.EqualFold(result.ResultType, "amenity")
		if candidateHouse == "" || (expectedHouse != "" && candidateHouse != expectedHouse) ||
			(expectedStreet != "" && candidateStreet != expectedStreet) || candidateStreet == "" ||
			!strings.EqualFold(result.CountryCode, "ru") || !isSPBAddress(result.City, result.State) || !validType ||
			!validCommutePoint(commutePoint{Latitude: result.Latitude, Longitude: result.Longitude}) {
			continue
		}
		suggestions = append(suggestions, addressSuggestion{Address: strings.TrimSpace(result.Street) + ", " + strings.TrimSpace(result.HouseNumber) + ", Санкт-Петербург"})
		if len(suggestions) == 5 {
			break
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string][]addressSuggestion{"suggestions": suggestions})
}

func autocompleteHouseNumber(query string) string {
	parts := strings.Split(query, ",")
	for index := len(parts) - 1; index >= 0; index-- {
		match := autocompleteHouseRE.FindStringSubmatch(strings.TrimSpace(parts[index]))
		if len(match) == 2 {
			return normalizeAutocompleteHouse(match[1])
		}
	}
	return ""
}

func normalizeAutocompleteHouse(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "ё", "е")
	return strings.Join(strings.Fields(value), "")
}

func autocompleteQueryStreet(query string) string {
	parts := strings.Split(query, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || isSPBAddress(part, "") || strings.EqualFold(part, "Россия") || strings.EqualFold(part, "Russia") || autocompleteHouseRE.MatchString(part) {
			continue
		}
		return normalizeAutocompleteStreet(part)
	}
	return ""
}

func normalizeAutocompleteStreet(value string) string {
	ignored := map[string]struct{}{
		"улица": {}, "ул": {}, "проспект": {}, "просп": {}, "пр": {}, "пркт": {},
		"набережная": {}, "наб": {}, "переулок": {}, "пер": {}, "шоссе": {}, "ш": {},
		"линия": {}, "лин": {}, "бульвар": {}, "бул": {}, "площадь": {}, "пл": {},
	}
	var normalized strings.Builder
	for _, word := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		word = strings.ReplaceAll(word, "ё", "е")
		if _, skip := ignored[word]; !skip {
			normalized.WriteString(word)
		}
	}
	return normalized.String()
}

func isSPBAddress(city, state string) bool {
	for _, value := range []string{city, state} {
		compact := strings.NewReplacer("-", "", " ", "", "ё", "е").Replace(strings.ToLower(value))
		if strings.Contains(compact, "санктпетербург") || compact == "saintpetersburg" || compact == "stpetersburg" {
			return true
		}
	}
	return false
}

type clientKeyContext struct{}

func requestClientKey(ctx context.Context) string {
	if value, ok := ctx.Value(clientKeyContext{}).(string); ok && value != "" {
		return value
	}
	return "unknown"
}

func clientIdentity(r *http.Request) string {
	if r.RemoteAddr == "" {
		return "unknown"
	}
	return r.RemoteAddr
}

func decodeSmallJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func validCommutePoint(point commutePoint) bool {
	return finite(point.Latitude) && finite(point.Longitude) && point.Latitude >= spbMinLatitude && point.Latitude <= spbMaxLatitude && point.Longitude >= spbMinLongitude && point.Longitude <= spbMaxLongitude
}

func finite(value float64) bool            { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finitePositive(value float64) bool    { return finite(value) && value > 0 }
func finiteNonNegative(value float64) bool { return finite(value) && value >= 0 }

func writeBudgetError(w http.ResponseWriter, err error) {
	if errors.Is(err, errMinuteCommuteLimit) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limit_exceeded"})
		return
	}
	if errors.Is(err, errDailyCommuteLimit) || errors.Is(err, geocoding.ErrDailyLimit) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "daily_geoapify_limit_reached"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "geoapify_budget_unavailable"})
}
