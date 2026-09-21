package finance

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount : l'exploitation (portée `core`) lit l'aperçu, le journal, les
// constats, règle la facturation et lance un balayage.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/finance/billing", h.billing)
		g.Put("/admin/finance/billing", h.updateBilling)
		g.Get("/admin/finance/overview", h.overview)
		g.Get("/admin/finance/journal", h.journal)
		g.Get("/admin/finance/accounts", h.accounts)
		g.Get("/admin/finance/findings", h.findings)
		g.Post("/admin/finance/findings/{id}/ack", h.ack)
		g.Post("/admin/finance/integrity/run", h.run)
	})
}

// MountInternal : les verticales lisent la facturation et déclarent ce qui
// bouge chez elles.
func (h *Handler) MountInternal(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Get("/internal/finance/billing", h.internalBilling)
		g.Post("/internal/finance/events", h.event)
	})
}

func (h *Handler) billing(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.Billing(r.Context(), "")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, b)
}

func (h *Handler) updateBilling(w http.ResponseWriter, r *http.Request) {
	var in BillingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	b, err := h.svc.UpdateBilling(r.Context(), actor, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, b)
}

func (h *Handler) internalBilling(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.Billing(r.Context(), r.URL.Query().Get("country"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, b)
}

func (h *Handler) event(w http.ResponseWriter, r *http.Request) {
	var ev Event
	if err := httpx.Decode(r, &ev); err != nil {
		httpx.Error(w, r, err)
		return
	}
	n, err := h.svc.PostEvent(r.Context(), ev)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"entries": n})
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ov, err := h.svc.Overview(r.Context(), parseTime(q.Get("from")), parseTime(q.Get("to")))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ov)
}

func (h *Handler) journal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := EntryFilter{
		Account: q.Get("account"), Reason: q.Get("reason"), Service: q.Get("service"),
		RefKind: q.Get("ref_kind"), RefID: q.Get("ref_id"), OwnerID: q.Get("owner_id"), Unit: q.Get("unit"),
		From: parseTime(q.Get("from")), To: parseTime(q.Get("to")),
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, next, err := h.svc.Entries(r.Context(), f, limit, q.Get("cursor"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) accounts(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"items": Chart})
}

func (h *Handler) findings(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Findings(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "last_run": h.svc.LastRun()})
}

func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note" validate:"omitempty,max=500"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	actor, _ := auth.UserFromContext(r.Context())
	f, err := h.svc.Acknowledge(r.Context(), actor, chi.URLParam(r, "id"), in.Note)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, f)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	run, err := h.svc.RunIntegrity(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if run == nil {
		httpx.Error(w, r, apperr.Internal(nil))
		return
	}
	httpx.JSON(w, http.StatusOK, run)
}
