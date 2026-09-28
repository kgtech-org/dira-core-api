package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ LE CONTEXTE ÉCRIT DANS CHAQUE LIGNE, SANS TOUCHER L'APPELANT. C'est ce
// qui transforme six flux de journaux séparés en un seul récit — et ce qui
// évite d'aller ajouter « et aussi l'identifiant de requête » à des centaines
// d'appels existants, dont neuf cents auraient été oubliés.
func TestEveryLineCarriesWhatMakesItRecoupable(t *testing.T) {
	var buf bytes.Buffer
	h := contextHandler{
		Handler: slog.NewJSONHandler(&buf, nil),
		keys:    []ctxKey{KeyRequest, KeyCountry, KeyUser, KeyRoute},
	}
	log := slog.New(h).With("service", "vtc")

	ctx := WithField(context.Background(), KeyRequest, "abc123")
	ctx = WithField(ctx, KeyCountry, "TG")
	ctx = WithField(ctx, KeyUser, "6ab8")
	log.InfoContext(ctx, "course créée")

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "abc123", got["request_id"])
	assert.Equal(t, "TG", got["country"])
	assert.Equal(t, "6ab8", got["user_id"])
	assert.Equal(t, "vtc", got["service"])
	assert.Equal(t, "course créée", got["msg"])
}

// Un champ absent ne s'écrit pas : une ligne pleine de `""` se lit moins bien
// qu'une ligne courte, et un filtre sur « pays vide » ramènerait tout.
func TestAnAbsentFieldIsNotWritten(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(contextHandler{Handler: slog.NewJSONHandler(&buf, nil), keys: []ctxKey{KeyRequest, KeyCountry}})
	log.InfoContext(context.Background(), "sans contexte")
	assert.NotContains(t, buf.String(), "request_id")
	assert.NotContains(t, buf.String(), "country")
}

// ⚠️ LE GABARIT DE ROUTE, JAMAIS LE CHEMIN. `/rides/6ab8…` créerait une série
// temporelle par course — quelques milliers en un après-midi, et la base de
// mesures tombe. C'est la faute classique, et elle ne se voit qu'une fois le
// mal fait.
func TestMetricsCountTheRoutePatternNotThePath(t *testing.T) {
	m := NewMetrics("vtc", "test")
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/rides/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	for _, id := range []string{"6ab8", "6ac1", "6ad2"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rides/"+id, nil))
	}

	assert.Equal(t, 3.0, testutil.ToFloat64(m.requests.WithLabelValues("vtc", "GET", "/rides/{id}", "200")))
	// Et surtout : AUCUNE série par identifiant.
	has(t, m, `route="/rides/{id}"`)
	hasNot(t, m, "6ab8")
}

// Une requête qui ne trouve aucune route est comptée sous un nom FIXE :
// reprendre le chemin demandé laisserait n'importe qui créer autant de séries
// qu'il veut en tapant des URL au hasard.
func TestAnUnmatchedRequestCannotCreateSeriesAtWill(t *testing.T) {
	m := NewMetrics("core", "test")
	r := chi.NewRouter()
	r.Use(m.Middleware)
	// ⚠️ UNE ROUTE, AU MOINS. Un routeur chi sans aucune route ne monte pas
	// sa chaîne de middlewares : il répond 404 directement, et la mesure ne
	// verrait jamais passer la requête. Le vrai service en a des centaines ;
	// le test doit ressembler au vrai service.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	for _, path := range []string{"/n-importe-quoi", "/autre-chose", "/encore-un"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	has(t, m, `route="unmatched"`)
	hasNot(t, m, "n-importe-quoi")
}

// La VERSION est une mesure : sans elle, un tableau de bord ne sait pas quelle
// version tournait au moment du pic — la première question posée après un
// déploiement.
func TestTheRunningVersionIsMeasurable(t *testing.T) {
	has(t, NewMetrics("food", "v1.2.3"), `dira_build_info{service="food",version="v1.2.3"} 1`)
}

// ⚠️ LE FIL SE RECOPIE D'UN SERVICE À L'AUTRE. Sans cela, l'appel sortant naît
// sans passé : le service d'en face lui donne un identifiant tout neuf, et les
// deux moitiés de la même histoire ne se retrouvent jamais.
func TestTheThreadIsCopiedOntoOutboundCalls(t *testing.T) {
	ctx := WithField(WithField(context.Background(), KeyRequest, "fil-1"), KeyCountry, "SN")
	req := Propagate(ctx, httptest.NewRequest(http.MethodGet, "http://core/wallets", nil))
	assert.Equal(t, "fil-1", req.Header.Get(HeaderRequestID))
	assert.Equal(t, "SN", req.Header.Get("X-Dira-Country"))
}

// Sans fil dans le contexte, rien n'est posé : un en-tête vide vaudrait un
// identifiant vide côté serveur, et masquerait celui qu'il aurait tiré.
func TestWithoutAThreadNothingIsSet(t *testing.T) {
	req := Propagate(context.Background(), httptest.NewRequest(http.MethodGet, "http://core/x", nil))
	assert.Empty(t, req.Header.Get(HeaderRequestID))
}

