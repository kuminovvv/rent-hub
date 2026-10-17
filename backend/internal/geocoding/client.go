package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const defaultEndpoint = "https://api.geoapify.com/v1/geocode/search"

var (
	ErrPermanentProvider = errors.New("Geoapify permanent provider error")
	errUnauthorized      = fmt.Errorf("%w: authentication failed", ErrPermanentProvider)
	ErrDailyLimit        = errors.New("Geoapify daily request budget exhausted")
	houseTokenRE         = regexp.MustCompile(`(?i)^(?:д(?:ом)?\.?\s*)?(\d+[a-zа-яё]?(?:\s*(?:к(?:орп(?:ус)?)?|стр(?:оение)?|с|лит(?:ера)?)\.?\s*\d+[a-zа-яё]?)?(?:/\d+[a-zа-яё]?)?)$`)
	houseSuffixRE        = regexp.MustCompile(`(?i)(?:^|[,;\s])(?:д(?:ом)?\.?\s*)?(\d+[a-zа-яё]?(?:\s*(?:к(?:орп(?:ус)?)?|стр(?:оение)?|с|лит(?:ера)?)\.?\s*\d+[a-zа-яё]?)?(?:/\d+[a-zа-яё]?)?)\s*$`)
)

// Coordinate is a building coordinate that passed the local confidence and
// Saint Petersburg bounds checks.
type Coordinate struct {
	Latitude   float64
	Longitude  float64
	Confidence float64
}

// Outcome distinguishes a definitive unsuccessful lookup (which can be
// cached) from a transient API error (which must remain retryable).
type Outcome struct {
	Coordinate *Coordinate
	Status     string // success, not_found, or rejected
}

type GeoapifyClient struct {
	apiKey   string
	http     *http.Client
	endpoint string
	retries  int
	backoff  func(int) time.Duration
	reserve  func(context.Context) error
}

func NewClient(apiKey string) (*GeoapifyClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("GEOAPIFY_API_KEY is required")
	}
	return newClient(apiKey, defaultEndpoint, &http.Client{Timeout: 12 * time.Second}, 3), nil
}

// NewClientWithEndpoint creates a geocoding client with an alternate endpoint
// and HTTP client. The production constructor should be preferred; this is
// useful for controlled provider-compatible deployments and httptest mocks.
func NewClientWithEndpoint(apiKey, endpoint string, client *http.Client) (*GeoapifyClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("GEOAPIFY_API_KEY is required")
	}
	if strings.TrimSpace(endpoint) == "" || client == nil {
		return nil, errors.New("Geoapify endpoint and HTTP client are required")
	}
	return newClient(apiKey, endpoint, client, 1), nil
}

func newClient(apiKey, endpoint string, client *http.Client, retries int) *GeoapifyClient {
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		// The API key is in the query string; never forward it to a redirect host.
		return http.ErrUseLastResponse
	}
	return &GeoapifyClient{
		apiKey: apiKey, http: client, endpoint: endpoint, retries: retries,
		backoff: jitteredBackoff,
	}
}

func jitteredBackoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	// Full clients can be restarted simultaneously after an outage; adding up
	// to 25% jitter avoids a thundering herd while keeping retries bounded.
	jitter := time.Duration(rand.Int64N(max(int64(base/4), 1)))
	return base + jitter
}

// SetRequestReservation installs a persistent request-budget gate. The gate is
// called immediately before every outbound attempt, including retries.
func (c *GeoapifyClient) SetRequestReservation(reserve func(context.Context) error) {
	c.reserve = reserve
}

