package country

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler exposes the country routes.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the routes on the /api/v1 router.
//
// `GET /countries` est PUBLIC : une application a besoin de la liste avant
// toute connexion — pour proposer l'indicatif à l'inscription, et pour dire
// « Dira n'est pas encore chez vous ».
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Get("/countries", h.listPublic)
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/me/country/resolve", h.resolve)
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/countries", h.listAdmin)
		g.With(admin).Put("/admin/countries/{code}", h.update)
	})
}

// MountService registers what a VERTICAL may ask: the installed list.
func (h *Handler) MountService(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.With(serviceMW).Get("/internal/countries", h.listPublic)
}

func (h *Handler) listPublic(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context(), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "default": h.svc.Default()})
}

func (h *Handler) listAdmin(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context(), false)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "default": h.svc.Default()})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), chi.URLParam(r, "code"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /me/country/resolve {lng?, lat?}
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req ResolveRequest
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &req); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	out, err := h.svc.Resolve(r.Context(), userID, ClientIP(r), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
