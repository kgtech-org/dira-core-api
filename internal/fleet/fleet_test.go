package fleet

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

func bp(v int) *int { return &v }

// ⚠️ LE TEST QUI COMPTE. `nil` et `0` ne veulent pas dire la même chose : nil
// signifie « cette flotte suit le taux de la plateforme », zéro signifie « une
// gratuité a été négociée ». Les confondre ferait travailler gratuitement
// toute flotte enregistrée sans taux — une perte sèche que rien ne signale,
// parce qu'un zéro est une valeur parfaitement valide.
func TestCommissionDistinguishesUnsetFromZero(t *testing.T) {
	assert.NoError(t, checkCommission(nil), "non renseigné : la flotte suivra le taux de la plateforme")
	assert.NoError(t, checkCommission(bp(0)), "zéro est une gratuité NÉGOCIÉE, pas une absence")
	assert.NoError(t, checkCommission(bp(10000)))

	// Et le pointeur le PORTE jusqu'à la réponse : un `int` nu aurait rendu
	// les deux cas identiques à la lecture.
	unset := toResponse(&Fleet{})
	assert.Nil(t, unset.CommissionBp, "non renseigné doit rester distinguable à la lecture")
	free := toResponse(&Fleet{CommissionBp: bp(0)})
	require.NotNil(t, free.CommissionBp)
	assert.Equal(t, 0, *free.CommissionBp)
}

// Un taux hors bornes est un taux qui ne veut rien dire. 12 000 dix-millièmes
// prélèveraient 120 % d'une course.
func TestCommissionIsBounded(t *testing.T) {
	for _, v := range []int{-1, 10001, 999999} {
		err := checkCommission(bp(v))
		require.Error(t, err, "%d devrait être refusé", v)
		assert.Equal(t, "validation_failed", apperr.From(err).Code)
	}
}

// ⚠️ La recherche est une entrée NON FIABLE. Sans échappement, un nom
// contenant une parenthèse fait échouer la requête, et `.*` parcourt la
// collection entière.
func TestSearchIsEscaped(t *testing.T) {
	assert.Equal(t, `Sodigaz \(Lomé\)`, regexEscape("Sodigaz (Lomé)"))
	assert.Equal(t, `\.\*`, regexEscape(".*"))
	assert.Equal(t, `Transports Kodjo`, regexEscape("Transports Kodjo"), "un nom ordinaire n'est pas altéré")
}

// La réponse porte l'identifiant du gérant quand il existe, et RIEN quand il
// n'existe pas — une chaîne vide dans un champ d'identifiant se lit comme un
// compte introuvable, alors que la flotte n'en a simplement pas.
func TestOwnerIsOmittedWhenAbsent(t *testing.T) {
	assert.Empty(t, toResponse(&Fleet{}).OwnerUserID)
}

// --- LA SURFACE PARTENAIRE ----------------------------------------------

// ⚠️⚠️ LE TEST QUI PORTE TOUT L'ISOLEMENT : LA SURFACE PARTENAIRE N'A AUCUN
// IDENTIFIANT DANS SES CHEMINS.
//
// C'est structurel, pas documentaire. Un `GET /partner/fleets/{id}` aurait
// suffi à un partenaire curieux pour lire le parc d'un concurrent en changeant
// un chiffre dans l'URL — et un garde oublié une seule fois, sur une seule
// route, suffit. En n'offrant aucune route paramétrée, il n'y a rien à essayer.
func TestThePartnerSurfaceHasNoIdentifierToTamperWith(t *testing.T) {
	routes := mountedPartnerRoutes(t)
	require.NotEmpty(t, routes, "la surface partenaire existe")
	for _, r := range routes {
		assert.NotContains(t, r, "{", "aucun paramètre de chemin : %s", r)
	}
	assert.Equal(t, []string{"GET /partner/me"}, routes)
}

// mountedPartnerRoutes liste les routes `/partner/...` servies par ce module.
func mountedPartnerRoutes(t *testing.T) []string {
	t.Helper()
	r := chi.NewRouter()
	NewHandler(&Service{}).Mount(r, func(next http.Handler) http.Handler { return next })
	var out []string
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/partner/") {
			out = append(out, method+" "+strings.TrimSuffix(route, "/"))
		}
		return nil
	})
	sort.Strings(out)
	return out
}
