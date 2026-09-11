package user

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// Les trois gestes de la console sur un compte — ouvrir, corriger, supprimer
// — sont trois routes, avec leur verbe. Le module les a toujours eues ; ce
// que ce test épingle, c'est leur forme exacte, celle que la console appelle
// (`corePost("/admin/users")`, `corePatch("/admin/users/{id}")`, `DELETE`).
// Un verbe qui glisse — PUT pour PATCH — laisse un bouton « Éditer » qui
// répond 405 sans qu'aucun test du module ne rougisse.
func TestAdminUsersMountsTheThreeBackOfficeVerbs(t *testing.T) {
	r := chi.NewRouter()
	NewAdminUsers(nil, nil, nil).Mount(r, func(h http.Handler) http.Handler { return h })

	got := map[string]bool{}
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}))
	for _, want := range []string{
		"POST /admin/users",
		"PATCH /admin/users/{id}",
		"DELETE /admin/users/{id}",
	} {
		require.True(t, got[want], "route manquante : %s (montées : %v)", want, got)
	}
	// Contrôle négatif : le statut n'est PAS ici — il a sa propre route dans
	// le handler public, avec sa trace « avant / après ».
	require.False(t, got["PATCH /admin/users/{id}/status"], "le statut ne se change pas par ce module")
	require.Len(t, got, 3, "une route de plus ici est une route que personne n'a décidée")
}
