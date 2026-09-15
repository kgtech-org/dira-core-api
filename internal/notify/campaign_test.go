package notify

import (
	"strings"
	"testing"
)

// Le français est le repli : sans lui, une campagne ne pourrait rien servir
// à un téléphone dans une autre langue.
func TestACampaignNeedsTheFrenchText(t *testing.T) {
	if _, err := cleanLocales(map[string]Text{"en": {Title: "Hi", Body: "…"}}); err == nil {
		t.Fatalf("une campagne sans français doit être refusée")
	}
	out, err := cleanLocales(map[string]Text{"FR": {Title: " Alerte ", Body: "réseau "}, "en": {Title: "", Body: ""}, "de": {Title: "x", Body: "y"}})
	if err != nil {
		t.Fatalf("cleanLocales: %v", err)
	}
	if got := out["fr"]; got.Title != "Alerte" || got.Body != "réseau" {
		t.Fatalf("le texte doit être normalisé : %+v", got)
	}
	if _, ok := out["en"]; ok {
		t.Fatalf("une langue vide ne se garde pas")
	}
	if _, ok := out["de"]; ok {
		t.Fatalf("une langue inconnue ne se garde pas")
	}
	if _, err := cleanLocales(map[string]Text{"fr": {Title: strings.Repeat("a", MaxTitle*2+1)}}); err == nil {
		t.Fatalf("un titre déraisonnable doit être refusé")
	}
}

func TestCampaignRolesAreKnownAndUnique(t *testing.T) {
	got := uniqueRoles([]string{"driver", "admin", "client", "driver", "x"})
	if len(got) != 2 || got[0] != "driver" || got[1] != "client" {
		t.Fatalf("rôles inattendus : %v", got)
	}
}

// Une alerte de service ne se coupe pas — comme l'appel de course.
func TestAlertsAreNotMuteable(t *testing.T) {
	if Muteable(CategoryAlerts) {
		t.Fatalf("une alerte doit atteindre tout le monde")
	}
	if !Muteable(CategoryPromotions) {
		t.Fatalf("une promotion respecte le réglage de chacun")
	}
}
