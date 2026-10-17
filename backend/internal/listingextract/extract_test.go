package listingextract

import (
	"testing"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		kind        string
		building    string
		rooms       *int
		price       *int
		area        *float64
		metro       string
		address     string
		coordinates bool
	}{
		{
			name:     "Yandex rental heading extracts price and rooms",
			text:     "45 000 ₽ · 1-комнатная квартира\nСанкт-Петербург, Парнас",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(1),
			price:    intPtr(45000),
		},
		{
			name:     "Yandex standardized studio card is a rental offer",
			text:     "39 500 ₽ · студия, 30 м² / Санкт-Петербург / 📍",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(0),
			price:    intPtr(39500),
			area:     floatPtr(30),
		},
		{
			name:     "uppercase room abbreviation with explicit monthly rent is an offer",
			text:     "2-КОМН КВАРТИРА, 56 м² / м Автово / рядом парк / Маршала Казакова 50к1 / 65 000 ₽/мес, БЕЗ КОМИССИИ",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(2),
			price:    intPtr(65000),
			area:     floatPtr(56),
			metro:    "Автово",
		},
		{
			name:     "rent verb monthly price is separate from deposit",
			text:     "Сдаётся 2-комнатная квартира, вторичка. Аренда 50 000 руб./мес., залог 80 000 рублей. Метро Парнас.",
			kind:     "offer",
			building: "secondary",
			rooms:    intPtr(2),
			price:    intPtr(50000),
			metro:    "Парнас",
		},
		{
			name:     "studio and compact monthly price",
			text:     "Сдам студию в новостройке у метро Девяткино за 35 тыс. в месяц",
			kind:     "offer",
			building: "new",
			rooms:    intPtr(0),
			price:    intPtr(35000),
			metro:    "Девяткино",
		},
		{
			name:     "person looking for an apartment is wanted",
			text:     "Ищу квартиру, 1-комнатную, бюджет до 45 000 ₽. Залог готов.",
			kind:     "wanted",
			building: "unknown",
		},
		{
			name:     "wanting to rent is never classified as offering",
			text:     "Хочу снять квартиру на длительный срок, бюджет 40 000 ₽ в месяц.",
			kind:     "wanted",
			building: "unknown",
		},
		{
			name:     "channel hashtag still marks a wanted post",
			text:     "#ИщуГнездышко Студию или комнату, бюджет 30 000 рублей",
			kind:     "wanted",
			building: "unknown",
		},
		{
			name:     "channel hashtag and studio mark a rental offer",
			text:     "#СдаюГнездышко / Студия / м.Ломоносовская / Сдаётся на длительный срок",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(0),
			metro:    "Ломоносовская",
		},
		{
			name:     "number-k abbreviation is rooms, not a house number",
			text:     "2-к квартира❤️ / м.Выборгская / Друзья! Сдается уютная квартира за 55 000 рублей в месяц",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(2),
			price:    intPtr(55000),
			metro:    "Выборгская",
		},
		{
			name:     "person offering to rent is an offer",
			text:     "Сдам 1-комнатную квартиру, 42 000 ₽ в месяц. Залог 60 000 ₽.",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(1),
			price:    intPtr(42000),
		},
		{
			name:     "advertisement is not an offer",
			text:     "Реклама: подпишитесь на наш канал недвижимости! Лучшие предложения каждый день.",
			kind:     "ad",
			building: "unknown",
		},
		{
			name:     "service notice is not an offer",
			text:     "Служебное сообщение: канал временно закрыт на обновление.",
			kind:     "ad",
			building: "unknown",
		},
		{
			name:     "hidden contact service text is not an offer",
			text:     "Контакты скрыты — нажмите кнопку, чтобы перейти к автору.",
			kind:     "ad",
			building: "unknown",
		},
		{
			name:     "sale is a housing topic but not rental offer",
			text:     "Продам квартиру в новостройке, 3-комнатная, 12 000 000 ₽.",
			kind:     "other",
			building: "unknown",
		},
		{
			name:     "deposit alone is not monthly rental price",
			text:     "Сдам квартиру, залог 45 000 ₽. Цена обсуждается.",
			kind:     "offer",
			building: "unknown",
		},
		{
			name:     "explicit address with building number",
			text:     "Сдаётся 1-комнатная квартира за 44 000 ₽ в месяц. 📍Адрес: Комендантский проспект 66к5",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(1),
			price:    intPtr(44000),
			address:  "Комендантский проспект 66к5",
		},
		{
			name:     "explicit address without street suffix and with comma",
			text:     "Сдам студию, 38 000 ₽ в месяц, по адресу Афонская 20, рядом метро Озерки",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(0),
			price:    intPtr(38000),
			metro:    "Озерки",
			address:  "Афонская 20",
		},
		{
			name:     "address can include comma and building number",
			text:     "Сдаётся квартира 40 000 ₽ в месяц. 📍проспект Науки, 4 к.1 / м.Выборгская / Друзья!",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
			metro:    "Выборгская",
			address:  "проспект Науки, 4 к.1",
		},
		{
			name:     "labeled address with корпус and улица abbreviation",
			text:     "Сдается комната за 30 000 ₽ в месяц. Адрес: ул. Тосина д.1 к.1",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(30000),
			address:  "ул. Тосина д.1 к.1",
		},
		{
			name:     "labeled address with корпус suffix",
			text:     "Сдаётся квартира за 50 000 ₽ в месяц. Адрес: проспект Металлистов, 61к2",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(50000),
			address:  "проспект Металлистов, 61к2",
		},
		{
			name:     "address without house number is omitted",
			text:     "Сдаю квартиру, 40 000 ₽ в месяц. Адрес: Невский проспект",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
		},
		{
			name:     "walking time after pin is not mistaken for house number",
			text:     "Сдаётся квартира за 40 000 ₽ в месяц. 📍15 минут пешком до метро",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
		},
		{
			name:     "number without street text is not an address",
			text:     "Сдаётся квартира за 40 000 ₽ в месяц. 📍 10",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
		},
		{
			name:     "skip walking-time pin and take the later street address",
			text:     "Сдаётся квартира за 40 000 ₽ в месяц. 📍 10 минут пешком до метро\n📍 Днепропетровская, 37",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
			address:  "Днепропетровская, 37",
		},
		{
			name:     "ordinal Soviet street and slash house number are preserved",
			text:     "Сдаётся квартира за 40 000 ₽ в месяц. 📍Адрес: 4-ая Советская, 46/8",
			kind:     "offer",
			building: "unknown",
			price:    intPtr(40000),
			address:  "4-ая Советская, 46/8",
		},
		{
			name:     "ambiguous location is not given invented coordinates",
			text:     "Сдаётся 1-комнатная квартира за 45 000 ₽ в месяц недалеко от метро Парнас",
			kind:     "offer",
			building: "unknown",
			rooms:    intPtr(1),
			price:    intPtr(45000),
			metro:    "Парнас",
		},
		{
			name:     "empty text remains unknown",
			text:     "  \n ",
			kind:     "unknown",
			building: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.text)
			if got.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.BuildingType != tt.building {
				t.Errorf("BuildingType = %q, want %q", got.BuildingType, tt.building)
			}
			assertIntPtr(t, "Rooms", got.Rooms, tt.rooms)
			assertIntPtr(t, "PriceRub", got.PriceRub, tt.price)
			if got.Metro != tt.metro {
				t.Errorf("Metro = %q, want %q", got.Metro, tt.metro)
			}
			if got.Address != tt.address {
				t.Errorf("Address = %q, want %q", got.Address, tt.address)
			}
			assertFloatPtr(t, "AreaSQM", got.AreaSQM, tt.area)
			if (got.Latitude != nil || got.Longitude != nil) != tt.coordinates {
				t.Errorf("coordinates present = %v, want %v", got.Latitude != nil || got.Longitude != nil, tt.coordinates)
			}
		})
	}
}

