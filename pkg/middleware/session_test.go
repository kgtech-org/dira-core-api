package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
	"github.com/kgtech-org/dira-core-api/pkg/session"
)

// stubGuard répond ce qu'on lui dit : l'appareil courant du compte.
type stubGuard struct {
	holder session.Device
	known  bool
	calls  int
}

func (s *stubGuard) Accepts(_ context.Context, _, deviceID string) (bool, session.Device) {
	s.calls++
	if !s.known || s.holder.ID == deviceID {
		return true, s.holder
	}
	return false, s.holder
}

func driverToken(t *testing.T, m *auth.Manager, device string) string {
	t.Helper()
	tok, err := m.Issue(auth.Grant{UserID: "abc", Role: auth.RoleDriver, Device: device})
	require.NoError(t, err)
	return tok
}

func callWith(t *testing.T, mw func(http.Handler) http.Handler, token string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	var superseded bool
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		superseded = middleware.SupersededSession(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, superseded
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (code, reason string) {
	t.Helper()
	var body struct {
		Error struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code, body.Error.Reason
}

// ⚠️ SANS CE GARDE, LE SOCLE NE SAURAIT PAS CHASSER PERSONNE. Un jeton d'accès
// est apatride : le révoquer en base ne l'empêche pas d'être accepté par les
// six services jusqu'à son expiration. C'est ici, et dans le suivi, que la
// révocation devient réelle.
func TestAChasedDeviceIsRefusedWithItsOwnCode(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{holder: session.Device{ID: "install-B", Name: "Itel A70"}, known: true}
	mw := middleware.Auth(m, middleware.WithSessions(guard))

	rec, _ := callWith(t, mw, driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	code, reason := errorCode(t, rec)
	assert.Equal(t, "session_superseded", code)
	// ⚠️ `reason` porte le NOM de l'appareil qui a pris la place : c'est ce qui
	// permet à l'écran de dire « vous vous êtes connecté sur Itel A70 » au lieu
	// d'« erreur de connexion ».
	assert.Equal(t, "Itel A70", reason)
}

// 401 et non 403, délibérément : les applications ont déjà un intercepteur sur
// 401 qui tente un rafraîchissement puis déconnecte. Ce chemin-là mène au bon
// endroit — le rafraîchissement répond le MÊME code — et une application pas
// encore mise à jour se comporte donc correctement, juste sans la bonne phrase.
func TestTheRefusalKeepsTheStatusAppsAlreadyHandle(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{holder: session.Device{ID: "install-B"}, known: true}
	rec, _ := callWith(t, middleware.Auth(m, middleware.WithSessions(guard)), driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTheDeviceThatHoldsTheSessionPasses(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{holder: session.Device{ID: "install-A"}, known: true}
	rec, superseded := callWith(t, middleware.Auth(m, middleware.WithSessions(guard)), driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, superseded)
}

// ⚠️ AU MIEUX, TOUJOURS DANS LE SENS DE LAISSER TRAVAILLER. Registre injoignable
// ou base Redis vidée : on ne sait plus qui détient la session. Refuser sur une
// ignorance aurait déconnecté TOUS les chauffeurs de la plateforme au premier
// redémarrage de Redis, pour un problème qui concerne un chauffeur sur mille.
func TestAnUnknownRegistryRefusesNobody(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{known: false}
	rec, _ := callWith(t, middleware.Auth(m, middleware.WithSessions(guard)), driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Un jeton sans appareil n'est comparé à rien : le registre n'est même pas
// consulté. C'est le cas d'un client, d'un marchand, du staff — et d'un jeton
// émis avant ce mécanisme.
func TestATokenWithoutADeviceNeverReachesTheRegistry(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{holder: session.Device{ID: "install-B"}, known: true}
	tok, err := m.Issue(auth.Grant{UserID: "abc", Role: auth.RoleClient})
	require.NoError(t, err)
	rec, _ := callWith(t, middleware.Auth(m, middleware.WithSessions(guard)), tok)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, guard.calls, "un compte sans règle d'appareil ne coûte pas une lecture Redis par requête")
}

// ⚠️ LE RATTRAPAGE HORS LIGNE PASSE, ET LUI SEUL. Un chauffeur chassé peut avoir
// dix courses faites sans réseau, jamais remontées : refuser sa
// resynchronisation ferait PERDRE ces courses — donc son argent — pour
// appliquer une règle dont l'objet est d'éviter deux flux de positions.
// Raconter le passé ne crée aucun second flux.
func TestTheOfflineReplayToleratesAChasedDevice(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	guard := &stubGuard{holder: session.Device{ID: "install-B", Name: "Itel A70"}, known: true}
	mw := middleware.Auth(m, middleware.WithSessions(guard), middleware.TolerateSupersededSession())

	rec, superseded := callWith(t, mw, driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, superseded, "la route doit POUVOIR savoir qu'elle sert un appareil chassé")
}

// Sans registre branché, rien ne change : c'est l'état d'un déploiement où Redis
// n'est pas partagé, et le service doit y tourner comme avant.
func TestWithoutARegistryNothingChanges(t *testing.T) {
	m := auth.NewManager("secret", 15*time.Minute, time.Hour)
	rec, _ := callWith(t, middleware.Auth(m), driverToken(t, m, "install-A"))
	assert.Equal(t, http.StatusOK, rec.Code)
}
