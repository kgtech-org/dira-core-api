package chat

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
	// base est le segment de route de la VERTICALE : « orders » pour la
	// livraison, « rides » pour les courses.
	//
	// ⚠️ Un argument, pas une constante. Écrire « orders » en dur dans une
	// bibliothèque partagée obligerait les courses à servir leurs
	// conversations sous le nom de la livraison — et un passager lirait
	// « /rides/… » dans sa spec pour appeler « /orders/… ».
	base string
}

// NewHandler builds the HTTP surface. `base` est le segment de la verticale :
// « orders » ou « rides ».
func NewHandler(svc *Service, base string) *Handler {
	return &Handler{svc: svc, base: base}
}

// Mount registers the module routes on the /api/v1 router.
//
// Aucune route publique : une conversation n'a que deux participants, plus
// l'administration qui la LIT pour le support.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	messages := "/" + h.base + "/{id}/messages"
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Get(messages, h.list)
		g.Post(messages, h.send)
		g.Post(messages+"/read", h.markRead)
		// Émulateurs de la console : écrire POUR une partie, par la même
		// méthode de service. Un administrateur n'écrit jamais en son nom —
		// un message de la plateforme passerait pour celui d'un livreur.
		g.With(middleware.RequireRole(auth.RoleAdmin)).
			Post("/admin"+messages, h.sendAs)
	})
}

// GET /{orders|rides}/{id}/messages
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	page := httpx.PageFromRequest(r)
	items, unread, err := h.svc.List(r.Context(), userID, role, chi.URLParam(r, "id"), page.Cursor, page.Limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := ConversationResponse{Items: items, Unread: unread}
	if len(items) > 0 {
		resp.NextCursor = items[len(items)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// POST /{orders|rides}/{id}/messages
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	var req SendMessageRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	msg, err := h.svc.Send(r.Context(), userID, role, chi.URLParam(r, "id"), req.Body)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, msg)
}

// POST /{orders|rides}/{id}/messages/read — tout ce que l'autre a envoyé.
func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	n, err := h.svc.MarkRead(r.Context(), userID, role, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"marked": n})
}

// POST /admin/{orders|rides}/{id}/messages — au nom d'une partie.
func (h *Handler) sendAs(w http.ResponseWriter, r *http.Request) {
	var req SendAsRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	msg, err := h.svc.SendAs(r.Context(), chi.URLParam(r, "id"), req.As, req.Body)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, msg)
}

func caller(r *http.Request) (userID, role string, ok bool) {
	userID, ok = auth.UserFromContext(r.Context())
	if !ok {
		return "", "", false
	}
	role, _ = auth.RoleFromContext(r.Context())
	return userID, role, true
}
