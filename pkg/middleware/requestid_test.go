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
