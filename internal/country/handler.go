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
		// LE FOND DE CARTE. ⚠️ La lecture ne rend JAMAIS les clés — seulement
		// « configurée » et leurs quatre derniers caractères. Une clé qu'un
		// écran d'administration réaffiche finit dans une capture d'écran, un
		// ticket, un canal de discussion ; et il n'y a aucune raison de la
		// relire : on la remplace, on ne la consulte pas.
		g.With(admin).Get("/admin/countries/{code}/maps", h.maps)
		g.With(admin).Put("/admin/countries/{code}/maps", h.updateMaps)
		// LE VERROU DES APPLICATIONS — biométrie ou code, par pays. Voir
		// `security.go`.
		g.With(admin).Get("/admin/countries/{code}/security", h.security)
		g.With(admin).Put("/admin/countries/{code}/security", h.updateSecurity)
		// LE BOUTON D'ALERTE : détections, délai d'annulation, numéros de
		// secours — voir `sos.go`. ⚠️ Une route À PART du reste de la sécurité
		// parce que c'est l'exploitation DE TERRAIN qui saisit les numéros, pas
		// celle qui règle les verrous d'application.
		g.With(admin).Get("/admin/countries/{code}/sos", h.sos)
		g.With(admin).Put("/admin/countries/{code}/sos", h.updateSOS)
		// QUI VOIT QUOI DE QUI entre un client et l'agent qui le sert, métier
		// par métier. Voir `privacy.go`.
		g.With(admin).Get("/admin/countries/{code}/privacy", h.privacy)
		g.With(admin).Put("/admin/countries/{code}/privacy", h.updatePrivacy)
		// CE QUE DONNE « INVITE UN AMI » dans ce pays. Voir `referral.go`.
		g.With(admin).Get("/admin/countries/{code}/referral", h.referral)
		g.With(admin).Put("/admin/countries/{code}/referral", h.updateReferral)
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

// GET /admin/countries/{code}/security
func (h *Handler) security(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Security(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/countries/{code}/privacy
func (h *Handler) privacy(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Privacy(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PUT /admin/countries/{code}/privacy {vertical, audience, name?, phone?, …}
//
// Un SEUL sens d'un SEUL métier par appel — voir `PrivacyUpdateRequest`.
func (h *Handler) updatePrivacy(w http.ResponseWriter, r *http.Request) {
	var req PrivacyUpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdatePrivacy(r.Context(), chi.URLParam(r, "code"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/countries/{code}/referral
func (h *Handler) referral(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Referral(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PUT /admin/countries/{code}/referral {invitee_xof?, sponsor_xof?, max_sponsored?, valid_days?}
func (h *Handler) updateReferral(w http.ResponseWriter, r *http.Request) {
	var req ReferralUpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateReferral(r.Context(), chi.URLParam(r, "code"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PUT /admin/countries/{code}/security {mode?, biometrics?, pin_length?, grace_seconds?, max_attempts?}
func (h *Handler) updateSecurity(w http.ResponseWriter, r *http.Request) {
	var req SecurityUpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateSecurity(r.Context(), chi.URLParam(r, "code"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/countries/{code}/sos
func (h *Handler) sos(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.SOSPolicy(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PUT /admin/countries/{code}/sos {shake?, crash?, voice?, countdown_seconds?, numbers?}
func (h *Handler) updateSOS(w http.ResponseWriter, r *http.Request) {
	var req SOSUpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateSOS(r.Context(), chi.URLParam(r, "code"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/countries/{code}/maps
func (h *Handler) maps(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Maps(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PUT /admin/countries/{code}/maps {basemap?, google?}
func (h *Handler) updateMaps(w http.ResponseWriter, r *http.Request) {
	var req MapsUpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateMaps(r.Context(), chi.URLParam(r, "code"), req)
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
