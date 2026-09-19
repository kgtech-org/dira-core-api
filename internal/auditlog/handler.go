// Package auditlog serves the platform's ONE audit journal to the console.
//
// Le socle est le seul à le lire : les verticales y écrivent par
// `POST /internal/audit` et n'en gardent pas de copie. Une action sensible
// se retrouve donc à un seul endroit, quel que soit le service qui l'a
// faite — c'est ce qui manquait quand chaque service tenait le sien et que
// la console ne lisait que celui de la livraison.
package auditlog

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ rec *audit.Recorder }

func NewHandler(rec *audit.Recorder) *Handler { return &Handler{rec: rec} }

// Mount registers GET /admin/audit. Tout administrateur : le journal est
// transverse, et c'est la portée de chaque ACTION qui a été vérifiée au
// moment où elle a été faite.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.With(authMW, middleware.RequireRole(auth.RoleAdmin)).Get("/admin/audit", h.search)
}

// GET /admin/audit?service=&actor_id=&action=&resource_id=&from=&to=&limit=&cursor=
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := audit.Filter{
		Service:    q.Get("service"),
		ActorID:    q.Get("actor_id"),
		Action:     q.Get("action"),
		ResourceID: q.Get("resource_id"),
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = &t
		}
	}
	page := httpx.PageFromRequest(r)
	items, next, err := h.rec.Search(r.Context(), f, page.Limit, page.Cursor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}
