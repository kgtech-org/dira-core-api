package country

import "testing"

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
