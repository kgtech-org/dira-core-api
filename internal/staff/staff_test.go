package staff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// ⚠️ VIDE = TOUTES, et la même règle vaut dans le jeton. Deux conventions
// inverses — vide=tout ici, vide=rien là-bas — auraient produit un membre
// affiché « accès complet » et refusé partout, sans que rien ne l'explique.
func TestNoScopeMeansEveryScope(t *testing.T) {
	m := &Member{}
	assert.True(t, m.Unrestricted())
	assert.ElementsMatch(t, Scopes, m.EffectiveScopes())

	// Et la même personne, vue par le jeton, passe partout.
	assert.True(t, auth.Claims{Scopes: m.Scopes}.Allows(auth.ScopeFood))
	assert.True(t, auth.Claims{Scopes: m.Scopes}.Allows(auth.ScopeVTC))
}

// ⚠️ LES TROIS PORTÉES SE RÉDUISENT À AUCUNE. Sans cette réduction, ajouter
// une quatrième verticale demain laisserait ces gens DEHORS : ils porteraient
// « toutes » les portées d'hier, pas celles d'aujourd'hui — et la panne
// arriverait des mois après la décision qui l'a causée.
func TestAllScopesCollapseToUnrestricted(t *testing.T) {
	out, err := normaliseScopes([]string{auth.ScopeCore, auth.ScopeFood, auth.ScopeVTC})
	require.NoError(t, err)
	assert.Nil(t, out, "les trois portées se stockent comme aucune restriction")

	partial, err := normaliseScopes([]string{auth.ScopeVTC})
	require.NoError(t, err)
	assert.Equal(t, []string{auth.ScopeVTC}, partial)
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

// ⚠️ LE PIÈGE. Une fiche SUSPENDUE ne doit pas rendre une liste vide : vide
// veut dire « aucune restriction », et une suspension aurait donc transformé
// la personne en administrateur tout-puissant — exactement l'inverse de
// l'intention, et sans que rien ne le signale.
func TestSuspendedMemberDoesNotBecomeUnrestricted(t *testing.T) {
	suspended := &Member{Status: StatusSuspended, Scopes: []string{auth.ScopeVTC}}
	// ⚠️ On appelle la VRAIE règle, pas des claims fabriqués : un test écrit
	// sur des valeurs saisies à la main aurait continué de passer si cette
	// décision changeait.
	got := scopesFor(suspended)
	require.NotEmpty(t, got, "une liste vide voudrait dire « aucune restriction »")
	claims := auth.Claims{Scopes: got}
	assert.False(t, claims.Allows(auth.ScopeVTC), "une fiche suspendue n'ouvre plus rien")
	assert.False(t, claims.Allows(auth.ScopeFood))
	assert.False(t, claims.Allows(auth.ScopeCore))

	// Et le membre lui-même reste lisible : la console doit pouvoir montrer
	// le périmètre qu'il AVAIT, sinon on ne sait pas quoi rétablir.
	assert.Equal(t, []string{auth.ScopeVTC}, suspended.EffectiveScopes())

	// Les deux autres cas de la même règle, sur la même fonction.
	assert.Nil(t, scopesFor(nil), "pas de fiche de staff : aucune restriction")
	assert.Equal(t, []string{auth.ScopeVTC},
		scopesFor(&Member{Status: StatusActive, Scopes: []string{auth.ScopeVTC}}))
}

// La réponse dit EXPLICITEMENT « aucune restriction » plutôt que de laisser la
// console déduire d'une liste pleine : les deux se ressemblent à l'écran et ne
// se modifient pas pareil.
func TestResponseStatesUnrestrictedExplicitly(t *testing.T) {
	full := toResponse(&Member{})
	assert.True(t, full.Unrestricted)
	assert.ElementsMatch(t, Scopes, full.Scopes)

	scoped := toResponse(&Member{Scopes: []string{auth.ScopeFood}})
	assert.False(t, scoped.Unrestricted)
	assert.Equal(t, []string{auth.ScopeFood}, scoped.Scopes)
}
