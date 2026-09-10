package payment

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

const maxWebhookBody = 1 << 20 // 1 MiB

// Handler exposes the payment HTTP endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
// authMW is the JWT middleware built at wiring time.
//
// allowSandbox ouvre la confirmation manuelle d'un paiement. C'est un
// paramètre EXPLICITE et non une lecture de configuration cachée ici : le
// choix d'autoriser la simulation d'un encaissement doit se lire à
// l'assemblage, là où l'environnement est connu.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler, allowSandbox bool) {
	// Public, protected by webhook signature verification.
	r.Post("/webhooks/payment/{provider}", h.webhook)

	r.Group(func(g chi.Router) {
		g.Use(authMW)
		// La liste des opérateurs RÉELLEMENT branchés. Sans elle, une
		// application code la liste en dur et propose des paiements qui
		// échoueront.
		g.With(middleware.RequireRole(auth.RoleClient, auth.RoleDriver, auth.RoleMerchant)).
			Get("/payments/providers", h.providers)
		g.With(middleware.RequireRole(auth.RoleClient, auth.RoleDriver, auth.RoleMerchant)).
			Post("/payments/initiate", h.initiate)
		g.Get("/payments/{id}", h.get)
	})

	if allowSandbox {
		r.Group(func(g chi.Router) {
			g.Use(authMW)
			g.With(middleware.RequireRole(auth.RoleAdmin)).
				Post("/admin/payments/{id}/confirm", h.confirmSandbox)
		})
	}
}

// POST /admin/payments/{id}/confirm — hors production uniquement.
func (h *Handler) confirmSandbox(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ConfirmSandboxPayment(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /payments/providers — ce que le serveur sait réellement encaisser.
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"items": h.svc.Providers()})
}

func (h *Handler) initiate(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req InitiatePaymentRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Initiate(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	role, _ := auth.RoleFromContext(r.Context())
	resp, err := h.svc.Get(r.Context(), userID, role, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpx.Error(w, r, apperr.Validation("unreadable webhook body").WithCause(err))
		return
	}
	signature := r.Header.Get("X-Signature")
	if err := h.svc.HandleWebhook(r.Context(), chi.URLParam(r, "provider"), payload, signature); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, WebhookAck{Received: true})
}
