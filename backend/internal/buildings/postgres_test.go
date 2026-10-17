package buildings

import "testing"

func TestResolveBuildingKeyRejectsAmbiguousCompleteRecords(t *testing.T) {
	_, ok := resolveBuildingKey([]keyedBuilding{
		{id: 1, guid: "a", score: 9},
		{id: 2, guid: "b", score: 8},
	})
	if ok {
		t.Fatal("equally complete conflicting rows must remain ambiguous")
	}
}

func TestResolveBuildingKeyAcceptsClearlyRicherDuplicate(t *testing.T) {
	id, ok := resolveBuildingKey([]keyedBuilding{
		{id: 1, guid: "legacy", score: 1},
		{id: 2, guid: "current", score: 9},
	})
	if !ok || id != 2 {
		t.Fatalf("id=%d ok=%t, want id=2 ok=true", id, ok)
	}
}
