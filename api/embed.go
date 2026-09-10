// Package api embarque le contrat OpenAPI dans le binaire.
//
// Le fichier openapi.yaml reste la source de vérité à sa place habituelle : il
// est simplement compilé dans l'exécutable, donc la documentation servie est
// TOUJOURS celle de la version déployée — rien à copier sur le serveur.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
