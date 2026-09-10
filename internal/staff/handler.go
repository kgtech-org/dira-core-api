package staff

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler exposes the staff routes.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the back-office routes.
//
// ⚠️ ADMINISTRATION SEULEMENT. Qui administre quoi est une information
// sensible : la liste du staff et de ses périmètres dit à un attaquant quel
// compte viser pour atteindre quelle partie de la plateforme.
//
// ⚠️ Ces routes ne sont PAS gardées par une portée. Un administrateur borné
// aux courses doit pouvoir consulter l'équipe — mais c'est la fonction
// `admin` qui devrait seule pouvoir MODIFIER, et cette distinction n'est pas
// encore posée. Dit ici plutôt que supposé : voir le journal.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/staff", h.list)
		g.With(admin).Post("/admin/staff", h.create)
		g.With(admin).Patch("/admin/staff/{id}", h.update)
		g.With(admin).Delete("/admin/staff/{id}", h.remove)
		// Le VOCABULAIRE, pour que la console propose les mêmes fonctions et
		// les mêmes portées que celles que le serveur accepte. Deux listes
		// recopiées divergeraient au premier ajout.
		g.With(admin).Get("/admin/staff/vocabulary", h.vocabulary)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, next, err := h.svc.List(r.Context(),
		q.Get("function"), q.Get("scope"), q.Get("status"), httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Remove(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /admin/staff/vocabulary — les fonctions et les portées connues.
func (h *Handler) vocabulary(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"functions": Functions,
		"scopes":    Scopes,
		// ⚠️ Dit EXPLICITEMENT ce que « aucune portée » signifie, pour qu'une
		// console n'ait pas à le supposer. La réponse est littérale : rien.
		"empty_scopes_means": "nothing",
	})
}
