package qwen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractUsesJSONModeAndValidatesFacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Errorf("authorization header = %q", got)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ResponseFormat.Type != "json_object" || request.Model != "qwen-flash" || request.EnableThinking {
			t.Errorf("request config = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":123,"completion_tokens":45},"choices":[{"message":{"content":"{\"kind\":\"offer\",\"building_type\":\"secondary\",\"rooms\":2,\"price\":45000,\"area_sqm\":\"42,5 кв. м.\",\"metro\":\"Парнас\",\"address\":null}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := client.Extract(context.Background(), "Сдам квартиру")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Kind != "offer" || facts.Rooms == nil || *facts.Rooms != 2 || facts.PriceRub == nil || *facts.PriceRub != 45000 || facts.AreaSQM == nil || *facts.AreaSQM != 42.5 || facts.Address != "" {
		t.Fatalf("unexpected facts: %+v", facts)
	}
	input, output := client.Usage()
	if input != 123 || output != 45 {
		t.Fatalf("usage = (%d, %d), want (123, 45)", input, output)
	}
}

func TestExtractPrefersExplicitSourceRentOverModelUtilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"kind\":\"offer\",\"building_type\":\"unknown\",\"price_rub\":2000}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	text := "Сдаю свою комнату. Цена 15т.р без к.у, с коммуналкой выходит ещё около 2-3т.р; залог 50%"
	facts, err := client.Extract(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	if facts.PriceRub == nil || *facts.PriceRub != 15000 {
		t.Fatalf("price = %v, want 15000", facts.PriceRub)
	}
}

func TestExtractDetailsAreNormalizedAndUnsafeContactsDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"kind\":\"offer\",\"building_type\":\"unknown\",\"unexpected\":\"ignored\",\"details\":{\"floor\":\"19\",\"floors_total\":250,\"deposit_rub\":\"45 000 ₽\",\"utilities_terms\":\"коммунальные отдельно\",\"contact_name\":\"Дмитрий\",\"contact_phone\":\"+7 000 000-00-00\",\"contact_telegram\":\"javascript:alert(1)\",\"contact_vk\":\"https://vk.com/owner\",\"contact_methods\":[\"phone\",\"vk\",\"fax\",\"phone\"],\"features\":[\"мебель\",\"МЕБЕЛЬ\",\"стиральная машина\"]}}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := client.Extract(context.Background(), "Сдаётся квартира, 19 этаж, залог 45 000 ₽, с мебелью и стиральной машиной. Дмитрий: +7 000 000-00-00, https://vk.com/owner")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Details == nil {
		t.Fatal("details were not decoded")
	}
	details := facts.Details
	if details.Floor == nil || *details.Floor != 19 || details.FloorsTotal != nil || details.DepositRub == nil || *details.DepositRub != 45000 {
		t.Fatalf("unexpected numeric details: %+v", details)
	}
	if details.ContactName != "Дмитрий" || details.ContactPhone != "+7 000 000-00-00" || details.ContactTelegram != "" || details.ContactVK != "https://vk.com/owner" {
		t.Fatalf("unexpected contacts: %+v", details)
	}
	if len(details.ContactMethods) != 2 || details.ContactMethods[0] != "phone" || details.ContactMethods[1] != "vk" || len(details.Features) != 2 {
		t.Fatalf("unexpected normalized lists: %+v", details)
	}
}

func TestExtractGroundsDetailsAndRebuildsContactMethodsFromSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"kind\":\"offer\",\"building_type\":\"unknown\",\"details\":{\"commission_terms\":\"без комиссии\",\"contact_name\":\"Алексей\",\"contact_phone\":\"+7 000 000-00-00\",\"contact_telegram\":\"https://t.me/example_channel\",\"contact_vk\":\"https://vk.com/seller\",\"contact_methods\":[\"phone\",\"other\"]}}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	text := "Связаться можно в л.с. ВК или сообщения (Telegram) на номер +7 000 000-00-00 Дмитрий."
	facts, err := client.Extract(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Details == nil {
		t.Fatal("details were not decoded")
	}
	details := facts.Details
	if details.CommissionTerms != "" || details.ContactName != "" || details.ContactPhone != "" || details.ContactTelegram != "" || details.ContactVK != "" {
		t.Fatalf("unsupported details survived source grounding: %+v", details)
	}
	if len(details.ContactMethods) != 3 || details.ContactMethods[0] != "phone" || details.ContactMethods[1] != "telegram" || details.ContactMethods[2] != "vk" {
		t.Fatalf("contact methods = %v, want [phone telegram vk] from explicit source wording", details.ContactMethods)
	}
}

func TestExtractUsesExplicitSourceFallbackForDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"kind\":\"offer\",\"building_type\":\"unknown\",\"details\":{}}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	text := "Сдаётся студия, 19 этаж, 27 метров кв. 38 000 + ку, с мебелью, стиральной машиной, электроплитой и холодильником. Пишите: +7 000 000-00-00"
	facts, err := client.Extract(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Details == nil {
		t.Fatal("details were not decoded")
	}
	details := facts.Details
	if details.Floor == nil || *details.Floor != 19 || details.UtilitiesTerms != "коммунальные услуги отдельно" || details.ContactPhone != "+7 000 000-00-00" {
		t.Fatalf("explicit detail fallbacks missing: %+v", details)
	}
	for _, feature := range []string{"мебель", "стиральная машина", "электроплита", "холодильник"} {
		if !containsFold(details.Features, feature) {
			t.Errorf("features %v do not include %q", details.Features, feature)
		}
	}
}

func TestExtractNormalizesInvalidEnumsToUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"kind\":\"probably\",\"building_type\":\"unknown\"}"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "test-secret", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := client.Extract(context.Background(), "text")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Kind != "unknown" || facts.BuildingType != "unknown" {
		t.Fatalf("invalid enums were not normalized: %+v", facts)
	}
}

func TestNormalizesModelValueVariants(t *testing.T) {
	if got := normalizeBuildingType("secondary_market"); got != "secondary" {
		t.Fatalf("building type = %q, want secondary", got)
	}
	if got := normalizeKind("advertisement"); got != "ad" {
		t.Fatalf("kind = %q, want ad", got)
	}
	if got := string(normalizeTextValue(json.RawMessage(`["Парнас", "Проспект Просвещения"]`))); got != `"Парнас"` {
		t.Fatalf("metro = %s, want first array value", got)
	}
	if got := string(normalizeIntegerValue(json.RawMessage(`"45 000 ₽"`))); got != "45000" {
		t.Fatalf("price = %s, want 45000", got)
	}
	if got := normalizeContactLink("vk.com/owner", "vk"); got != "https://vk.com/owner" {
		t.Fatalf("bare VK URL = %q", got)
	}
	if !sourceContainsContactLink("профиль vk.com/owner", "https://vk.com/owner", "vk") {
		t.Fatal("explicit bare VK URL was not matched against the source")
	}
}

func TestNormalizeAreaValueAcceptsSquareMetersAndRejectsInvalidRange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`42.5`, "42.5"},
		{`"42,5 кв. м."`, "42.5"},
		{`"38 м²"`, "38"},
		{`"4.9"`, "null"},
		{`"501 кв.м"`, "null"},
		{`"примерно 40"`, "null"},
	}
	for _, test := range tests {
		if got := string(normalizeAreaValue(json.RawMessage(test.input))); got != test.want {
			t.Errorf("normalizeAreaValue(%s) = %s, want %s", test.input, got, test.want)
		}
	}
}
