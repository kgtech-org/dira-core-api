package challenge

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

// Handler sert les objectifs.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount pose les routes.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		// ⚠️ UNE SEULE ROUTE POUR LES TROIS PUBLICS, et le PUBLIC VIENT DU
		// RÔLE. Trois routes (`/drivers/me/challenges`, …) auraient fait
		// trois écrans à maintenir pour la même chose — et surtout, un
		// paramètre de public aurait laissé un client lire les objectifs des
		// chauffeurs, puis en réclamer le bonus.
		g.Get("/me/challenges", h.mine)
	})
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/challenges", h.list)
		g.Post("/admin/challenges", h.create)
		g.Get("/admin/challenges/{id}", h.one)
		g.Put("/admin/challenges/{id}", h.update)
		// ⚠️ LANCER EST UN SECOND GESTE. Un objectif qui démarrerait à
		// l'écriture afficherait une faute de frappe à dix mille personnes
		// avant qu'on la voie — et une cible fausse déjà atteinte ne se retire
		// plus.
		g.Post("/admin/challenges/{id}/launch", h.launch)
		g.Post("/admin/challenges/{id}/end", h.end)
		g.Get("/admin/challenges/{id}/winners", h.winners)
		// Les mesures proposables, par public — ce que l'écran de création
		// affiche. ⚠️ Avec les mesures REFUSÉES et leur raison : la question
		// « pourquoi pas le taux d'acceptation ? » doit trouver sa réponse là
		// où elle se pose.
		g.Get("/admin/challenges/metrics", h.metrics)
	})
}

// GET /me/challenges — mes objectifs en cours.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	role, _ := auth.RoleFromContext(r.Context())
	out, err := h.svc.Mine(r.Context(), userID, audienceOfRole(role))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// audienceOfRole traduit un rôle en public.
//
// ⚠️ `driver` COUVRE LES DEUX MÉTIERS D'AGENT, et c'est une limite du jeton :
// un chauffeur VTC et un livreur portent le même rôle. On ne peut donc pas les
// distinguer ici — c'est la MESURE qui s'en charge, puisque `rides_done` ne
// bouge que pour qui conduit et `deliveries_done` que pour qui livre. Un livreur
// voit donc l'objectif des chauffeurs à zéro, et c'est le seul défaut connu de
// cette route : il sera corrigé quand le jeton distinguera les deux.
func audienceOfRole(role string) string {
	switch role {
	case auth.RoleDriver:
		return ForDriver
	case auth.RoleClient:
		return ForClient
	default:
		return ""
	}
}

// GET /admin/challenges?audience=&status=&live=&limit=
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := h.svc.List(r.Context(), Filter{
		Audience: q.Get("audience"),
		Status:   q.Get("status"),
		Live:     q.Get("live") == "true",
	}, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// Input est ce que la console envoie.
//
// ⚠️ PAS DE `status` NI DE COMPTEURS : l'état se change par `launch` et `end`,
// et les compteurs appartiennent au serveur. Les accepter aurait laissé une
// console remettre une enveloppe à zéro — et repayer tout le monde.
type Input struct {
	Audience    string `json:"audience"`
	Metric      string `json:"metric"`
	Target      int    `json:"target"`
	RewardXOF   int    `json:"reward_xof"`
	Title       string `json:"title" validate:"omitempty,max=120"`
	Description string `json:"description" validate:"omitempty,max=1000"`
	From        string `json:"from"`
	To          string `json:"to"`
	Repeat      string `json:"repeat"`
	BudgetXOF   int    `json:"budget_xof" validate:"omitempty,min=0"`
	MaxWinners  int    `json:"max_winners" validate:"omitempty,min=0"`
}

var errBadDate = apperr.Validation("from and to are required: `2026-10-12` or RFC 3339").
	WithMeta(map[string]any{"fields": []string{"from", "to"}})

func (in Input) challenge() (*Challenge, error) {
	from, err := parseDay(in.From)
	if err != nil {
		return nil, err
	}
	to, err := parseDay(in.To)
	if err != nil {
		return nil, err
	}
	c := &Challenge{
		Audience: in.Audience, Metric: in.Metric, Target: in.Target,
		RewardXOF: in.RewardXOF, Title: in.Title, Description: in.Description,
		Window: Window{From: from, To: to}, Repeat: in.Repeat,
	}
	c.Limits.BudgetXOF = in.BudgetXOF
	c.Limits.MaxUses = in.MaxWinners
	// ⚠️ UN GAGNANT NE GAGNE QU'UNE FOIS, et ce n'est pas réglable. Un objectif
	// qui se gagnerait deux fois par la même personne serait un tarif, pas un
	// objectif : « 3 courses = 5 000 F » répété serait une prime au volume, donc
	// exactement le mécanisme qu'on refuse ailleurs dans ce paquet.
	c.Limits.MaxUsesPerUser = 1
	return c, nil
}

// parseDay accepte un jour ou un instant RFC 3339.
func parseDay(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, errBadDate
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, errBadDate
	}
	return t, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	c, err := in.challenge()
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Create(r.Context(), actorID, c)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := toResponse(out, 0, 0)
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	c, err := in.challenge()
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), c)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := toResponse(out, 0, 0)
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) launch(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Launch(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := toResponse(out, 0, 0)
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) end(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.End(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := toResponse(out, 0, 0)
	httpx.JSON(w, http.StatusOK, resp)
}

// GET /admin/challenges/{id}/winners — qui a gagné, et qui a été manqué.
func (h *Handler) winners(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.WinnersOf(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// GET /admin/challenges/metrics — ce qu'on peut mesurer, et ce qu'on refuse.
//
// ⚠️ LES REFUS SONT SERVIS AVEC LEUR RAISON. La question « pourquoi ne peut-on
// pas faire un objectif sur le taux d'acceptation ? » reviendra — elle revient
// toujours —, et la réponse doit être là où elle se pose, pas dans un compte
// rendu de réunion. Les trois mesures écartées sont techniquement disponibles :
// c'est un refus de produit, pas un manque.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	type metricOut struct {
		Key       string   `json:"key"`
		Label     string   `json:"label"`
		For       []string `json:"for"`
		Money     bool     `json:"money"`
		MaxPerDay int      `json:"max_per_day,omitempty"`
	}
	out := make([]metricOut, 0, len(Metrics))
	for _, m := range Metrics {
		out = append(out, metricOut{
			Key: m.Key, Label: m.Label, For: m.For, Money: m.Money, MaxPerDay: m.MaxPerDay,
		})
	}
	refused := make([]map[string]string, 0, len(Forbidden))
	for key, reason := range Forbidden {
		refused = append(refused, map[string]string{"key": key, "reason": reason})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"refused":  refused,
		"max_days": MaxDays,
	})
}