// Geocode sends only the extracted address plus the user's requested city and
// country context. It never receives or forwards the source post body.
func (c *GeoapifyClient) Geocode(ctx context.Context, address string) (Outcome, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return Outcome{Status: "rejected"}, nil
	}
	expectedHouse, ok := extractHouseNumber(address)
	if !ok {
		return Outcome{Status: "rejected"}, nil
	}

	query := url.Values{}
	query.Set("text", address+", Санкт-Петербург, Россия")
	query.Set("filter", "countrycode:ru")
	query.Set("format", "json")
	query.Set("lang", "ru")
	query.Set("limit", "5")
	query.Set("apiKey", c.apiKey)
	endpoint, err := url.Parse(c.endpoint)
	if err != nil {
		return Outcome{}, fmt.Errorf("parse Geoapify endpoint: %w", err)
	}
	endpoint.RawQuery = query.Encode()

	var response geoapifyResponse
	for attempt := 0; attempt < c.retries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return Outcome{}, fmt.Errorf("create Geoapify request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		if c.reserve != nil {
			if err := c.reserve(ctx); err != nil {
				return Outcome{}, err
			}
		}
		result, err := c.http.Do(request)
		if err != nil {
			if attempt+1 < c.retries && ctx.Err() == nil {
				if err := wait(ctx, c.backoff(attempt)); err != nil {
					return Outcome{}, err
				}
				continue
			}
			// net/http errors include the request URL, which contains the API key.
			return Outcome{}, errors.New("Geoapify request failed after retries")
		}

		if result.StatusCode == http.StatusTooManyRequests || result.StatusCode >= 500 {
			delay := retryDelay(result.Header.Get("Retry-After"), c.backoff(attempt))
			_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 16*1024))
			result.Body.Close()
			if attempt+1 < c.retries {
				if err := wait(ctx, delay); err != nil {
					return Outcome{}, err
				}
				continue
			}
			return Outcome{}, fmt.Errorf("Geoapify temporary HTTP status %d after retries", result.StatusCode)
		}
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Body.Close()
			return Outcome{}, errUnauthorized
		}
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			result.Body.Close()
			return Outcome{}, fmt.Errorf("%w: HTTP status %d", ErrPermanentProvider, result.StatusCode)
		}
		decodeErr := json.NewDecoder(io.LimitReader(result.Body, 1<<20)).Decode(&response)
		closeErr := result.Body.Close()
		if decodeErr != nil {
			return Outcome{}, fmt.Errorf("decode Geoapify response: %w", decodeErr)
		}
		if closeErr != nil {
			return Outcome{}, fmt.Errorf("close Geoapify response: %w", closeErr)
		}
		return validateCandidates(address, expectedHouse, response), nil
	}
	return Outcome{}, errors.New("Geoapify retry loop exhausted")
}

type geoapifyResponse struct {
	Results []geoapifyFeature `json:"results"`
}

type geoapifyFeature struct {
	HouseNumber string       `json:"housenumber"`
	Street      string       `json:"street"`
	City        string       `json:"city"`
	State       string       `json:"state"`
	CountryCode string       `json:"country_code"`
	Latitude    float64      `json:"lat"`
	Longitude   float64      `json:"lon"`
	ResultType  string       `json:"result_type"`
	Rank        geoapifyRank `json:"rank"`
}

type geoapifyRank struct {
	Confidence         float64 `json:"confidence"`
	CityConfidence     float64 `json:"confidence_city_level"`
	StreetConfidence   float64 `json:"confidence_street_level"`
	BuildingConfidence float64 `json:"confidence_building_level"`
}

func validateCandidates(address, expectedHouse string, response geoapifyResponse) Outcome {
	if len(response.Results) == 0 {
		return Outcome{Status: "not_found"}
	}
	for _, result := range response.Results {
		if !validCandidate(address, expectedHouse, result) {
			continue
		}
		return Outcome{
			Status: "success",
			Coordinate: &Coordinate{
				Latitude: result.Latitude, Longitude: result.Longitude,
				Confidence: result.Rank.Confidence,
			},
		}
	}
	return Outcome{Status: "rejected"}
}

