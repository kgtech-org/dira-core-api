package rating

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
func (h *Handler) Mount(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	// Les avis se LISENT sans compte : c'est ce qui aide à choisir un
	// restaurant avant même de s'inscrire.
	r.Get("/stores/{id}/ratings", h.listStoreRatings)
	r.Get("/agents/{id}/ratings", h.listAgentRatings)
	r.Get("/dishes/{id}/ratings", h.listDishRatings)

	// ⚠️ Écrire une note est une route de SERVICE À SERVICE, pas une route
	// publique. Le socle ne sait pas si une commande est livrée ni qui l'a
	// portée : c'est la verticale qui valide, puis dépose.
	//
	// Ouvrir cette route au porteur d'un jeton d'utilisateur laisserait
	// n'importe qui noter n'importe quoi autant de fois qu'il le veut — seule
	// l'unicité (commande, cible) l'arrêterait, et elle ne connaît pas les
	// commandes qui n'existent pas.
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Post("/internal/ratings", h.record)
	})
}

// POST /internal/ratings — dépôt d'une note DÉJÀ validée par une verticale.
func (h *Handler) record(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string         `json:"client_id" validate:"required,len=24,hexadecimal"`
		OrderID  string         `json:"order_id" validate:"required,len=24,hexadecimal"`
		Targets  []RecordTarget `json:"targets" validate:"required,min=1,max=40,dive"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.svc.Record(r.Context(), req.ClientID, req.OrderID, req.Targets)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"items": items})
}

func (h *Handler) listStoreRatings(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, TargetStore)
}

func (h *Handler) listAgentRatings(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, TargetDriver)
}

func (h *Handler) listDishRatings(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, TargetDish)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, target string) {
	page := httpx.PageFromRequest(r)
	items, err := h.svc.ListForTarget(r.Context(), target, chi.URLParam(r, "id"), page.Limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, "")
}
