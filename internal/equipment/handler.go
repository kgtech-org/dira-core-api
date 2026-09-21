package equipment

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
//
// Deux publics : l'AGENT (livreur, chauffeur — rôle `driver`) lit le
// catalogue, ses contrats, accepte, paie, demande ; l'EXPLOITATION (admin,
// portée `core`) tient le catalogue, les réglages et les contrats.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleDriver))
		g.Get("/equipment/catalogue", h.catalogue)
		g.Post("/equipment/requests", h.request)
		g.Get("/me/equipment", h.mine)
		g.Post("/me/equipment/{id}/accept", h.accept)
		g.Post("/me/equipment/{id}/pay", h.pay)
	})
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/equipment/settings", h.settings)
		g.Put("/admin/equipment/settings", h.updateSettings)
		g.Get("/admin/equipment/items", h.items)
		g.Post("/admin/equipment/items", h.createItem)
		g.Patch("/admin/equipment/items/{id}", h.updateItem)
		g.Get("/admin/equipment/contracts", h.contracts)
		g.Post("/admin/equipment/contracts", h.createContract)
		g.Get("/admin/equipment/contracts/{id}", h.contract)
		g.Patch("/admin/equipment/contracts/{id}", h.updateContract)
		g.Post("/admin/equipment/contracts/{id}/qualify", h.qualify)
		g.Post("/admin/equipment/contracts/{id}/hand-over", h.handOver)
		g.Post("/admin/equipment/contracts/{id}/payments", h.payment)
		g.Post("/admin/equipment/contracts/{id}/return", h.ret)
		g.Post("/admin/equipment/contracts/{id}/cancel", h.cancel)
		g.Post("/admin/equipment/contracts/{id}/default", h.defaulted)
		g.Post("/admin/equipment/contracts/{id}/lines/{n}/waive", h.waive)
		g.Get("/admin/equipment/standing/{userID}", h.standing)
	})
}

func caller(r *http.Request) (string, string) {
	userID, _ := auth.UserFromContext(r.Context())
	role, _ := auth.RoleFromContext(r.Context())
	return userID, role
}

// --- agent ---

func (h *Handler) catalogue(w http.ResponseWriter, r *http.Request) {
	_, role := caller(r)
	vertical, err := roleVertical(role, r.URL.Query().Get("vertical"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.svc.Catalogue(r.Context(), vertical)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) request(w http.ResponseWriter, r *http.Request) {
	userID, role := caller(r)
	var req struct {
		RequestInput
		Vertical string `json:"vertical" validate:"required,oneof=food vtc"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	vertical, err := roleVertical(role, req.Vertical)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Request(r.Context(), userID, vertical, req.RequestInput)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := caller(r)
	items, err := h.svc.MyContracts(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	st, _ := h.svc.Standing(r.Context(), userID)
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "standing": st})
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	userID, _ := caller(r)
	resp, err := h.svc.Accept(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) pay(w http.ResponseWriter, r *http.Request) {
	userID, _ := caller(r)
	var req PayInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Pay(r.Context(), userID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// --- exploitation ---

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetSettings(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.UpdateSettings(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListItems(r.Context(), r.URL.Query().Get("active") == "true")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createItem(w http.ResponseWriter, r *http.Request) {
	var req ItemInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.CreateItem(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	var req ItemInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.UpdateItem(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) contracts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ContractFilter{Status: q.Get("status"), Vertical: q.Get("vertical")}
	if v := q.Get("user_id"); v != "" {
		if oid, err := primitive.ObjectIDFromHex(v); err == nil {
			f.UserID = &oid
		}
	}
	if v := q.Get("item_id"); v != "" {
		if oid, err := primitive.ObjectIDFromHex(v); err == nil {
			f.ItemID = &oid
		}
	}
	items, next, err := h.svc.ListContracts(r.Context(), f, httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) createContract(w http.ResponseWriter, r *http.Request) {
	actor, _ := caller(r)
	var req ContractInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.CreateContract(r.Context(), actor, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) contract(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetContract(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) updateContract(w http.ResponseWriter, r *http.Request) {
	actor, _ := caller(r)
	var req ContractPatch
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.UpdateContract(r.Context(), actor, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, do func(actor, id string) (*ContractResponse, error)) {
	actor, _ := caller(r)
	resp, err := do(actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) qualify(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, func(actor, id string) (*ContractResponse, error) { return h.svc.Qualify(r.Context(), actor, id) })
}

func (h *Handler) handOver(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, func(actor, id string) (*ContractResponse, error) { return h.svc.HandOver(r.Context(), actor, id) })
}

func (h *Handler) payment(w http.ResponseWriter, r *http.Request) {
	var req PaymentInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.action(w, r, func(actor, id string) (*ContractResponse, error) {
		return h.svc.RecordPayment(r.Context(), actor, id, req)
	})
}

func (h *Handler) ret(w http.ResponseWriter, r *http.Request) {
	var req ReturnInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.action(w, r, func(actor, id string) (*ContractResponse, error) { return h.svc.Return(r.Context(), actor, id, req) })
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason" validate:"omitempty,max=500"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	h.action(w, r, func(actor, id string) (*ContractResponse, error) {
		return h.svc.Cancel(r.Context(), actor, id, req.Reason)
	})
}

func (h *Handler) defaulted(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, func(actor, id string) (*ContractResponse, error) { return h.svc.Default(r.Context(), actor, id) })
}

func (h *Handler) waive(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil {
		httpx.Error(w, r, apperr.Validation("line number must be an integer"))
		return
	}
	var req struct {
		Note string `json:"note" validate:"omitempty,max=500"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	h.action(w, r, func(actor, id string) (*ContractResponse, error) {
		return h.svc.Waive(r.Context(), actor, id, n, req.Note)
	})
}

func (h *Handler) standing(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Standing(r.Context(), chi.URLParam(r, "userID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
