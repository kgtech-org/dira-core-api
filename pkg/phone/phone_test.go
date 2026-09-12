package phone

import "testing"

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"+22899000001":       "+22899000001",
		" +228 99 00 00 01 ": "+22899000001",
		"+228-99.00(00)01":   "+22899000001",
		"0022899000001":      "+22899000001",
		"+33612345678":       "+33612345678",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v ; attendu %q", in, got, err, want)
		}
	}
	bad := []string{"", "22899000001", "99000001", "+0228990000", "+228", "+228 99 00 00 01 x", "+2289900000112345"}
	for _, in := range bad {
		if _, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) devait échouer", in)
		}
	}
}
