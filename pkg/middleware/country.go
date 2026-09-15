package middleware

import (
	"net/http"
	"strings"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Installed dit quels pays ce déploiement SERT, et lequel il sert par
// défaut. Le socle le lit en base (la liste réglée depuis la console) ; une
// verticale se contente du catalogue et du pays par défaut de son
// environnement — voir `country.Catalogued`.
type Installed interface {
	Enabled(code string) bool
	Default() string
}

// Country pose le PAYS EFFECTIF d'une requête dans le contexte.
//
// La règle, dans l'ordre :
//
//  1. Un compte ordinaire opère dans le pays de son COMPTE, lu dans le jeton.
//     L'en-tête `X-Dira-Country` ne le change pas : un client ne se
//     téléporte pas en changeant un en-tête. La réponse porte le pays retenu
//     dans le même en-tête, pour que l'application s'aligne.
//  2. Un compte qui a le droit de changer de pays (`cty_any` — la direction,
//     sur la console) opère dans le pays de l'en-tête s'il est servi, sinon
//     dans celui de son compte.
//  3. Sans jeton — inscription, connexion, catalogue public — ou avec un
//     jeton d'avant la couche pays, l'en-tête est admis s'il nomme un pays
//     servi.
//  4. Sinon, le pays par défaut du déploiement.
//
// ⚠️ À MONTER GLOBALEMENT, avec le vérificateur de jetons. Le pays se lit
// dans le JETON même sur une route publique — `GET /vtc/cities`, le
// catalogue — quand l'application en présente un : sans cela, un client
// connecté qui pose un en-tête `SN` sur une route sans `Auth` verrait les
// villes du Sénégal, et pas celles de son compte. Le jeton est vérifié ici
// pour le pays seulement ; `Auth` reste ce qui autorise. Un jeton invalide
// est ignoré à cet étage — c'est `Auth` qui le refusera, là où il compte.
//
// Un en-tête qui ne nomme aucun pays servi est IGNORÉ, pas refusé : refuser
// aurait coupé l'inscription d'un voyageur dont le téléphone dit « FR », alors
// que le pays par défaut le sert très bien ; et la réponse lui dit dans quel
// pays il a été inscrit.
func Country(installed Installed, tokens *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claimed, any := auth.CountryFromContext(ctx)
			if _, has := auth.UserFromContext(ctx); !has && tokens != nil {
				if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && bearer != "" {
					if c, err := tokens.Verify(bearer); err == nil && c.Type == auth.TokenTypeAccess {
						claimed, any = c.Country, c.CountryAny
					}
				}
			}
			asked := country.Normalize(r.Header.Get(country.Header))
			if asked == "" {
				asked = country.Normalize(r.URL.Query().Get(country.QueryParam))
			}
			if asked != "" && !installed.Enabled(asked) {
				asked = ""
			}

			code, source := installed.Default(), country.SourceDefault
			switch {
			case claimed != "" && !any:
				code, source = claimed, country.SourceClaims
			case asked != "":
				code, source = asked, country.SourceHeader
			case claimed != "":
				code, source = claimed, country.SourceClaims
			}
			w.Header().Set(country.Header, code)
			next.ServeHTTP(w, r.WithContext(country.WithCountry(ctx, code, source)))
		})
	}
}
