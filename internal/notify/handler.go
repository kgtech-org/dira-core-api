package notify

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount enregistre les routes du module.
//
// Deux publics : TOUT utilisateur connecté déclare son téléphone, seul
// l'ADMIN touche aux gabarits. Un texte qui part à des milliers de personnes
// n'est pas un réglage d'utilisateur.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/me/devices", h.registerDevice)
		g.Delete("/me/devices/{token}", h.unregisterDevice)
		// Le centre de notifications. Une notification poussée ne laisse
		// aucune trace : un téléphone éteint, une bannière balayée, et le
		// message n'existe plus nulle part.
		g.Get("/me/notifications", h.listInbox)
		g.Post("/me/notifications/read", h.markAllRead)
		g.Post("/me/notifications/{id}/read", h.markOneRead)

		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/message-templates", h.listTemplates)
		g.With(admin).Put("/admin/message-templates/{key}", h.saveTemplate)
		g.With(admin).Post("/admin/message-templates/{key}/preview", h.previewTemplate)
	})
}

// POST /me/devices — déclare un téléphone joignable.
func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	var req RegisterDeviceRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.RegisterDevice(r.Context(), userID, req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]bool{"registered": true})
}

// DELETE /me/devices/{token} — à la déconnexion.
//
// Le retirer, et pas seulement oublier le jeton côté application : sans cela,
// le téléphone continuerait de recevoir les notifications d'un compte dont
// son propriétaire est sorti.
func (h *Handler) unregisterDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	if err := h.svc.UnregisterDevice(r.Context(), userID, chi.URLParam(r, "token")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /admin/message-templates
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListTemplates(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, "")
}

// PUT /admin/message-templates/{key}
func (h *Handler) saveTemplate(w http.ResponseWriter, r *http.Request) {
	var req SaveTemplateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.SaveTemplate(r.Context(), chi.URLParam(r, "key"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// POST /admin/message-templates/{key}/preview — voir avant d'envoyer.
func (h *Handler) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Preview(r.Context(), chi.URLParam(r, "key"), req.Locale, req.Vars)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// GET /me/notifications — la liste, et le compte de non-lus.
func (h *Handler) listInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	page := httpx.PageFromRequest(r)
	items, unread, next, err := h.svc.ListInbox(r.Context(), userID, page.Cursor, page.Limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": items, "unread": unread, "next_cursor": next,
	})
}

// POST /me/notifications/read — tout marquer lu.
func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request) { h.markRead(w, r, "") }

// POST /me/notifications/{id}/read — une seule.
func (h *Handler) markOneRead(w http.ResponseWriter, r *http.Request) {
	h.markRead(w, r, chi.URLParam(r, "id"))
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request, id string) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	n, err := h.svc.MarkRead(r.Context(), userID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"marked": n})
}
