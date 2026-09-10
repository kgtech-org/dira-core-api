// Package docs sert la documentation interactive de l'API.
//
// Routes exposées (hors /api/v1, donc sans authentification) :
//
//	GET /docs         page Swagger UI
//	GET /openapi.yaml contrat OpenAPI 3.1 embarqué dans le binaire
//
// Le contrat déclare son `servers:` — servi sur la même origine que l'API, le
// bouton « Try it out » cible directement l'instance courante.
//
// ⚠️ Le contrat est passé par le SERVICE, pas embarqué ici : chaque verticale a
// le sien, et l'embarquer dans le socle aurait fait servir la documentation de
// la livraison par le service des courses.
//
// ⚠️ Derrière la passerelle, le `servers:` du contrat doit annoncer le chemin
// PUBLIC (`/api/v1/food`, `/api/v1/vtc`) et non le chemin local, sinon tout
// client généré depuis lui tape la mauvaise adresse.
package docs

import (
	"html"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const specPath = "/openapi.yaml"

// page est la coquille Swagger UI (assets servis par un CDN pour garder
// l'image légère ; seul le contrat est embarqué).
const page = `<!doctype html>
<html lang="fr">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>{{TITLE}}</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css" />
    <style>
      body { margin: 0; background: #fafafa; }
      .topbar { display: none; }
    </style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" crossorigin></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: "` + specPath + `",
          dom_id: "#swagger-ui",
          deepLinking: true,
          persistAuthorization: true,
          tryItOutEnabled: true,
          docExpansion: "none",
        });
      };
    </script>
  </body>
</html>
`

// Mount branche les routes de documentation sur le routeur racine.
//
// `spec` est le contrat du service appelant, embarqué chez lui par go:embed —
// la documentation servie est donc TOUJOURS celle de la version déployée.
func Mount(r chi.Router, title string, spec []byte) {
	rendered := []byte(strings.Replace(page, "{{TITLE}}", html.EscapeString(title), 1))
	r.Get("/docs", servePage(rendered))
	r.Get("/docs/", servePage(rendered))
	r.Get(specPath, serveSpec(spec))
}

func servePage(rendered []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(rendered)
	}
}

func serveSpec(spec []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(spec)
	}
}
