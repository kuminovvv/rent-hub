// Package listingextract parses only attributes that are explicit enough in
// Telegram post text. Missing or ambiguous values are left empty.
package listingextract

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"spb-rent/backend/internal/domain"
)

var (
	roomPattern                   = regexp.MustCompile(`(?i)([1-9][0-9]?)\s*[-–]?\s*(?:комнатн[^\s]*|комн\.?|к)\s*(?:квартир[^\s]*|комнат[^\s]*|апартамент[^\s]*)`)
	yandexRoomPattern             = regexp.MustCompile(`(?i)([1-9][0-9]?)\s*[-–]?\s*комнатн[^\s]*\s+квартир`)
	pricePattern                  = regexp.MustCompile(`(?i)([0-9][0-9\s  .,]*)\s*(?:₽|руб(?:лей|ля|ль)?\.?|р\.(?:\s|$))`)
	monthlyPattern                = regexp.MustCompile(`(?i)([0-9][0-9\s  .,]*)\s*(?:₽|руб(?:лей|ля|ль)?\.?|р\.)?\s*(?:/\s*(?:мес\.?|месяц)|в\s+месяц|ежемесячно)`)
	compactMonthlyPattern         = regexp.MustCompile(`(?i)([0-9]{2,6})\s*(?:тыс\.?|к)\s*(?:₽|руб(?:лей|ля|ль)?\.?)?\s*(?:/\s*(?:мес\.?|месяц)|в\s+месяц|ежемесячно|$)`)
	labeledPricePattern           = regexp.MustCompile(`(?i)(?:цена|стоимост[а-яё]*|аренд[а-яё]*(?:\s+плат[а-яё]*)?)\s*[:=—-]?\s*([0-9][0-9\s  .,]*)\s*(?:([тt]\s*\.?\s*(?:[рr]|руб(?:лей|ля|ль)?)\.?|тыс(?:\.|яч(?:а|и)?)?|к)\s*(?:₽|руб(?:лей|ля|ль)?\.?)?|₽|руб(?:лей|ля|ль)?\.?|р\.)?(?:\s*/\s*(?:мес\.?|месяц))?`)
	rentWithUtilitiesPattern      = regexp.MustCompile(`(?i)([0-9][0-9\s  .,]*?)\s*(([тt]\s*\.?\s*(?:[рr]|руб(?:лей|ля|ль)?)\.?|тыс(?:\.|яч(?:а|и)?)?|к)\s*(?:₽|руб(?:лей|ля|ль)?\.?)?|₽|руб(?:лей|ля|ль)?\.?|р\.)?\s*\+\s*(?:к\.?\s*у\.?|коммун[а-яё]*)`)
	areaPattern                   = regexp.MustCompile(`(?i)([0-9]{1,3}(?:[.,][0-9]+)?)\s*(?:м\s*(?:²|2|кв\.?\s*м?\.?|етр(?:а|ов)?\s*(?:кв\.?|квадратн[а-яё]*))|метр[а-яё]*\s*(?:кв\.?|квадратн[а-яё]*)|кв\.?\s*м\.?)`)
	nonApartmentAreaBeforePattern = regexp.MustCompile(`(?i)(?:кухн[а-яё]*|комнат[а-яё]*|жил[а-яё]*\s+площад[а-яё]*|балкон[а-яё]*|лодж[а-яё]*|террас[а-яё]*|участ[а-яё]*|потол[а-яё]*|этаж[а-яё]*)[^,;.!\n]{0,45}$`)
	addressMarkerPattern          = regexp.MustCompile(`(?i)(?:📍\s*)?(?:адрес\s*[:—-]\s*|по\s+адресу\s+|📍\s*)([^\n;]{3,120})`)
	houseNumberPattern            = regexp.MustCompile(`(?i)\b\d{1,4}(?:\s*(?:к\.?\s*|/\s*)\d{1,4})?(?:\s*(?:корп(?:ус)?\.?|лит(?:ера)?\.?)\s*[0-9а-я])?`)
	streetPattern                 = regexp.MustCompile(`(?i)(?:проспект|пр-т|пр\.|улиц\w*|ул\.|набережн\w*|шоссе|переул\w*|пер\.|бульвар|б-р|площадь|пл\.|лини\w*|алле\w*|дорог\w*|проезд)`)
	ordinalStreetPattern          = regexp.MustCompile(`(?i)\b[1-9][0-9]?(?:-ая|-я|-й)\s+[а-яё-]+`)
	streetNamePattern             = regexp.MustCompile(`(?i)[а-яё-]*(?:ская|ской|цкая|цкой|овская|овской|евская|евской)(?:$|[^а-яё])`)
	addressWalkPattern            = regexp.MustCompile(`(?i)(?:минут|(?:^|[^а-яё])мин(?:$|[^а-яё])|пешком|до\s+метро|метро|(?:^|[^а-яё])м\.|км|этаж)`)
	addressMetaPattern            = regexp.MustCompile(`\s+[|/]\s+`)
	strongWantedPattern           = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]а-яё])(?:ищу|сниму|снимем|ищем|хочу\s+снять|нужна?\s+(?:квартира|студия)|нужны?\s+(?:квартира|комната))(?:$|[^[:alnum:]а-яё])`)
	hashtagIntentPattern          = regexp.MustCompile(`(?i)#(?:ищу|сниму)[а-яё]*`)
	rentVerbPattern               = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]а-яё])(?:сда[её]тся|сда[её]т|сдаю|сдам|сда[её]м|сда[её]те|аренда)(?:$|[^[:alnum:]а-яё])`)
	hashtagOfferPattern           = regexp.MustCompile(`(?i)#сда[а-яё]*`)
	housingPattern                = regexp.MustCompile(`(?i)(?:квартир\w*|студи\w*|комнат\w*|апартамент\w*|жил\w*\s+комплекс)`)
	salePattern                   = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]а-яё])(?:продам|прода[её]тся|куплю|продажа)(?:$|[^[:alnum:]а-яё])`)
	yandexOfferPattern            = regexp.MustCompile(`(?is)^\s*[0-9][0-9\s  .,]*\s*₽\s*[·•]\s*[1-9][0-9]?\s*[-–]?\s*комнатн[^\s]*\s+квартир`)
	yandexStudioPattern           = regexp.MustCompile(`(?is)^\s*[0-9][0-9\s  .,]*\s*₽\s*[·•]\s*студи[а-яё]*`)
	metroCuePattern               = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:метро(?:$|[^\p{L}])|м\.|м\s+)\s*[:—-]?\s*([^,;.!\n]{2,60})`)
)

