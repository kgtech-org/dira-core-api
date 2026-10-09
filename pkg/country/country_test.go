package country

import (
	"strings"
	"testing"
)

func TestLocate(t *testing.T) {
	cases := []struct {
		name     string
		lng, lat float64
		want     string
		ok       bool
	}{
		{"Lomé", 1.2255, 6.1319, "TG", true},
		{"Kara", 1.1833, 9.5511, "TG", true},
		{"Aflao (Ghana, contre la frontière de Lomé)", 1.1900, 6.1180, "GH", true},
		{"Cotonou", 2.4183, 6.3654, "BJ", true},
		{"Abidjan", -4.0083, 5.3600, "CI", true},
		{"Dakar", -17.4467, 14.6928, "SN", true},
		{"Conakry", -13.5784, 9.6412, "GN", true},
		{"Accra", -0.1870, 5.6037, "GH", true},
		{"Lagos", 3.3792, 6.5244, "NG", true},
		{"Douala", 9.7679, 4.0511, "CM", true},
		{"N'Djamena", 15.0600, 12.1100, "TD", true},
		{"Libreville", 9.4500, 0.4100, "GA", true},
		{"Port-Gentil", 8.7815, -0.7193, "GA", true},
		{"en mer devant Lomé", 1.2255, 5.9000, "", false},
		{"Paris", 2.3522, 48.8566, "", false},
	}
	for _, c := range cases {
		got, ok := Locate(c.lng, c.lat)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Locate(%v, %v) = %q,%v ; want %q,%v", c.name, c.lng, c.lat, got, ok, c.want, c.ok)
		}
	}
}

func TestCatalogHasBorders(t *testing.T) {
	loadOnce.Do(load)
	for _, c := range Catalog {
		found := false
		for _, s := range shapes {
			if s.code == c.Code {
				found = len(s.polys) > 0
			}
		}
		if !found {
			t.Errorf("%s is in the catalog but has no border", c.Code)
		}
		if got, ok := Locate(c.Center[0], c.Center[1]); !ok || got != c.Code {
			t.Errorf("%s: its own center locates to %q", c.Code, got)
		}
	}
}

func TestCatalogCurrenciesAreKnown(t *testing.T) {
	for _, c := range Catalog {
		if _, ok := LookupCurrency(c.Currency); !ok {
			t.Errorf("%s: currency %s is not in Currencies", c.Code, c.Currency)
		}
	}
	if cur, ok := LookupCurrency("gnf"); !ok || cur.Symbol != "FG" || cur.Decimals != 0 {
		t.Fatalf("LookupCurrency(gnf) = %+v, %v", cur, ok)
	}
	if NormalizeCurrency("xo") != "" || NormalizeCurrency("x0f") != "" {
		t.Fatal("NormalizeCurrency accepted a bad code")
	}
}

func TestNormalizeAndPhone(t *testing.T) {
	if Normalize(" tg ") != "TG" || Normalize("Togo") != "" || Normalize("t1") != "" {
		t.Fatal("Normalize")
	}
	if code, ok := ByPhone("+22899000001"); !ok || code != "TG" {
		t.Fatalf("ByPhone = %q,%v", code, ok)
	}
	if _, ok := ByPhone("+33612345678"); ok {
		t.Fatal("France is not in the catalog")
	}
}

// --- LES NUMÉROS DE SECOURS ---------------------------------------------

// ⚠️ UN NUMÉRO DE SECOURS NE S'ÉCRIT PAS EN E.164. `17`, `118`, `1515` sont des
// numéros COURTS : les préfixer de l'indicatif du pays (`+228 17`) les rendrait
// incomposables. Ce test fige la règle parce que la tentation de « normaliser »
// un numéro de téléphone est forte partout ailleurs dans cette base.
func TestAnEmergencyNumberIsNeverInInternationalForm(t *testing.T) {
	for _, c := range Catalog {
		for name, n := range map[string]string{
			"police": c.Emergency.Police, "fire": c.Emergency.Fire,
			"ambulance": c.Emergency.Ambulance,
		} {
			if n == "" {
				continue
			}
			if strings.ContainsAny(n, "+ ") {
				t.Errorf("%s: %s = %q — ni `+` ni espace dans un numéro court", c.Code, name, n)
			}
			for _, r := range n {
				if r < '0' || r > '9' {
					t.Errorf("%s: %s = %q — seulement des chiffres", c.Code, name, n)
					break
				}
			}
			if len(n) > 6 {
				t.Errorf("%s: %s = %q — un numéro de secours est court ; au-delà, "+
					"c'est un numéro ordinaire qui n'a rien à faire ici", c.Code, name, n)
			}
		}
	}
}

// ⚠️ TOUT PAYS DU CATALOGUE A AU MOINS LA POLICE ET LES POMPIERS. Ce sont les
// deux numéros qu'un opérateur appelle vraiment : « il y a eu un accident » et
// « quelqu'un est agressé ». Un pays ouvert sans eux laisserait le service
// client sans rien à composer, et c'est l'écran où il n'y a pas le temps de
// chercher.
//
// ⚠️ L'AMBULANCE, ELLE, PEUT MANQUER — et c'est une réponse, pas un oubli. Les
// sources ne concordent pas pour le Togo, la Guinée ni le Tchad : un numéro
// inventé serait pire que son absence, parce que la console afficherait un
// bouton qui ne mène à rien.
func TestEveryCatalogueCountryHasPoliceAndFire(t *testing.T) {
	for _, c := range Catalog {
		if c.Emergency.Police == "" {
			t.Errorf("%s (%s) : pas de numéro de police", c.Code, c.Name)
		}
		if c.Emergency.Fire == "" {
			t.Errorf("%s (%s) : pas de numéro de pompiers", c.Code, c.Name)
		}
	}
}

// ⚠️ LES PAYS AU PLAN PARTICULIER SONT FIGÉS ICI, un par un. « Police = 17 »
// est le schéma de l'ancienne AOF, et il est faux dans cinq de nos pays : la
// Côte d'Ivoire (111), le Ghana (191), le Nigeria (112), le Gabon (1730) et
// tous ceux passés en 117. Quelqu'un qui « harmoniserait » le catalogue par
// réflexe casserait exactement ces lignes — et ce test est là pour l'arrêter.
func TestTheCountriesWithTheirOwnPlanAreNotHarmonised(t *testing.T) {
	for code, police := range map[string]string{
		"CI": "111", "GH": "191", "NG": "112", "GA": "1730",
		"TG": "117", "BJ": "117", "GN": "117", "CM": "117", "GW": "117",
		"SN": "17", "ML": "17", "NE": "17", "BF": "17", "TD": "17",
	} {
		info, ok := Lookup(code)
		if !ok {
			t.Fatalf("%s a disparu du catalogue", code)
		}
		if info.Emergency.Police != police {
			t.Errorf("%s : police = %q, attendu %q — si le pays a vraiment changé "+
				"de plan, corrigez le test AVEC sa source", code, info.Emergency.Police, police)
		}
	}
}

// `Any` répond à la seule question que la console pose : « ai-je quelque chose
// à composer ? ».
func TestAnySaysWhetherThereIsAnythingToDial(t *testing.T) {
	if (Emergency{}).Any() {
		t.Error("rien à composer doit se dire")
	}
	if !(Emergency{Police: "17"}).Any() {
		t.Error("un seul numéro suffit à afficher la section")
	}
}
