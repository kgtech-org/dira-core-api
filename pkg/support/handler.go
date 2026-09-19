package support

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct {
	svc *Service
	// scope est la PORTÉE exigée d'un ADMINISTRATEUR sur tout le guichet —
	// `auth.ScopeVTC`, `auth.ScopeFood`. C'est ce qui fait que les plaintes
	// des courses et celles de la livraison peuvent être tenues par DEUX
	// profils de support : un membre dont le périmètre ne couvre pas la
	// verticale ne lit pas sa file, n'y répond pas, ne la modifie pas.
	// `RequireScope` laisse passer les autres rôles : la portée est une
	// notion de staff. Vide quand la verticale pose déjà le garde à l'entrée.
	scope string
}

// NewHandler builds the HTTP surface of one vertical's support desk.
func NewHandler(svc *Service, scope string) *Handler {
	return &Handler{svc: svc, scope: scope}
}

// Mount registers the routes on the vertical's /api/v1/<vertical> router.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		if h.scope != "" {
			g.Use(middleware.RequireScope(h.scope))
		}
		g.Post("/tickets", h.create)
		g.Get("/tickets", h.list)
		g.Get("/tickets/{id}", h.get)
		g.Post("/tickets/{id}/messages", h.addMessage)
		g.Post("/tickets/{id}/lost-item", h.answerLostItem)
		g.With(middleware.RequireRole(auth.RoleAdmin)).Patch("/admin/tickets/{id}", h.update)
	})
}

// POST /tickets
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	var req CreateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Create(r.Context(), userID, role, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// GET /tickets
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	q := ListQuery{
		Status:     r.URL.Query().Get("status"),
		Category:   r.URL.Query().Get("category"),
		AssignedTo: r.URL.Query().Get("assigned_to"),
	}
	items, next, err := h.svc.List(r.Context(), userID, role, q, httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

// GET /tickets/{id}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	resp, err := h.svc.Get(r.Context(), userID, role, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// POST /tickets/{id}/messages
func (h *Handler) addMessage(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	var req MessageRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.AddMessage(r.Context(), userID, role, chi.URLParam(r, "id"), req.Body)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// POST /tickets/{id}/lost-item — la réponse du chauffeur.
func (h *Handler) answerLostItem(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	var req LostItemAnswerRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.AnswerLostItem(r.Context(), userID, role, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// PATCH /admin/tickets/{id}
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func caller(r *http.Request) (userID, role string, ok bool) {
	userID, ok = auth.UserFromContext(r.Context())
	if !ok {
		return "", "", false
	}
	role, _ = auth.RoleFromContext(r.Context())
	return userID, role, true
}
