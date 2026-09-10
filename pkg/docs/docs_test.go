package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// spec est un contrat MINUSCULE et reconnaissable : ce paquet ne connaît plus
// le contenu d'aucune verticale, il transporte ce qu'on lui donne.
var spec = []byte("openapi: 3.1.0\ninfo:\n  title: Contrat de test\nservers:\n  - url: /api/v1/food\n")

func router() http.Handler {
	r := chi.NewRouter()
	Mount(r, "Dira Food API — Documentation", spec)
	return r
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestSpecIsServedAndEmbedded(t *testing.T) {
	rec := get(t, "/openapi.yaml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, attendu 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Fatalf("Content-Type = %q, attendu application/yaml", ct)
	}
	// Le contrat revient TEL QUEL. C'est la seule garantie qui compte ici :
	// le socle ne réécrit pas la documentation d'un service, il la sert.
	if rec.Body.String() != string(spec) {
		t.Fatalf("la spec servie diffère de celle fournie")
	}
}

// Le TITRE vient du service : servir « Dira Food API » depuis le service des
// courses induirait en erreur quiconque ouvre la page.
func TestPageCarriesTheServiceTitle(t *testing.T) {
	rec := get(t, "/docs")
	if !strings.Contains(rec.Body.String(), "Dira Food API — Documentation") {
		t.Fatalf("la page ne porte pas le titre du service")
	}
}

// Un titre venu de la configuration est ÉCHAPPÉ : il finit dans du HTML.
func TestTitleIsEscaped(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, `<script>alert(1)</script>`, spec)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("le titre n'est pas échappé")
	}
}

func TestDocsPageReferencesSpec(t *testing.T) {
	for _, path := range []string{"/docs", "/docs/"} {
		rec := get(t, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, attendu 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s Content-Type = %q, attendu text/html", path, ct)
		}
		if !strings.Contains(rec.Body.String(), specPath) {
			t.Fatalf("GET %s : la page ne référence pas %s", path, specPath)
		}
	}
}
