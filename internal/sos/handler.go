package sos

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler sert les routes d'alerte.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount pose les routes.
//
// ⚠️ DÉCLENCHER N'EXIGE AUCUN RÔLE — seulement un compte. C'est la décision
// d'accès la plus importante du module : un passager, un chauffeur, un livreur,
// un marchand, un membre du staff en déplacement. Restreindre aux rôles
// « agents » aurait laissé le passager, qui est précisément la partie la plus
// exposée d'une course, sans bouton.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/sos", h.raise)
		g.Get("/sos/settings", h.settings)
		g.Get("/sos/me", h.mine)
		g.Post("/sos/{id}/position", h.position)
		g.Post("/sos/{id}/cancel", h.cancel)
	})
	r.Group(func(g chi.Router) {
		// ⚠️ LA PORTÉE EST `core`, PAS `vtc` NI `food`. Une personne en danger
		// n'est pas un sujet de verticale : un opérateur habilité aux seules
		// commandes doit pouvoir traiter l'alerte d'un chauffeur qui passe
		// devant lui. Les notifications, elles, sont bien dirigées par
		// verticale — mais l'écran reste ouvert à toute l'exploitation.
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/sos", h.live)
		g.Get("/admin/sos/history", h.history)
		g.Get("/admin/sos/{id}", h.one)
		g.Post("/admin/sos/{id}/ack", h.ack)
		g.Post("/admin/sos/{id}/close", h.close)
	})
}

// POST /sos — DÉCLENCHER.
//
// ⚠️ UN CORPS ILLISIBLE NE FAIT PAS ÉCHOUER L'APPEL. `httpx.Decode` rendrait un
// `422` sur un JSON tronqué par une coupure réseau — exactement la situation où
// l'on déclenche une alerte. On garde ce qu'on a pu lire et on écrit l'alerte :
// « quelqu'un a appuyé, on ne sait rien d'autre » est l'information la plus
// importante de ce module, et elle ne doit dépendre d'aucun champ.
func (h *Handler) raise(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var in RaiseInput
	_ = httpx.Decode(r, &in)
	out, err := h.svc.Raise(r.Context(), userID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// GET /sos/settings — ce que ce pays décide des détections et des numéros.
func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.SettingsFor(r.Context()))
}

// GET /sos/me — mon alerte vivante, pour retrouver l'écran.
//
// ⚠️ `200` AVEC `null`, ET NON `404`. Un `404` sur « je n'ai pas d'alerte en
// cours » aurait fait traiter le cas normal comme une erreur dans chaque
// application — et une application qui se relance après un choc appelle
// précisément cette route.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Mine(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"alert": out})
}

// PositionInput : où la personne est MAINTENANT.
type PositionInput struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	AccuracyM int     `json:"accuracy_m"`
}

// POST /sos/{id}/position
func (h *Handler) position(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var in PositionInput
	_ = httpx.Decode(r, &in)
	if err := h.svc.Position(r.Context(), userID, chi.URLParam(r, "id"), in.Lat, in.Lng, in.AccuracyM); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /sos/{id}/cancel — « fausse alerte », par la personne elle-même.
func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Cancel(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/sos — la file.
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, n, err := h.svc.Live(r.Context(), limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// `live` est le compte de ce qui est VRAIMENT ouvert — la pastille. Il
	// diffère de `len(items)`, qui comprend les annulées récentes.
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "live": n})
}

// GET /admin/sos/history?status=&since=&limit=
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	var since *time.Time
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = &t
		}
	}
	items, err := h.svc.History(r.Context(), q.Get("status"), since, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /admin/sos/{id}
func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/sos/{id}/ack — « je m'en occupe ».
func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Acknowledge(r.Context(), actorID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CloseInput : le dénouement, exigé.
type CloseInput struct {
	Outcome    string `json:"outcome"`
	Resolution string `json:"resolution" validate:"omitempty,max=2000"`
}

// POST /admin/sos/{id}/close
func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var in CloseInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Close(r.Context(), actorID, chi.URLParam(r, "id"), in.Outcome, in.Resolution)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
