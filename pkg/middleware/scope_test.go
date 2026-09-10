package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// ⚠️ LE CONTRÔLE DE BOUT EN BOUT : du jeton signé jusqu'au refus HTTP. Les
// tests de `pkg/auth` prouvent que la portée est écrite et relue ; celui-ci
// prouve qu'elle ARRÊTE une requête. Sans lui, la chaîne pourrait être juste
// à chaque maillon et cassée entre deux.
func TestRequireScopeBlocksOutOfScopeStaff(t *testing.T) {
	m := auth.NewManager("s3cret", time.Minute, time.Hour)
	guarded := func(scope string) http.Handler {
		return middleware.Auth(m)(middleware.RequireScope(scope)(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))
	}
	call := func(h http.Handler, token string) int {
		req := httptest.NewRequest(http.MethodGet, "/admin/anything", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	vtcOnly, err := m.GenerateAccess("u1", auth.RoleAdmin, auth.ScopeVTC)
	require.NoError(t, err)
	unrestricted, err := m.GenerateAccess("u2", auth.RoleAdmin)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, call(guarded(auth.ScopeVTC), vtcOnly))
	assert.Equal(t, http.StatusForbidden, call(guarded(auth.ScopeFood), vtcOnly),
		"un chargé des courses ne doit pas administrer la livraison")
	assert.Equal(t, http.StatusOK, call(guarded(auth.ScopeFood), unrestricted),
		"un jeton sans portée garde l'accès qu'il avait")
}
