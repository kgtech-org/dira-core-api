package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
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
	// LA PORTE PAR CODE, pour les clients — voir `otp.go`. Publique par
	// nature : on s'inscrit avant d'avoir un jeton.
	r.Post("/auth/otp", h.requestOTP)
	r.Post("/auth/otp/verify", h.verifyOTP)

	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/auth/logout", h.logout)
		g.Get("/me", h.me)
		g.Patch("/me", h.updateMe)
		g.Patch("/me/preferences", h.updatePreferences)
		// SUPPRIMER SON COMPTE — voir `erasure.go`. Deux temps : la fermeture
		// maintenant, l'effacement après le délai de grâce.
		g.Delete("/me", h.deleteMe)
		// Carnet d'adresses : le client répétait jusqu'ici son adresse et ses
		// indications de porte à chaque commande.
		g.Get("/me/addresses", h.listAddresses)
		g.Post("/me/addresses", h.createAddress)
		g.Put("/me/addresses/{id}", h.updateAddress)
		g.Delete("/me/addresses/{id}", h.deleteAddress)

		// Administration des comptes. Elle vit au SOCLE parce qu'un compte est
		// un compte : la console des repas et celle des courses cherchent la
		// même personne. Deux écrans d'administration pour un seul annuaire
		// auraient donné une suspension qui ne vaut que d'un côté.
		//
		// La fiche COMPLÈTE d'un livreur — solde, véhicules, courses — se
		// compose dans la verticale, à partir de ces lignes.
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/users", h.listAccounts)
		g.With(admin).Get("/admin/users/{id}", h.getAccount)
		g.With(admin).Patch("/admin/users/{id}/status", h.setAccountStatus)
		// LA SORTIE DU SUPPORT — voir `agentapp.go`. Une personne change de
		// métier : livreuse hier, chauffeuse aujourd'hui. Sans ce geste, le
		// support n'a aucune issue et ouvrira un second compte à la même
		// personne — second téléphone, second portefeuille, second historique
		// —, c'est-à-dire exactement le désordre que la règle évite.
		g.With(admin).Delete("/admin/users/{id}/agent-app", h.releaseAgentApp)
	})
}

// GET /admin/users?role=&status=&q=&cursor=&limit=
func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	page := httpx.PageFromRequest(r)
	items, next, err := h.svc.ListAccounts(r.Context(), AccountFilter{
		Role:   r.URL.Query().Get("role"),
		Status: r.URL.Query().Get("status"),
		Query:  r.URL.Query().Get("q"),
	}, page.Cursor, page.Limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

// GET /admin/users/{id}
func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.AccountByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, row)
}

// PATCH /admin/users/{id}/status
func (h *Handler) setAccountStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status" validate:"required,oneof=active suspended"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	before, err := h.svc.SetAccountStatus(r.Context(), chi.URLParam(r, "id"), req.Status)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// L'état AVANT est rendu pour que l'appelant sache ce qui a réellement
	// bougé : suspendre un compte déjà suspendu n'est pas la même chose que
	// suspendre un compte actif, et seule la trace d'audit le distingue.
	httpx.JSON(w, http.StatusOK, map[string]any{"before": before, "status": req.Status})
}

// DELETE /admin/users/{id}/agent-app — LA SORTIE DU SUPPORT.
//
// Rend l'appartenance LIBÉRÉE, pour que l'écran puisse dire laquelle — « ce
// compte n'est plus rattaché à Dira Livreur » — plutôt qu'un « c'est fait »
// dont l'opérateur ne peut rien conclure. Vide : il n'y en avait pas, et
// c'est un succès aussi.
func (h *Handler) releaseAgentApp(w http.ResponseWriter, r *http.Request) {
	released, err := h.svc.ReleaseAgentApp(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"released_agent_app": released})
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
	// Le pays RETENU pour le compte — l'indicatif a pu parler — remplace
	// celui que le middleware avait annoncé avant de connaître le numéro.
	if resp.User.Country != "" {
		w.Header().Set(country.Header, resp.User.Country)
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) requestOTP(w http.ResponseWriter, r *http.Request) {
	var req OTPRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.RequestOTP(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var req OTPVerifyRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.VerifyOTP(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Le pays retenu pour un compte qui vient de naître — l'indicatif a pu
	// parler —, comme à l'inscription ordinaire.
	if resp.User.Country != "" {
		w.Header().Set(country.Header, resp.User.Country)
	}
	status := http.StatusOK
	if resp.Created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, resp)
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
	resp, err := h.svc.Refresh(r.Context(), req.RefreshToken, req.Platform)
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

func (h *Handler) deleteMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req ErasureRequest
	// Le corps peut être VIDE pour un compte sans mot de passe qui n'a pas
	// encore demandé de code : le service répondra ce qu'il faut envoyer.
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	if err := h.svc.RequestErasure(r.Context(), userID, req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, h.svc.ErasureStatus(r.Context(), userID))
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