// Extract classifies a rental post and extracts apartment facts where the text
// supports them. For map coordinates, a station must be named unambiguously.
func Extract(text string) domain.ListingFacts {
	facts := domain.ListingFacts{Kind: "unknown", BuildingType: "unknown"}
	if strings.TrimSpace(text) == "" {
		return facts
	}
	clean := strings.TrimSpace(text)
	lower := strings.ToLower(clean)

	switch {
	case strongWantedPattern.MatchString(lower) || hashtagIntentPattern.MatchString(lower):
		facts.Kind = "wanted"
	case isAd(lower):
		facts.Kind = "ad"
	case hasHousingOffer(lower):
		facts.Kind = "offer"
	case hasHousingTopic(lower):
		facts.Kind = "other"
	default:
		facts.Kind = "unknown"
	}

	if facts.Kind != "offer" {
		return facts
	}
	if strings.Contains(lower, "студи") {
		zero := 0
		facts.Rooms = &zero
	} else {
		facts.Rooms = extractRooms(lower)
	}
	facts.PriceRub = extractPrice(clean)
	facts.AreaSQM = extractArea(clean)
	facts.BuildingType = extractBuildingType(lower)
	facts.Address = extractAddress(clean)
	facts.Metro = extractMetro(lower)
	return facts
}

func hasHousingOffer(text string) bool {
	if !housingPattern.MatchString(text) {
		return false
	}
	if salePattern.MatchString(text) {
		return false
	}
	if rentVerbPattern.MatchString(text) || hashtagOfferPattern.MatchString(text) {
		return true
	}
	// Yandex Rental cards commonly start with "45 000 ₽ · 1-комнатная квартира"
	// or "39 500 ₽ · студия".
	if yandexOfferPattern.MatchString(text) || yandexStudioPattern.MatchString(text) {
		return true
	}
	// Some channels omit the verb but state a room type and an explicit monthly
	// rent. A generic price or an unrelated number is not sufficient.
	return monthlyPattern.MatchString(text) || compactMonthlyPattern.MatchString(text) || rentWithUtilitiesPattern.MatchString(text)
}

