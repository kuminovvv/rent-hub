package buildings

import (
	"regexp"
	"strings"
	"unicode"
)

var listingHousePattern = regexp.MustCompile(`(?i)^(.+?)[, ]+(?:д(?:ом)?\.?\s*)?(\d+[а-яa-z]?(?:/\d+[а-яa-z]?)?)(?:\s*к(?:орпус)?\.?\s*(\d+[а-яa-z]?))?(?:\s*(?:с|стр|строение)\.?\s*(\d+[а-яa-z]?))?(?:\s*(?:л|лит|литера)\.?\s*([а-яa-z0-9]+))?\s*$`)

var streetStopWords = map[string]struct{}{
	"г": {}, "город": {}, "санкт": {}, "петербург": {}, "спб": {},
	"ул": {}, "улица": {}, "пр": {}, "проспект": {}, "пр-т": {},
	"пер": {}, "переулок": {}, "ш": {}, "шоссе": {},
	"наб": {}, "набережная": {}, "проезд": {}, "пл": {}, "площадь": {},
	"ал": {}, "аллея": {}, "линия": {}, "дор": {}, "дорога": {},
	"бул": {}, "бульвар": {},
}

func ListingMatchKey(address string) string {
	address = cleanText(address)
	match := listingHousePattern.FindStringSubmatch(address)
	if len(match) == 0 {
		return ""
	}
	house := normalizeHousePart(match[2])
	if match[3] != "" {
		house += "к" + normalizeHousePart(match[3])
	}
	if match[4] != "" {
		house += "с" + normalizeHousePart(match[4])
	}
	if match[5] != "" {
		house += "л" + normalizeHousePart(match[5])
	}
	street := normalizeStreet(match[1])
	if street == "" || house == "" {
		return ""
	}
	return street + "|" + house
}

func StructuredMatchKey(street, house, block, building, letter string) string {
	street = normalizeStreet(street)
	house = normalizeHousePart(house)
	if street == "" || house == "" {
		return ""
	}
	if value := normalizeHousePart(block); value != "" {
		house += "к" + value
	}
	if value := normalizeHousePart(building); value != "" {
		house += "с" + value
	}
	if value := normalizeHousePart(letter); value != "" {
		house += "л" + value
	}
	return street + "|" + house
}

func normalizeStreet(value string) string {
	value = cleanText(value)
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte(' ')
		}
	}
	words := strings.Fields(builder.String())
	kept := words[:0]
	for _, word := range words {
		if _, ignored := streetStopWords[word]; ignored {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

func normalizeHousePart(value string) string {
	value = cleanText(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' {
			return r
		}
		return -1
	}, value)
	return value
}

func cleanText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.ReplaceAll(value, "ё", "е")
}
