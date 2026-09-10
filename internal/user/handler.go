package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Handler exposes the user HTTP endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
// authMW is the JWT middleware built at wiring time.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
	r.Post("/auth/refresh", h.refresh)

	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/auth/logout", h.logout)
		g.Get("/me", h.me)
		g.Patch("/me", h.updateMe)
		g.Patch("/me/preferences", h.updatePreferences)
		// Carnet d'adresses : le client répétait jusqu'ici son adresse et ses
		// indications de porte à chaque commande.
		g.Get("/me/addresses", h.listAddresses)
		g.Post("/me/addresses", h.createAddress)
		g.Put("/me/addresses/{id}", h.updateAddress)
		g.Delete("/me/addresses/{id}", h.deleteAddress)
	})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Register(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Login(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, err := callerID(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Me(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	userID, err := callerID(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req UpdateMeRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.UpdateProfile(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func callerID(r *http.Request) (string, error) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		return "", apperr.Unauthorized("missing_token", "missing bearer token")
	}
	return userID, nil
}

func (h *Handler) updatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req UpdatePreferencesRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.UpdatePreferences(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) listAddresses(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	items, err := h.svc.ListAddresses(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, "")
}

func (h *Handler) createAddress(w http.ResponseWriter, r *http.Request) { h.saveAddress(w, r, "") }

func (h *Handler) updateAddress(w http.ResponseWriter, r *http.Request) {
	h.saveAddress(w, r, chi.URLParam(r, "id"))
}

func (h *Handler) saveAddress(w http.ResponseWriter, r *http.Request, id string) {
	userID, _ := auth.UserFromContext(r.Context())
	var req AddressRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.SaveAddress(r.Context(), userID, id, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, resp)
}

func (h *Handler) deleteAddress(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	if err := h.svc.DeleteAddress(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
