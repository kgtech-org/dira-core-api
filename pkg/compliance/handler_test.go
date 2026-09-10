package compliance

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// mountAs monte le handler avec les segments d'une verticale, et rend le
// routeur, le chauffeur réel du dépôt, et son véhicule.
func mountAs(t *testing.T, routes Routes, role string) (chi.Router, string) {
	t.Helper()
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	r := chi.NewRouter()
	NewHandler(fx.svc, routes).Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), driver, role)))
		})
	})
	return r, driver
}

// mounted énumère les routes RÉELLEMENT enregistrées.
//
// ⚠️ On interroge le routeur plutôt que d'appeler les URL : un 404 de routage
// et un 404 métier (« chauffeur inconnu ») sont le même code. Un test bâti sur
// les réponses aurait passé au vert en vérifiant la mauvaise chose — c'est
// exactement ce qu'a fait sa première version.
func mounted(t *testing.T, r chi.Router) []string {
	t.Helper()
	var out []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method+" "+strings.TrimSuffix(route, "/"))
		return nil
	}))
	require.NotEmpty(t, out, "un balayage vide ne prouve rien : le handler n'a rien monté")
	sort.Strings(out)
	return out
}

// ⚠️ LE test de ce handler. Les segments viennent de l'APPELANT : un « agent »
// écrit en dur dans la bibliothèque ferait servir aux courses les routes de la
// livraison, et un chauffeur lirait `/driver/documents` dans sa spec pour
// appeler une route qui n'existe pas.
//
// Le contrôle est NÉGATIF des deux côtés : sa propre route est là, celle de
// l'autre verticale est absente. Sans la seconde moitié, un handler qui
// monterait les DEUX passerait le test.
func TestRouteSegmentsComeFromTheVertical(t *testing.T) {
	food, _ := mountAs(t, Routes{Self: "agent", Owners: "agents"}, auth.RoleDriver)
	vtc, _ := mountAs(t, Routes{Self: "driver", Owners: "drivers"}, auth.RoleDriver)

	foodRoutes, vtcRoutes := mounted(t, food), mounted(t, vtc)

	assert.Contains(t, foodRoutes, "GET /agent/documents")
	assert.Contains(t, foodRoutes, "POST /agent/documents")
	assert.NotContains(t, foodRoutes, "GET /driver/documents",
		"la livraison ne sert pas la route des courses")

	assert.Contains(t, vtcRoutes, "GET /driver/documents")
	assert.Contains(t, vtcRoutes, "POST /driver/documents")
	assert.NotContains(t, vtcRoutes, "GET /agent/documents",
		"les courses ne servent pas la route de la livraison")
}

// Le segment d'ADMINISTRATION est un mot DIFFÉRENT — « agents » n'est pas
// « agent ». Le déduire du premier en ajoutant un « s » était la tentation.
func TestAdminSegmentIsSeparate(t *testing.T) {
	food, _ := mountAs(t, Routes{Self: "agent", Owners: "agents"}, auth.RoleAdmin)
	vtc, _ := mountAs(t, Routes{Self: "driver", Owners: "drivers"}, auth.RoleAdmin)

	foodRoutes, vtcRoutes := mounted(t, food), mounted(t, vtc)

	assert.Contains(t, foodRoutes, "GET /admin/agents/{id}/documents")
	assert.NotContains(t, foodRoutes, "GET /admin/drivers/{id}/documents")

	assert.Contains(t, vtcRoutes, "GET /admin/drivers/{id}/documents")
	assert.NotContains(t, vtcRoutes, "GET /admin/agents/{id}/documents")

	// La file et l'arbitrage ne portent AUCUN segment de verticale : ils sont
	// identiques des deux côtés, et ce sont les préfixes de la passerelle
	// (`/food`, `/vtc`) qui les séparent.
	for _, routes := range [][]string{foodRoutes, vtcRoutes} {
		assert.Contains(t, routes, "GET /admin/compliance")
		assert.Contains(t, routes, "PATCH /admin/documents/{id}")
	}
}

// La surface est CLOSE : quatre routes, pas une de plus. Une route ajoutée
// sans y penser — un effacement, une liste globale — apparaîtrait ici.
func TestSurfaceIsExactlyFourRoutes(t *testing.T) {
	vtc, _ := mountAs(t, Routes{Self: "driver", Owners: "drivers"}, auth.RoleAdmin)
	assert.Equal(t, []string{
		"GET /admin/compliance",
		"GET /admin/drivers/{id}/documents",
		"GET /driver/documents",
		"PATCH /admin/documents/{id}",
		"POST /driver/documents",
	}, mounted(t, vtc))
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ⚠️ Le dépôt est réservé au PORTEUR, l'arbitrage à l'ADMINISTRATION. Un
// chauffeur qui validerait ses propres pièces rendrait la file décorative.
func TestRolesAreEnforcedOnEachSide(t *testing.T) {
	asDriver, _ := mountAs(t, Routes{Self: "driver", Owners: "drivers"}, auth.RoleDriver)
	asClient, _ := mountAs(t, Routes{Self: "driver", Owners: "drivers"}, auth.RoleClient)

	assert.Equal(t, http.StatusForbidden,
		call(asDriver, http.MethodPatch, "/admin/documents/"+newAdmin(), `{"status":"valid"}`).Code,
		"un chauffeur n'arbitre pas ses propres pièces")
	assert.Equal(t, http.StatusForbidden,
		call(asDriver, http.MethodGet, "/admin/compliance", "").Code,
		"ni ne lit la file")
	assert.Equal(t, http.StatusForbidden,
		call(asClient, http.MethodGet, "/driver/documents", "").Code,
		"un client n'a pas de pièces")
}

// Le porteur lit son propre état, et la file répond `items` même vide — une
// clé absente et une liste vide ne se lisent pas pareil côté console.
func TestOwnerReadsItsOwnStateAndQueueAlwaysCarriesItems(t *testing.T) {
	driverSide, _ := mountAs(t, Routes{Self: "agent", Owners: "agents"}, auth.RoleDriver)
	own := call(driverSide, http.MethodGet, "/agent/documents", "")
	require.Equal(t, http.StatusOK, own.Code)
	assert.Contains(t, own.Body.String(), `"missing"`, "ce qui manque est NOMMÉ")

	adminSide, _ := mountAs(t, Routes{Self: "agent", Owners: "agents"}, auth.RoleAdmin)
	queue := call(adminSide, http.MethodGet, "/admin/compliance", "")
	require.Equal(t, http.StatusOK, queue.Code)
	assert.Contains(t, queue.Body.String(), `"items"`)
}