func TestExtractExplicitMonthlyPricesAndApartmentArea(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		price *int
		area  *float64
	}{
		{name: "rent plus utilities", text: "Сдаётся квартира-студия. 38000 + ку. 27 метров кв. включая балкон-лоджию", price: intPtr(38000), area: floatPtr(27)},
		{name: "dot grouped ruble rent", text: "Сдаётся квартира. Аренда 38.000₽ + ку, 27 м²", price: intPtr(38000), area: floatPtr(27)},
		{name: "rental label with full amount", text: "Сдам квартиру. Аренда: 38 000 рублей, общая площадь 27 кв.м.", price: intPtr(38000), area: floatPtr(27)},
		{name: "compact rent label", text: "Сдаю студию. Цена 38к, площадь 27 кв. м", price: intPtr(38000), area: floatPtr(27)},
		{name: "fractional compact thousands", text: "Сдам квартиру. Аренда 38,5 тыс. руб. в месяц", price: intPtr(38500)},
		{name: "deposit mentioned before rent", text: "Сдам квартиру. Залог 60 000 ₽, аренда 38 000 ₽ в месяц", price: intPtr(38000)},
		{name: "bare thousands before utilities", text: "Сдаётся квартира. Далее 25+КУ", price: intPtr(25000)},
		{name: "compact k before utilities", text: "Сдам комнату. 26к+ ку", price: intPtr(26000)},
		{name: "compact tr before utilities", text: "Сдам комнату. 30тр + ку", price: intPtr(30000)},
		{name: "deposit after utilities is ignored", text: "Сдам квартиру. 20+ку, залог 18т", price: intPtr(20000)},
		{name: "different room prices are ambiguous", text: "Сдам комнаты. 30тр + ку; 25тр + ку; 30тр + ку"},
		{name: "daily rate is not monthly rent", text: "Сдам квартиру посуточно, 3 000 ₽/сутки"},
		{name: "reject kitchen and room areas", text: "Сдам квартиру. 38 000 ₽ в месяц. Кухня 8 м², комната 14 кв.м.", price: intPtr(38000)},
		{name: "reject deposit utilities and unrelated numbers", text: "Сдаю квартиру на 5 этаже, телефон +7 000 000-00-00 Залог 60 000 ₽, коммунальные услуги 3 000 ₽. Кухня 9 м².", area: nil},
		{name: "reject sale price", text: "Продам квартиру 27 м² за 8 000 000 ₽", area: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.text)
			assertIntPtr(t, "PriceRub", got.PriceRub, tt.price)
			assertFloatPtr(t, "AreaSQM", got.AreaSQM, tt.area)
		})
	}
}