func isAd(text string) bool {
	for _, marker := range []string{
		"реклама", "рекламный пост", "рекламная публикация", "подпишись", "подпишитесь",
		"розыгрыш", "вакансия", "набор сотрудников", "служебное сообщение", "бот запущен",
		"контакты скрыты — нажмите кнопку", "контакты скрыты - нажмите кнопку",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func hasHousingTopic(text string) bool {
	return housingPattern.MatchString(text) || strings.Contains(text, "недвижим") || strings.Contains(text, "риелтор")
}

func extractRooms(text string) *int {
	m := yandexRoomPattern.FindStringSubmatch(text)
	if m == nil {
		m = roomPattern.FindStringSubmatch(text)
	}
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n > 20 {
		return nil
	}
	return &n
}

func extractBuildingType(text string) string {
	newBuild := strings.Contains(text, "новостро") || strings.Contains(text, "новый жилой комплекс") || strings.Contains(text, "новом жк")
	secondary := strings.Contains(text, "вторич") || strings.Contains(text, "старый фонд") || strings.Contains(text, "вторичный фонд")
	if newBuild == secondary {
		return "unknown"
	}
	if newBuild {
		return "new"
	}
	return "secondary"
}

func extractPrice(text string) *int {
	lower := strings.ToLower(text)
	prices := make([]int, 0, 4)
	addPrice := func(start, end int, value *int) {
		if value == nil || isDepositContext(lower, start) || isNonRentCostContext(lower, start) || isDailyRateContext(lower, start, end) {
			return
		}
		for _, existing := range prices {
			if existing == *value {
				return
			}
		}
		prices = append(prices, *value)
	}
	// Compact thousands without an explicit /month marker are accepted only
	// after a strong rent-price label. This covers "Цена 15т.р" while keeping
	// nearby utilities and deposit amounts out of the monthly rent field.
	for _, loc := range labeledPricePattern.FindAllStringSubmatchIndex(lower, -1) {
		if len(loc) >= 6 {
			addPrice(loc[0], loc[1], parsePriceNumber(lower[loc[2]:loc[3]], loc[4] >= 0))
		}
	}
	// Amount followed by " + к/у" is a common compact rental format. The
	// suffix identifies rent while avoiding unrelated standalone numbers.
	for _, loc := range rentWithUtilitiesPattern.FindAllStringSubmatchIndex(lower, -1) {
		if len(loc) >= 4 {
			thousands := len(loc) >= 8 && loc[6] >= 0
			value := parsePriceNumber(lower[loc[2]:loc[3]], thousands)
			if value == nil && !thousands {
				value = parseSmallBareRentThousands(lower[loc[2]:loc[3]])
			}
			addPrice(loc[0], loc[1], value)
		}
	}
	// Explicit monthly wording has precedence; skip matches explicitly labeled
	// as deposit/security instead of interpreting them as monthly rent.
	for i, pattern := range []*regexp.Regexp{monthlyPattern, compactMonthlyPattern} {
		for _, loc := range pattern.FindAllStringSubmatchIndex(lower, -1) {
			if len(loc) < 4 {
				continue
			}
			addPrice(loc[0], loc[1], parsePriceNumber(lower[loc[2]:loc[3]], i == 1))
		}
	}
	// A bare ruble amount is accepted only for the structured Yandex heading
	// format. It is rejected when the immediate context says deposit.
	if strings.Contains(lower, "₽") && (roomPattern.MatchString(lower) || strings.Contains(lower, "студи")) {
		for _, loc := range pricePattern.FindAllStringSubmatchIndex(lower, -1) {
			if len(loc) >= 4 {
				addPrice(loc[0], loc[1], parsePriceNumber(lower[loc[2]:loc[3]], false))
			}
		}
	}
	if len(prices) != 1 {
		return nil
	}
	return &prices[0]
}

func parseSmallBareRentThousands(raw string) *int {
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return nil
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil || n < 5 || n > 999 {
		return nil
	}
	return parsePriceNumber(strconv.Itoa(n), true)
}

func isDailyRateContext(text string, start, end int) bool {
	from := start - 24
	if from < 0 {
		from = 0
	}
	to := end + 32
	if to > len(text) {
		to = len(text)
	}
	context := text[from:to]
	for _, marker := range []string{"сутк", "в сутки", "/сут", "за день", "в день", "посуточн"} {
		if strings.Contains(context, marker) {
			return true
		}
	}
	return false
}

func isNonRentCostContext(text string, start int) bool {
	context := recentPriceContext(text, start, 40)
	for _, marker := range []string{"коммун", "к.у", "ку.", "залог", "депозит", "обеспечительн", "задаток", "комисси", "телефон", "номер телефона"} {
		if strings.Contains(context, marker) {
			return true
		}
	}
	return false
}

func recentPriceContext(text string, start, maxRunes int) string {
	from := start - maxRunes
	if from < 0 {
		from = 0
	}
	if separator := strings.LastIndexAny(text[from:start], "\n,;.!?"); separator >= 0 {
		from += separator + 1
	}
	return text[from:start]
}

func extractArea(text string) *float64 {
	for _, loc := range areaPattern.FindAllStringSubmatchIndex(text, -1) {
		if len(loc) < 4 {
			continue
		}
		from := loc[0] - 50
		if from < 0 {
			from = 0
		}
		if nonApartmentAreaBeforePattern.MatchString(strings.ToLower(text[from:loc[0]])) {
			continue
		}
		raw := strings.ReplaceAll(text[loc[2]:loc[3]], ",", ".")
		area, err := strconv.ParseFloat(raw, 64)
		if err == nil && area >= 8 && area <= 1000 {
			return &area
		}
	}
	return nil
}

func isDepositContext(text string, start int) bool {
	context := recentPriceContext(text, start, 36)
	return strings.Contains(context, "залог") || strings.Contains(context, "депозит") || strings.Contains(context, "обеспечительный")
}

func parsePriceNumber(raw string, thousands bool) *int {
	if thousands {
		normalized := strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", ",", ".").Replace(raw)
		if value, err := strconv.ParseFloat(normalized, 64); err == nil {
			amount := int(value*1000 + 0.5)
			if amount >= 5000 && amount <= 2_000_000 {
				return &amount
			}
		}
		return nil
	}
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return nil
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return nil
	}
	if n < 5000 || n > 2_000_000 {
		return nil
	}
	return &n
}

func extractAddress(text string) string {
	markers := addressMarkerPattern.FindAllStringSubmatchIndex(text, -1)
	for _, marker := range markers {
		if len(marker) < 4 {
			continue
		}
		address := strings.TrimSpace(text[marker[2]:marker[3]])
		address = regexp.MustCompile(`(?i)^санкт-петербург\s*,?\s*`).ReplaceAllString(address, "")
		address = regexp.MustCompile(`(?i)^спб\s*,?\s*`).ReplaceAllString(address, "")
		if loc := addressMetaPattern.FindStringIndex(address); loc != nil {
			address = strings.TrimSpace(address[:loc[0]])
		}

		houses := houseNumberPattern.FindAllStringIndex(address, -1)
		if len(houses) == 0 {
			continue
		}
		// The last number is the house number in addresses such as
		// "4-ая Советская, 46/8"; earlier digits can belong to street names.
		house := houses[len(houses)-1]
		prefix := strings.TrimSpace(strings.TrimRight(address[:house[0]], ",.;: "))
		if prefix == "" || addressWalkPattern.MatchString(prefix) {
			continue
		}
		streetStart := streetEvidenceStart(prefix)
		if streetStart < 0 {
			continue
		}
		if addressWalkPattern.MatchString(prefix[:streetStart]) || addressWalkPattern.MatchString(prefix) {
			continue
		}
		return strings.TrimSpace(strings.TrimRight(address[:house[1]], ",.;: "))
	}
	return ""
}

func streetEvidenceStart(prefix string) int {
	start := -1
	for _, pattern := range []*regexp.Regexp{streetPattern, ordinalStreetPattern, streetNamePattern} {
		loc := pattern.FindStringIndex(prefix)
		if loc != nil && (start < 0 || loc[0] < start) {
			start = loc[0]
		}
	}
	return start
}

// extractMetro returns only an explicitly named Saint Petersburg station.
// It intentionally does not synthesize coordinates: a station is not an
// apartment address and must not appear as a precise listing location.
func extractMetro(text string) string {
	match := metroCuePattern.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	text = match[1]
	for _, station := range metroStations {
		for _, alias := range station.aliases {
			if containsWord(text, alias) {
				return station.name
			}
		}
	}
	return ""
}

func containsWord(text, word string) bool {
	for offset := 0; offset < len(text); {
		relative := strings.Index(text[offset:], word)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(word)
		beforeOK := start == 0
		if !beforeOK {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			beforeOK = !unicode.IsLetter(previous) && !unicode.IsNumber(previous)
		}
		afterOK := end == len(text)
		if !afterOK {
			next, _ := utf8.DecodeRuneInString(text[end:])
			afterOK = !unicode.IsLetter(next) && !unicode.IsNumber(next)
		}
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
	return false
}
