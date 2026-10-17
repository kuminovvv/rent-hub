package freshness

import (
	"testing"
	"time"
)

func TestExplicitInactiveIsConservative(t *testing.T) {
	positive := []string{"Сдано ✅", "Квартира уже сдана", "Объявление не актуально", "Арендатор найден"}
	for _, text := range positive {
		if !ExplicitInactive(text) {
			t.Fatalf("%q should be inactive", text)
		}
	}
	negative := []string{"Дом будет сдан в 2027 году", "Квартира не сдана, ещё актуальна", "Сдаётся квартира на длительный срок"}
	for _, text := range negative {
		if ExplicitInactive(text) {
			t.Fatalf("%q must not be inactive", text)
		}
	}
}

func TestEvaluateHardAndEstimatedStatuses(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	deleted := Evaluate(now, Input{PostedAt: now.Add(-time.Hour), LastSeenAt: now, Deleted: true})
	if deleted.Status != "inactive" || deleted.Basis != "fact" || deleted.Score != 0 {
		t.Fatalf("deleted=%+v", deleted)
	}
	fresh := Evaluate(now, Input{PostedAt: now.Add(-12 * time.Hour), LastSeenAt: now})
	if fresh.Status != "active" || fresh.Score < 0.7 {
		t.Fatalf("fresh=%+v", fresh)
	}
	old := Evaluate(now, Input{PostedAt: now.Add(-30 * 24 * time.Hour), LastSeenAt: now})
	if old.Status != "likely_inactive" || old.Score >= 0.35 {
		t.Fatalf("old=%+v", old)
	}
}
