package buildings

import (
	"strings"
	"testing"
)

func TestParseCSVKeepsMoreCompleteDuplicate(t *testing.T) {
	csvData := "\ufeffid;formalname_street;house_number;building;block;letter;address;houseguid;built_year;elevators_count;wall_material\n" +
		"1;Турку;12;;5;;addr;guid-1;;;\n" +
		"2;Турку;12;;5;;addr;guid-1;1971;1;Кирпич\n"
	records, rows, duplicates, err := parseCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if rows != 2 || duplicates != 1 || len(records) != 1 {
		t.Fatalf("rows=%d duplicates=%d records=%d", rows, duplicates, len(records))
	}
	if records[0].BuiltYear == nil || *records[0].BuiltYear != 1971 || records[0].WallMaterial == nil || *records[0].WallMaterial != "Кирпич" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestProviderPlaceholdersBecomeNil(t *testing.T) {
	csvData := "\ufeffid;formalname_street;house_number;building;block;letter;address;houseguid;project_type;energy_efficiency\n" +
		"1;Турку;12;;5;;addr;guid-1;Не заполнено;-\n"
	records, _, _, err := parseCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ProjectType != nil || records[0].EnergyEfficiency != nil {
		t.Fatalf("unexpected placeholders: %+v", records)
	}
}
