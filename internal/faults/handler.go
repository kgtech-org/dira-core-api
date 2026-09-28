package faults

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

// Handler sert les pannes à l'exploitation.
type Handler struct{ repo *Repository }

func NewHandler(repo *Repository) *Handler { return &Handler{repo: repo} }

// Mount registers the operator routes.
//
// ⚠️ ADMINISTRATEUR SEULEMENT, et il le faut : une panne porte une pile
// d'exécution, des identifiants de comptes et le chemin d'une requête. C'est
// ce qu'on montre à quelqu'un qui répare, pas à quelqu'un qui passe.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	admin := r.With(authMW, middleware.RequireRole(auth.RoleAdmin))
	admin.Get("/admin/faults", h.list)
	admin.Get("/admin/faults/{fingerprint}", h.one)
	admin.Post("/admin/faults/{fingerprint}/resolve", h.resolve)
}

// GET /admin/faults?service=&kind=&state=open|resolved&days=&limit=
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{Service: q.Get("service"), Kind: q.Get("kind"), Resolved: q.Get("state")}
	if days, err := strconv.Atoi(q.Get("days")); err == nil && days > 0 {
		f.Since = time.Now().UTC().AddDate(0, 0, -days)
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, err := h.repo.List(r.Context(), f, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if items == nil {
		items = []Fault{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /admin/faults/{fingerprint}
func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	f, err := h.repo.ByFingerprint(r.Context(), chi.URLParam(r, "fingerprint"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f == nil {
		httpx.Error(w, r, apperr.NotFound("fault_not_found", "unknown fault"))
		return
	}
	httpx.JSON(w, http.StatusOK, f)
}

// POST /admin/faults/{fingerprint}/resolve — « c'est corrigé ».
//
// ⚠️ ELLE SE ROUVRE TOUTE SEULE si la panne revient (voir `Repository.Record`).
// C'est ce qui empêche une liste de pannes de devenir un mur de mensonges
// polis qu'on cesse de lire.
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	who, _ := auth.UserFromContext(r.Context())
	ok, err := h.repo.Resolve(r.Context(), chi.URLParam(r, "fingerprint"), who)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, apperr.NotFound("fault_not_found", "unknown fault"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"resolved": true})
}
