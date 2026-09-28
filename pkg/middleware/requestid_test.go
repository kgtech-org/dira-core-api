package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// L'identifiant fourni par l'application est REPRIS : c'est ce qui permet de
// retrouver un appel dont la réponse n'est jamais arrivée — précisément celui
// qu'on cherche à comprendre.
func TestAProvidedRequestIDIsKept(t *testing.T) {
	assert.Equal(t, "app-42_a.b", replay(t, "app-42_a.b"))
}

// Sans rien, on en tire un : une requête sans fil n'existe pas.
func TestWithoutOneWeMintIt(t *testing.T) {
	assert.NotEmpty(t, replay(t, ""))
}

// ⚠️ CE QUI ARRIVE DU CLIENT REPART DANS L'EN-TÊTE ET DANS CHAQUE LIGNE DE
// JOURNAL DE SIX SERVICES. Non borné, il suffit d'une application qui envoie
// dix kilo-octets à chaque appel pour remplir la base de journaux.
func TestAnOversizedRequestIDIsRefused(t *testing.T) {
	got := replay(t, strings.Repeat("a", 65))
	assert.NotEqual(t, strings.Repeat("a", 65), got)
	assert.NotEmpty(t, got, "refusé ne veut pas dire absent : on tire le nôtre")
}

// ⚠️ ET ON REFUSE, ON NE TRONQUE PAS : tronqué, un identifiant de trente
// caractères en devient un de soixante-quatre, et deux fils différents se
// confondraient au moment précis où on les cherche.
func TestARefusedRequestIDIsNotTruncated(t *testing.T) {
	long := strings.Repeat("a", 64) + "ZZZ"
	assert.NotEqual(t, long[:64], replay(t, long))
}

// Un identifiant qui porte autre chose que des caractères sûrs ne passe pas :
// il finirait recopié dans des journaux que personne ne songe à purger.
func TestAnUnsafeRequestIDIsRefused(t *testing.T) {
	for _, bad := range []string{"+22890000000", "fil 7", "a/b", "<script>", "e\nf", "fil;drop"} {
		assert.NotEqual(t, bad, replay(t, bad), "accepté à tort : %q", bad)
	}
}

// replay passe une requête dans le middleware et rend l'identifiant RETENU —
// lu sur la réponse, là où l'application le lira.
func replay(t *testing.T, sent string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if sent != "" {
		req.Header.Set(obs.HeaderRequestID, sent)
	}
	rec := httptest.NewRecorder()
	var seen string
	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	})).ServeHTTP(rec, req)

	// Le contexte et la réponse disent la MÊME chose : sinon l'application
	// citerait dans son ticket un identifiant absent de nos journaux.
	assert.Equal(t, rec.Header().Get(obs.HeaderRequestID), seen)
	// Et les journaux le voient : c'est `obs` qui le recopie dans chaque ligne.
	return seen
}

// ⚠️ LE PREMIER ÉLÉMENT DE `X-Forwarded-For` EST ÉCRIT PAR LE CLIENT. C'est lui
// qu'on lisait : en envoyant `X-Forwarded-For: 203.0.113.77`, la clé de cadence
// devenait celle de cette adresse inventée, et en faisant tourner l'en-tête,
// chaque requête obtenait son propre compartiment de 120 par minute — la limite
// n'existait pas. Mesuré en direct sur la recette, dans Redis.
func TestAForgedForwardedForNoLongerChoosesTheBucket(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "10.0.0.9:5555"
	// Ce que le client a écrit, puis ce que NOS mandataires ont ajouté à droite.
	r.Header.Set("X-Forwarded-For", "203.0.113.77, 41.207.10.4, 172.18.0.5")

	got := ClientIP(r)
	assert.NotEqual(t, "203.0.113.77", got, "l'adresse inventée par le client ne doit plus décider")
	assert.Equal(t, "172.18.0.5", got, "le dernier élément : celui que nous avons ajouté")
}

// La façade écrase ce que le client aurait mis : c'est la seule source qu'il ne
// choisit pas, et elle passe devant tout le reste.
func TestTheEdgeHeaderWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.77, 41.207.10.4")
	r.Header.Set(HeaderCloudflareIP, "41.207.10.4")
	assert.Equal(t, "41.207.10.4", ClientIP(r))
}

// Sans aucun en-tête — un appel direct sur le réseau interne —, l'adresse de la
// connexion, sans son port.
func TestWithoutHeadersWeUseTheConnection(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "172.18.0.24:41022"
	assert.Equal(t, "172.18.0.24", ClientIP(r))
}

// Un en-tête vide ou fait d'espaces ne doit pas rendre une chaîne vide : tout le
// trafic anonyme partagerait alors la clé `ip:`, et un seul assaillant
// verrouillerait la porte de tout le monde.
func TestAnEmptyHeaderNeverYieldsAnEmptyKey(t *testing.T) {
	for _, xff := range []string{"", "   ", ",", " , "} {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.RemoteAddr = "10.1.2.3:7000"
		r.Header.Set("X-Forwarded-For", xff)
		assert.Equal(t, "10.1.2.3", ClientIP(r), "xff=%q", xff)
	}
}
