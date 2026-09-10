package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ LE TEST DE RÉTROCOMPATIBILITÉ. Un jeton émis avant l'existence des
// portées n'en porte aucune, et doit garder l'accès qu'il avait.
//
// L'inverse — vide = aucune portée — aurait coupé l'accès de chaque
// administrateur connecté à l'instant du déploiement, y compris celui qui
// aurait dû corriger la situation.
func TestNoScopeMeansEveryScope(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.GenerateAccess("u1", RoleAdmin)
	require.NoError(t, err)

	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.Empty(t, c.Scopes, "aucune portée n'est écrite dans le jeton")
	for _, sc := range []string{ScopeCore, ScopeFood, ScopeVTC} {
		assert.True(t, c.Allows(sc), "un jeton sans portée doit couvrir %q", sc)
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
