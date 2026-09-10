package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ VIDE = AUCUNE, littéralement. C'est la lecture naturelle d'une liste de
// permissions, et c'est ce qui permet de suspendre quelqu'un simplement en
// vidant sa liste — sans valeur sentinelle, sans cas particulier.
//
// La convention inverse a existé ici : elle exigeait une portée « suspended »
// qu'aucune route ne demandait, et une phrase d'avertissement dans l'interface
// pour que des cases vides ne se lisent pas comme « aucun accès ».
func TestNoScopeGrantsNothing(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.GenerateAccess("u1", RoleAdmin)
	require.NoError(t, err)

	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.Empty(t, c.Scopes)
	for _, sc := range AllScopes {
		assert.False(t, c.Allows(sc), "une liste vide n'accorde pas %q", sc)
	}
}

// Couvrir TOUTE la plateforme s'écrit explicitement : les trois portées.
//
// ⚠️ Pas de valeur « all » séparée. Elle aurait fait deux façons d'exprimer la
// même chose, et le jour où une quatrième verticale apparaît, l'une des deux
// serait devenue fausse en silence — « all » aurait continué de désigner les
// trois d'hier.
func TestFullCoverageIsWrittenOut(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.GenerateAccess("u1", RoleAdmin, AllScopes...)
	require.NoError(t, err)
	c, err := m.Verify(tok)
	require.NoError(t, err)
	for _, sc := range AllScopes {
		assert.True(t, c.Allows(sc))
	}
}

// Une portée déclarée RESTREINT — sinon elle ne sert à rien.
func TestDeclaredScopeRestricts(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.GenerateAccess("u1", RoleAdmin, ScopeVTC)
	require.NoError(t, err)

	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, []string{ScopeVTC}, c.Scopes)
	assert.True(t, c.Allows(ScopeVTC))
	assert.False(t, c.Allows(ScopeFood), "un chargé des courses n'administre pas la livraison")
	assert.False(t, c.Allows(ScopeCore))
}

// Plusieurs portées cohabitent : quelqu'un peut couvrir deux métiers sans
// couvrir le troisième.
func TestSeveralScopes(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.GenerateAccess("u1", RoleAdmin, ScopeFood, ScopeVTC)
	require.NoError(t, err)
	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.True(t, c.Allows(ScopeFood))
	assert.True(t, c.Allows(ScopeVTC))
	assert.False(t, c.Allows(ScopeCore))
}

// ⚠️ La portée survit à l'ALLER-RETOUR par le contexte. Si elle s'arrêtait au
// jeton, le garde de route lirait une liste vide et laisserait tout passer —
// une restriction qui s'évapore en silence est pire qu'une absence de
// restriction, parce qu'on croit être protégé.
func TestScopeSurvivesTheContext(t *testing.T) {
	ctx := WithClaims(t.Context(), Claims{UserID: "u1", Role: RoleAdmin, Scopes: []string{ScopeVTC}})
	assert.Equal(t, []string{ScopeVTC}, ScopesFromContext(ctx))
	assert.True(t, AllowsFromContext(ctx, ScopeVTC))
	assert.False(t, AllowsFromContext(ctx, ScopeFood))

	// Et l'identité passe toujours : la portée ne remplace pas le porteur.
	id, ok := UserFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "u1", id)
	role, ok := RoleFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, RoleAdmin, role)
}
