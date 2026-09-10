package staff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// ⚠️ VIDE = AUCUNE, littéralement — ici comme dans le jeton.
//
// C'est la lecture naturelle d'une liste de permissions, et c'est elle qui
// rend tout le reste simple : suspendre quelqu'un consiste à vider sa liste,
// sans valeur sentinelle et sans cas particulier.
//
// La convention inverse a existé ici pour ne casser aucun jeton en vol. Elle
// coûtait trois mécanismes de compensation : une portée « suspended »
// qu'aucune route ne demandait, une méthode traduisant « vide » en « toutes »,
// et une phrase d'avertissement dans l'interface pour que des cases à cocher
// vides ne se lisent pas comme « aucun accès ».
func TestEmptyScopesGrantNothing(t *testing.T) {
	m := &Member{}
	assert.False(t, m.CoversEverything())
	for _, sc := range Scopes {
		assert.False(t, auth.Claims{Scopes: m.Scopes}.Allows(sc), "une liste vide n'accorde pas %q", sc)
	}
}

// Une fiche ACTIVE doit porter au moins une portée. Une fiche qui n'accorde
// rien n'est pas une fiche : c'est une ligne qui fait croire à une
// habilitation.
func TestAtLeastOneScopeIsRequired(t *testing.T) {
	_, err := normaliseScopes(nil)
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)

	_, err = normaliseScopes([]string{"  ", ""})
	require.Error(t, err, "des chaînes vides ne font pas une portée")
}

// ⚠️ COUVRIR TOUT S'ÉCRIT EN TROIS PORTÉES, et se stocke ainsi.
//
// Aucune réduction vers une valeur « toutes » : elle aurait fait deux façons
// d'exprimer la même chose, et le jour où une quatrième verticale apparaît,
// cette valeur aurait continué de désigner les trois d'hier — sans que rien ne
// le signale. Écrire la liste, c'est écrire une date.
func TestFullCoverageIsStoredAsThreeScopes(t *testing.T) {
	out, err := normaliseScopes([]string{auth.ScopeCore, auth.ScopeFood, auth.ScopeVTC})
	require.NoError(t, err)
	assert.Len(t, out, 3, "les trois portées se stockent telles quelles")

	full := &Member{Scopes: out}
	assert.True(t, full.CoversEverything())
	partial := &Member{Scopes: []string{auth.ScopeVTC}}
	assert.False(t, partial.CoversEverything())
}

// Les doublons et la casse ne créent pas de périmètres différents.
func TestScopesAreNormalised(t *testing.T) {
	out, err := normaliseScopes([]string{"VTC", "vtc", " food "})
	require.NoError(t, err)
	assert.Equal(t, []string{auth.ScopeVTC, auth.ScopeFood}, out)
}

// Une portée inconnue est REFUSÉE. L'accepter en silence produirait une fiche
// qui n'autorise rien, puisque aucune route ne demande jamais « vtcc ».
func TestUnknownScopeIsRefused(t *testing.T) {
	_, err := normaliseScopes([]string{"vtcc"})
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
}

// ⚠️ Les TROIS cas de `scopesFor` rendent une liste vide, qui n'accorde rien.
//
// C'est ce que la convention littérale achète : suspendre quelqu'un, retirer
// sa fiche, ou ne jamais lui en donner produisent le même résultat sûr, sans
// valeur magique à retenir.
func TestScopesForCoversTheThreeCases(t *testing.T) {
	assert.Nil(t, scopesFor(nil), "pas de fiche : n'administre rien")
	assert.Nil(t, scopesFor(&Member{Status: StatusSuspended, Scopes: []string{auth.ScopeVTC}}),
		"fiche suspendue : habilitations retirées")
	assert.Equal(t, []string{auth.ScopeVTC},
		scopesFor(&Member{Status: StatusActive, Scopes: []string{auth.ScopeVTC}}))

	// Et une fiche suspendue reste LISIBLE : la console doit pouvoir montrer
	// le périmètre qu'elle rendra, sinon on ne sait pas quoi rétablir.
	suspended := &Member{Status: StatusSuspended, Scopes: []string{auth.ScopeVTC}}
	assert.Equal(t, []string{auth.ScopeVTC}, suspended.Scopes)
}

// La réponse porte la liste ET le résumé calculé. `scopes` reste la seule
// vérité — c'est lui qu'on modifie.
func TestResponseCarriesScopesAndSummary(t *testing.T) {
	full := toResponse(&Member{Scopes: []string{auth.ScopeCore, auth.ScopeFood, auth.ScopeVTC}})
	assert.True(t, full.CoversEverything)
	assert.Len(t, full.Scopes, 3)

	scoped := toResponse(&Member{Scopes: []string{auth.ScopeFood}})
	assert.False(t, scoped.CoversEverything)
	assert.Equal(t, []string{auth.ScopeFood}, scoped.Scopes)
}

// ⚠️ LIRE L'ÉQUIPE ET LA MODIFIER NE SONT PAS LE MÊME GESTE.
//
// Sans cette distinction, un chargé de support pouvait s'attribuer toutes les
// portées : le périmètre ne bornait alors plus personne, puisque tout le monde
// pouvait se le retirer. Une habilitation qu'on peut s'accorder soi-même n'en
// est pas une.
func TestOnlyDirectionMayChangeTheTeam(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member *Member
		allow  bool
	}{
		{"direction active", &Member{Function: FunctionAdmin, Status: StatusActive}, true},
		{"support", &Member{Function: FunctionSupport, Status: StatusActive}, false},
		{"exploitation", &Member{Function: FunctionOps, Status: StatusActive}, false},
		{"direction SUSPENDUE", &Member{Function: FunctionAdmin, Status: StatusSuspended}, false},
		{"aucune fiche", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allow, mayChangeTeam(tc.member))
		})
	}
}