func validCandidate(address, expectedHouse string, result geoapifyFeature) bool {
	if !strings.EqualFold(result.CountryCode, "ru") || !isSaintPetersburg(result.City, result.State) {
		return false
	}
	resultType := strings.ToLower(strings.TrimSpace(result.ResultType))
	if resultType != "building" && resultType != "amenity" {
		return false
	}
	// Geoapify can classify a precise street-and-house result as an amenity
	// (for example, a business center in the building). Accept that classification
	// only when the provider also confirms building-level confidence.
	if resultType == "amenity" && result.Rank.BuildingConfidence < 0.9 {
		return false
	}
	actualHouse, ok := normalizeHouseNumber(result.HouseNumber)
	if !ok || actualHouse != expectedHouse {
		return false
	}
	expectedStreet := normalizeStreet(extractStreet(address))
	if expectedStreet == "" || expectedStreet != normalizeStreet(result.Street) {
		return false
	}
	if result.Rank.Confidence < 0.95 || result.Rank.CityConfidence < 0.9 || result.Rank.StreetConfidence < 0.9 {
		return false
	}
	// If the provider supplies building-level confidence, require it to be high.
	if result.Rank.BuildingConfidence != 0 && result.Rank.BuildingConfidence < 0.9 {
		return false
	}
	if result.Latitude < 59.55 || result.Latitude > 60.35 || result.Longitude < 29.25 || result.Longitude > 31.35 {
		return false
	}
	// Street-level confidence and the exact house number protect against fuzzy
	// matches. The source address itself must also contain a street component.
	return extractStreet(address) != ""
}

func isSaintPetersburg(city, state string) bool {
	return isSPBName(city) || isSPBName(state)
}

func isSPBName(value string) bool {
	value = compact(value)
	return strings.Contains(value, "санктпетербург") || value == "saintpetersburg" || value == "stpetersburg"
}

func extractHouseNumber(address string) (string, bool) {
	parts := strings.Split(address, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		match := houseSuffixRE.FindStringSubmatch(strings.TrimSpace(parts[i]))
		if len(match) == 2 {
			if normalized, ok := normalizeHouseNumber(match[1]); ok {
				return normalized, true
			}
		}
	}
	return "", false
}

func extractStreet(address string) string {
	parts := strings.Split(address, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if houseSuffixRE.FindStringSubmatch(part) != nil {
			part = strings.TrimSpace(houseSuffixRE.ReplaceAllString(part, ""))
		}
		if isSPBName(part) || strings.EqualFold(part, "Россия") || strings.EqualFold(part, "Russia") {
			continue
		}
		if part != "" {
			return part
		}
	}
	return ""
}

func normalizeHouseNumber(value string) (string, bool) {
	value = strings.TrimSpace(value)
	match := houseTokenRE.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", false
	}
	return compact(match[1]), true
}

func normalizeStreet(value string) string {
	ignored := map[string]struct{}{
		"улица": {}, "ул": {}, "проспект": {}, "просп": {}, "пркт": {}, "пр": {},
		"набережная": {}, "наб": {}, "переулок": {}, "пер": {}, "шоссе": {}, "ш": {},
		"линия": {}, "лин": {}, "бульвар": {}, "бул": {}, "площадь": {}, "пл": {},
	}
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var result strings.Builder
	for _, word := range words {
		word = strings.ReplaceAll(word, "ё", "е")
		if _, skip := ignored[word]; skip {
			continue
		}
		result.WriteString(word)
	}
	return result.String()
}

func compact(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if r == 'ё' {
			r = 'е'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func retryDelay(header string, fallback time.Duration) time.Duration {
	if value, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && value >= 0 {
		return clampDelay(time.Duration(value) * time.Second)
	}
	if value, err := http.ParseTime(header); err == nil {
		return clampDelay(time.Until(value))
	}
	return clampDelay(fallback)
}

func clampDelay(delay time.Duration) time.Duration {
	if delay < 0 {
		return 0
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