func TestExtractCompactRentalPriceIgnoresUtilitiesAndDeposit(t *testing.T) {
	tests := []string{
		"Сдаю свою комнату. Цена 15т.р без к.у, с коммуналкой выходит ещё около 2-3т.р; залог 50%",
		"Сдаю комнату. Цена 15 т.р.; коммуналка 3 т.р.",
		"Сдаю комнату. Цена 15тр, залог 50%",
		"Сдаю комнату. Цена 15 т. руб.; к.у. 2500",
		"Сдаю комнату. Цена 15 тысяч рублей; коммунальные услуги отдельно",
		"Сдаю комнату. Цена 15к/мес; залог 15к",
		"Сдаю комнату. Цена 15TR; коммуналка 2-3TR",
		"Сдаю комнату. Цена 15 t.r.; коммуналка отдельно",
	}
	for _, text := range tests {
		t.Run(text, func(t *testing.T) {
			got := Extract(text)
			if got.PriceRub == nil || *got.PriceRub != 15000 {
				t.Fatalf("PriceRub = %v, want 15000", got.PriceRub)
			}
		})
	}
}

func TestExtractDoesNotUseCompactUtilitiesOrDepositAsRent(t *testing.T) {
	got := Extract("Сдаю комнату. Коммуналка 2-3т.р; залог 15т.р. Цена обсуждается")
	if got.PriceRub != nil {
		t.Fatalf("PriceRub = %d, want nil", *got.PriceRub)
	}
}

func TestExtractRejectsMetroSubstring(t *testing.T) {
	got := Extract("Сдаётся квартира у Парнасского шоссе, 40 000 ₽ в месяц")
	if got.Metro != "" {
		t.Fatalf("Metro = %q, want empty for non-station substring", got.Metro)
	}
}

func TestExtractAddressRejectsTransitOnlyAndMixedCandidates(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "hashtag and metro walking range are not an address",
			text: "📍#Лесная (метро 10-15",
		},
		{
			name: "transit mentions before a later street and number invalidate the candidate",
			text: "📍15 мин Василеостровской, Горного института, Приморской . Малый проспект ВО 52",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractAddress(tt.text); got != "" {
				t.Fatalf("extractAddress() = %q, want empty", got)
			}
		})
	}
}

func assertIntPtr(t *testing.T, field string, got, want *int) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %d, want nil", field, *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Errorf("%s = %v, want %d", field, got, *want)
	}
}

func assertFloatPtr(t *testing.T, field string, got, want *float64) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %v, want nil", field, *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Errorf("%s = %v, want %v", field, got, *want)
	}
}

func intPtr(n int) *int { return &n }

func floatPtr(n float64) *float64 { return &n }
