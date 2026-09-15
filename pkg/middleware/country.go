package middleware

import (
	"net/http"

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
// ⚠️ À MONTER DEUX FOIS : globalement, pour les routes publiques, et DANS la
// chaîne d'authentification, après `Auth`, pour que le jeton soit lu. La
// première pose n'a pas les claims ; la seconde la remplace. Une seule pose
// globale aurait laissé tout compte connecté au pays de son en-tête — donc
// au choix de l'application, pas du serveur.
//
// Un en-tête qui ne nomme aucun pays servi est IGNORÉ, pas refusé : refuser
// aurait coupé l'inscription d'un voyageur dont le téléphone dit « FR », alors
// que le pays par défaut le sert très bien ; et la réponse lui dit dans quel
// pays il a été inscrit.
func Country(installed Installed) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			asked := country.Normalize(r.Header.Get(country.Header))
			if asked == "" {
				asked = country.Normalize(r.URL.Query().Get(country.QueryParam))
			}
			if asked != "" && !installed.Enabled(asked) {
				asked = ""
			}
			claimed, any := auth.CountryFromContext(ctx)

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
