package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

func TestCountry(t *testing.T) {
	m := auth.NewManager("secret", time.Minute, time.Hour)
	mint := func(g auth.Grant) string {
		tok, err := m.Issue(g)
		if err != nil {
			t.Fatal(err)
		}
		return "Bearer " + tok
	}
	client := mint(auth.Grant{UserID: "c", Role: auth.RoleClient, Country: "TG"})
	direction := mint(auth.Grant{UserID: "d", Role: auth.RoleAdmin, Country: "TG", CountryAny: true})
	legacy := mint(auth.Grant{UserID: "l", Role: auth.RoleClient})

	var seen string
	var source country.Source
	h := chainCountry(m, country.Catalogued{DefaultCode: "tg"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, source = country.FromContext(r.Context()), country.SourceFromContext(r.Context())
	}))

	cases := []struct {
		name, bearer, header, query string
		want                        string
		source                      country.Source
	}{
		{"no token, no header", "", "", "", "TG", country.SourceDefault},
		{"no token, header", "", "bj", "", "BJ", country.SourceHeader},
		{"no token, unknown header", "", "FR", "", "TG", country.SourceDefault},
		{"no token, query", "", "", "CI", "CI", country.SourceHeader},
		{"client ignores header", client, "BJ", "", "TG", country.SourceClaims},
		{"direction follows header", direction, "BJ", "", "BJ", country.SourceHeader},
		{"direction without header", direction, "", "", "TG", country.SourceClaims},
		{"direction unknown header", direction, "ZZ", "", "TG", country.SourceClaims},
		{"legacy token uses header", legacy, "BJ", "", "BJ", country.SourceHeader},
		{"legacy token default", legacy, "", "", "TG", country.SourceDefault},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x?country="+c.query, nil)
		if c.bearer != "" {
			req.Header.Set("Authorization", c.bearer)
		}
		if c.header != "" {
			req.Header.Set(country.Header, c.header)
		}
		rec := httptest.NewRecorder()
		seen, source = "", ""
		h.ServeHTTP(rec, req)
		if seen != c.want || source != c.source {
			t.Errorf("%s: got %q (%s), want %q (%s)", c.name, seen, source, c.want, c.source)
		}
		if got := rec.Header().Get(country.Header); got != c.want {
			t.Errorf("%s: response header %q, want %q", c.name, got, c.want)
		}
	}
}

// chainCountry monte le middleware comme un service le fait : une fois
// avant l'authentification, une fois après.
func chainCountry(m *auth.Manager, inst Installed, next http.Handler) http.Handler {
	authed := Auth(m)(Country(inst)(next))
	public := Country(inst)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			authed.ServeHTTP(w, r)
			return
		}
		public.ServeHTTP(w, r)
	})
}
