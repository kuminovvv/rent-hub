package buildings

import "testing"

func TestListingMatchKey(t *testing.T) {
	tests := map[string]string{
		"улица Турку, 12к5":             "турку|12к5",
		"улица Ушинского, 33к1":         "ушинского|33к1",
		"улица Ярослава Гашека, 12/100": "ярослава гашека|12/100",
		"Пулковское шоссе, 14":          "пулковское|14",
		"улица Шоссе в Лаврики, 59к1":   "в лаврики|59к1",
	}
	for input, want := range tests {
		if got := ListingMatchKey(input); got != want {
			t.Errorf("ListingMatchKey(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestStructuredMatchKey(t *testing.T) {
	if got := StructuredMatchKey("Турку", "12", "5", "", ""); got != "турку|12к5" {
		t.Fatalf("key=%q", got)
	}
	if got := StructuredMatchKey("Шоссе в Лаврики", "59", "1", "", ""); got != "в лаврики|59к1" {
		t.Fatalf("key=%q", got)
	}
}
