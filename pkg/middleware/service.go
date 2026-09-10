package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Service guards routes called by ANOTHER SERVICE, never by a person.
//
// Le jeton est un secret partagé entre deux services, comparé en TEMPS
// CONSTANT : une comparaison ordinaire s'arrête au premier octet différent, et
// le temps de réponse laisse deviner le secret octet par octet.
//
// ⚠️ SECRET VIDE = ROUTES FERMÉES, jamais ouvertes. Une porte de service sans
// serrure vaut moins que pas de porte : elle donne les pouvoirs d'un service —
// débiter un portefeuille, déposer une note au nom d'un client — à quiconque
// connaît le chemin. Un déploiement qui oublie la variable doit tomber en
// panne bruyamment, pas s'ouvrir en silence.
func Service(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				httpx.Error(w, r, apperr.Unauthorized("service_auth_disabled",
					"service-to-service routes are closed: no service token configured"))
				return
			}
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(raw), []byte(token)) != 1 {
				httpx.Error(w, r, apperr.Unauthorized("invalid_service_token",
					"invalid service token"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