// ⚠️ LA MÊME PANNE SE GROUPE. « Course 6ab8… introuvable » et « course 6ac1…
// introuvable » sont le MÊME défaut : les séparer rendrait la liste des pannes
// illisible au bout d'une heure.
func TestTheSameFaultGroupsAcrossOccurrences(t *testing.T) {
	a := Fault{Service: "vtc", Kind: "http_5xx", Method: "GET", Route: "/rides/{id}",
		Message: "ride 6ab816e4d6107b53ccbb3556 not found"}
	b := a
	b.Message = "ride 6ac91f22a8901c04dd7e1002 not found"
	assert.Equal(t, fingerprint(a), fingerprint(b))
}

// Mais DEUX défauts différents ne se confondent pas : grouper trop large
// cacherait la panne du jour derrière celle de la semaine dernière.
func TestTwoDifferentFaultsDoNotGroup(t *testing.T) {
	a := Fault{Service: "vtc", Kind: "http_5xx", Route: "/rides/{id}", Message: "ride not found"}
	b := Fault{Service: "vtc", Kind: "http_5xx", Route: "/rides/{id}", Message: "wallet unreachable"}
	assert.NotEqual(t, fingerprint(a), fingerprint(b))
	c := a
	c.Service = "food"
	assert.NotEqual(t, fingerprint(a), fingerprint(c))
}

// Un rangement absent ne fait rien tomber : une supervision qui casse ce
// qu'elle observe est pire que pas de supervision — on la coupe, et on est
// aveugle pour de bon.
func TestWithoutASinkCaptureIsHarmless(t *testing.T) {
	m := NewMetrics("core", "test")
	assert.NotPanics(t, func() { m.Capture(context.Background(), Fault{Kind: "panic", Message: "boum"}) })
}

// Ce que le rangement reçoit porte le contexte de la requête : sans lui, on
// lit une pile d'exécution sans savoir qui, où, ni quelle requête.
func TestACapturedFaultCarriesItsContext(t *testing.T) {
	m := NewMetrics("vtc", "v1")
	got := &memorySink{}
	m.SetSink(got)
	ctx := WithField(WithField(context.Background(), KeyRequest, "r-9"), KeyCountry, "GN")
	m.Capture(ctx, Fault{Kind: "panic", Message: "boum", Route: "/rides"})

	require.Len(t, got.faults, 1)
	f := got.faults[0]
	assert.Equal(t, "vtc", f.Service)
	assert.Equal(t, "r-9", f.RequestID)
	assert.Equal(t, "GN", f.Country)
	assert.NotEmpty(t, f.Fingerprint)
	assert.False(t, f.At.IsZero())
}

// Un appel sortant se compte, et une panne d'un autre service se distingue
// d'un succès : c'est ce qui dit lequel des six ralentit les autres.
func TestOutboundCallsAreCountedByOutcome(t *testing.T) {
	m := NewMetrics("vtc", "test")
	m.ObserveCall("core", "wallet_debit", 30*time.Millisecond, nil)
	m.ObserveCall("core", "wallet_debit", 2*time.Second, assert.AnError)
	// ⚠️ Les étiquettes sortent TRIÉES : on cherche chacune, pas une chaîne
	// dans l'ordre où on les a déclarées.
	has(t, m, `dira_outbound_calls_total{operation="wallet_debit",outcome="ok",service="vtc",target="core"} 1`)
	has(t, m, `dira_outbound_calls_total{operation="wallet_debit",outcome="error",service="vtc",target="core"} 1`)
}

// Une mesure MÉTIER est lue au moment où on la regarde : poussée à chaque
// changement, elle demanderait d'y penser à vingt endroits, et le premier
// oubli la ferait mentir pour toujours.
func TestABusinessGaugeIsReadOnScrape(t *testing.T) {
	m := NewMetrics("vtc", "test")
	value := 3.0
	m.Gauge("dira_rides_searching", "Courses en recherche.", nil, func() float64 { return value })
	has(t, m, `dira_rides_searching{service="vtc"} 3`)
	value = 7
	has(t, m, `dira_rides_searching{service="vtc"} 7`)
}

type memorySink struct{ faults []Fault }

func (s *memorySink) Capture(_ context.Context, f Fault) { s.faults = append(s.faults, f) }

// has et hasNot cherchent une ligne dans l'exposition SANS la recracher en
// entier : un rapport d'échec de dix kilo-octets ne se lit pas, et on finit
// par ignorer le test plutôt que de le comprendre.
func has(t *testing.T, m *Metrics, want string) {
	t.Helper()
	if !strings.Contains(scrape(t, m), want) {
		t.Fatalf("l'exposition ne contient pas : %s", want)
	}
}

func hasNot(t *testing.T, m *Metrics, unwanted string) {
	t.Helper()
	if strings.Contains(scrape(t, m), unwanted) {
		t.Fatalf("l'exposition contient ce qu'elle ne devrait pas : %s", unwanted)
	}
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, 200, rec.Code)
	return strings.Join(strings.Split(rec.Body.String(), "\n"), "\n")
}

// ⚠️ DEUX ROUTEURS SUR LA PLATEFORME. Les APIs métier utilisent chi ; le
// service de SUIVI — le plus sollicité de tous — utilise le multiplexeur de la
// bibliothèque standard. Sans cette seconde lecture, il compterait TOUTES ses
// requêtes sous `unmatched`, et la supervision ne dirait rien de ce qui
// ralentit.
func TestTheStandardMuxPatternIsReadToo(t *testing.T) {
	m := NewMetrics("tracking", "test")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /track/missions/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	h := m.Middleware(mux)

	for _, id := range []string{"m1", "m2"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/track/missions/"+id, nil))
	}

	has(t, m, `route="GET /track/missions/{id}"`)
	hasNot(t, m, "m1")
}
