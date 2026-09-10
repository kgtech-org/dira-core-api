package fleet

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler exposes the fleet routes.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the back-office routes on the /api/v1 router.
//
// ⚠️ ADMINISTRATION SEULEMENT, et aucune route publique. Une flotte est un
// contrat commercial : son taux de commission et sa référence de contrat n'ont
// rien à faire dans une application mobile, et une lecture publique aurait
// exposé la grille de négociation d'une société à ses concurrentes.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/fleets", h.list)
		g.With(admin).Post("/admin/fleets", h.create)
		g.With(admin).Get("/admin/fleets/{id}", h.get)
		g.With(admin).Patch("/admin/fleets/{id}", h.update)
	})
}

// MountService registers what a VERTICAL is allowed to ask.
//
// ⚠️ Une seule route, et elle ne rend que des NOMS. Une verticale affiche
// « Flotte Sodigaz » à côté d'une plaque ; lui ouvrir la fiche entière lui
// confierait un contrat et une commission qu'elle n'a aucune raison de porter
// — et qu'elle finirait par recopier chez elle, où les deux copies
// divergeraient.
func (h *Handler) MountService(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Post("/internal/fleets/names", h.names)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, next, err := h.svc.List(r.Context(),
		r.URL.Query().Get("status"), r.URL.Query().Get("q"), httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req CreateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Create(r.Context(), actorID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), actorID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /internal/fleets/names — résolution en lot, pour une verticale.
func (h *Handler) names(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids" validate:"required,max=200"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	names, err := h.svc.Names(r.Context(), req.IDs)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"names": names})
}
