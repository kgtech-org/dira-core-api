package promocode

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers what a CONNECTED person and the console may do.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		// ⚠️ LE CODE DE PARRAINAGE DE QUELQU'UN, à lui seul. Pas de route qui
		// rendrait le code d'un autre : ce serait un annuaire de codes, et le
		// premier usage en serait de les épuiser.
		g.Get("/me/referral", h.myReferral)

		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/promo-codes", h.list)
		g.With(admin).Post("/admin/promo-codes", h.create)
		g.With(admin).Get("/admin/promo-codes/{code}", h.one)
		g.With(admin).Patch("/admin/promo-codes/{code}", h.patch)

		g.With(admin).Get("/admin/influencers", h.listInfluencers)
		g.With(admin).Post("/admin/influencers", h.createInfluencer)
		g.With(admin).Get("/admin/influencers/{id}", h.oneInfluencer)
		g.With(admin).Patch("/admin/influencers/{id}", h.patchInfluencer)
	})
}

// MountService registers what a VERTICAL may ask: quote, redeem, settle,
// release.
//
// ⚠️ QUATRE ROUTES ET PAS UNE, parce qu'une remise a quatre moments : la
// chiffrer (sans rien consommer), la réserver (l'opération est commandée), la
// régler (l'argent est sorti) et la rendre (annulation). Une seule route qui
// « applique » aurait consommé l'enveloppe au devis — et le code aurait été
// épuisé par des gens qui regardaient le prix.
func (h *Handler) MountService(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Post("/internal/promo-codes/quote", h.svcQuote)
		g.Post("/internal/promo-codes/redeem", h.svcRedeem)
		g.Post("/internal/promo-codes/settle", h.svcSettle)
		g.Post("/internal/promo-codes/release", h.svcRelease)
	})
}

// GET /me/referral — mon code de parrainage, tiré au besoin.
func (h *Handler) myReferral(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	code, pol, err := h.svc.MyReferral(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := map[string]any{
		// ⚠️ `enabled` EXPLICITE, et non déduit d'un code absent. « Pas de
		// code » peut vouloir dire « le parrainage est éteint dans ce pays »
		// ou « on n'a pas su le tirer » : une application doit pouvoir CACHER
		// le bouton plutôt que d'afficher un écran vide.
		"enabled":       code != nil,
		"invitee_xof":   pol.InviteeXOF,
		"sponsor_xof":   pol.SponsorXOF,
		"max_sponsored": pol.MaxSponsored,
	}
	if code != nil {
		out["code"] = code.Code
		out["ends_at"] = code.EndsAt
		out["uses"] = code.Counters.Uses()
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.repo.List(r.Context(), r.URL.Query().Get("kind"), nil, 0)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, codeRow(&items[i]))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req NewCodeRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	c, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, codeRow(c))
}

// GET /admin/promo-codes/{code} — un code ET SON SUIVI.
//
// ⚠️ LE SUIVI AVEC LE CODE, et non sur une route à part. « Combien ce code a-t-il
// servi » est la seule question qu'on se pose en l'ouvrant : la servir ailleurs
// aurait fait un écran qui demande un second clic pour dire l'essentiel.
func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.repo.ByCode(r.Context(), Normalise(chi.URLParam(r, "code")))
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	if c == nil {
		httpx.Error(w, r, errCodeUnknown)
		return
	}
	stats, err := h.svc.repo.StatsOf(r.Context(), c.Code)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	history, err := h.svc.repo.Ledger().History(r.Context(), c.Code, 50)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	row := codeRow(c)
	row["stats"] = stats
	row["uses_history"] = history
	httpx.JSON(w, http.StatusOK, row)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Active *bool `json:"active"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if req.Active == nil {
		httpx.Error(w, r, apperr.Validation("nothing to update: send active"))
		return
	}
	if err := h.svc.SetActive(r.Context(), chi.URLParam(r, "code"), *req.Active); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// codeRow est un code tel que la console le lit.
//
// ⚠️ `remaining_xof` EST CALCULÉ ICI, et il vaut -1 quand il n'y a pas de
// budget. « Pas de plafond » n'est pas « zéro restant », et les confondre
// afficherait une enveloppe épuisée sur une campagne sans budget.
//
// ⚠️ ET L'ENVELOPPE EST SERVIE DANS LA MÊME FORME QUE CELLE D'UNE PROMOTION
// (`uses_reserved`, `uses_spent`, `uses_released`, `progress_pct`,
// `exhausted`). Les deux objets ne sont pas les mêmes — une promotion s'applique
// d'elle-même, un code se tape — mais LEUR ARGENT SE COMPTE PAREIL, par le même
// moteur (`pkg/promo`). Deux formes auraient obligé la console à deux lectures
// du même chiffre, donc à deux composants, donc à deux façons de se tromper ; et
// la première divergence serait passée inaperçue puisque les deux écrans auraient
// l'air de marcher.
//
// ⚠️ `uses` RESTE À CÔTÉ de `uses_reserved` + `uses_spent` dont il est la somme.
// C'est une redondance assumée : une liste de codes n'a la place que d'un seul
// nombre, et le faire calculer par chaque appelant est exactement la façon dont
// un total finit par différer d'un écran à l'autre.
func codeRow(c *Code) map[string]any {
	return map[string]any{
		"code": c.Code, "kind": c.Kind, "label": c.Label,
		"country": c.Country, "owner_id": ownerHex(c),
		"verticals": c.Verticals, "discount_kind": c.DiscountKind, "value": c.Value,
		"max_discount_xof": c.MaxDiscountXOF, "min_amount_xof": c.MinAmountXOF,
		"budget_xof": c.BudgetXOF, "max_uses": c.MaxUses,
		"max_uses_per_user": c.MaxUsesPerUser,
		"remaining_xof":     c.Limits.Remaining(c.Counters),
		"uses":              c.Counters.Uses(),
		"uses_reserved":     c.UsesReserved,
		"uses_spent":        c.UsesSpent,
		"uses_released":     c.UsesReleased,
		"spent_xof":         c.AmountSpent,
		"committed_xof":     c.Counters.Committed(),
		"progress_pct":      promo.Progress(c.Limits, c.Counters),
		"exhausted":         promo.Exhausted(c.Limits, c.Counters),
		"live":              c.Live(time.Now()),
		"starts_at":         c.StartsAt, "ends_at": c.EndsAt,
		"active": c.Active, "created_at": c.CreatedAt,
	}
}

// --- LES INFLUENCEURS -----------------------------------------------------

func (h *Handler) listInfluencers(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.repo.ListInfluencers(r.Context(), 0)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createInfluencer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id" validate:"required,len=24,hexadecimal"`
		Handle   string `json:"handle" validate:"required,max=40"`
		Network  string `json:"network" validate:"omitempty,max=20"`
		Audience int    `json:"audience" validate:"min=0"`
		Note     string `json:"note" validate:"omitempty,max=300"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	i, err := h.svc.CreateInfluencer(r.Context(), req.UserID, req.Handle, req.Network, req.Note, req.Audience)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, i)
}

// GET /admin/influencers/{id} — la fiche, ses codes, et CE QUE CHACUN A DONNÉ.
func (h *Handler) oneInfluencer(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, apperr.NotFound("influencer_not_found", "influencer not found"))
		return
	}
	i, err := h.svc.repo.InfluencerByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	if i == nil {
		httpx.Error(w, r, apperr.NotFound("influencer_not_found", "influencer not found"))
		return
	}
	codes, err := h.svc.repo.List(r.Context(), KindInfluencer, &id, 0)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	rows := make([]map[string]any, 0, len(codes))
	total := Stats{}
	for k := range codes {
		st, err := h.svc.repo.StatsOf(r.Context(), codes[k].Code)
		if err != nil {
			httpx.Error(w, r, apperr.Internal(err))
			return
		}
		row := codeRow(&codes[k])
		row["stats"] = st
		rows = append(rows, row)
		total.Uses += st.Uses
		total.DiscountXOF += st.DiscountXOF
		total.Reserved += st.Reserved
		total.Released += st.Released
		// ⚠️ LES PERSONNES NE S'ADDITIONNENT PAS entre codes — la même
		// personne peut avoir utilisé deux codes du même influenceur. On rend
		// le total par code, et on laisse `people` du total à zéro plutôt que
		// d'afficher une somme fausse.
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"influencer": i, "codes": rows, "total": total,
	})
}

func (h *Handler) patchInfluencer(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, apperr.NotFound("influencer_not_found", "influencer not found"))
		return
	}
	var req struct {
		Handle   *string `json:"handle" validate:"omitempty,max=40"`
		Network  *string `json:"network" validate:"omitempty,max=20"`
		Audience *int    `json:"audience" validate:"omitempty,min=0"`
		Note     *string `json:"note" validate:"omitempty,max=300"`
		Active   *bool   `json:"active"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	set := bson.M{}
	if req.Handle != nil {
		set["handle"] = NormaliseHandle(*req.Handle)
	}
	if req.Network != nil {
		if !ValidNetwork(*req.Network) {
			httpx.Error(w, r, apperr.Validation("unknown network").
				WithMeta(map[string]any{"fields": []string{"network"}, "allowed": Networks}))
			return
		}
		set["network"] = *req.Network
	}
	if req.Audience != nil {
		set["audience"] = *req.Audience
	}
	if req.Note != nil {
		set["note"] = *req.Note
	}
	if req.Active != nil {
		set["active"] = *req.Active
	}
	if len(set) == 0 {
		httpx.Error(w, r, apperr.Validation("nothing to update"))
		return
	}
	if err := h.svc.repo.UpdateInfluencer(r.Context(), id, set); err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- CE QU'UNE VERTICALE DEMANDE ------------------------------------------

func (h *Handler) svcQuote(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRedeem(r, false)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Quote(r.Context(), req.Code, req.UserID, req.Vertical, req.AmountXOF)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) svcRedeem(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRedeem(r, true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Redeem(r.Context(), req.Code, req.UserID, req.Vertical, req.RefID, req.AmountXOF)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) svcSettle(w http.ResponseWriter, r *http.Request) {
	ref, err := decodeRef(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Settle(r.Context(), ref); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) svcRelease(w http.ResponseWriter, r *http.Request) {
	ref, err := decodeRef(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Release(r.Context(), ref); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type redeemRequest struct {
	Code      string `json:"code" validate:"required,max=32"`
	UserID    string `json:"user_id" validate:"required,len=24,hexadecimal"`
	Vertical  string `json:"vertical" validate:"required,oneof=vtc food"`
	RefID     string `json:"ref_id"`
	AmountXOF int    `json:"amount_xof" validate:"required,min=1"`
}

func decodeRedeem(r *http.Request, needRef bool) (redeemRequest, error) {
	var req redeemRequest
	if err := httpx.Decode(r, &req); err != nil {
		return req, err
	}
	if needRef && req.RefID == "" {
		return req, apperr.Validation("ref_id is required to redeem a code")
	}
	return req, nil
}

func decodeRef(r *http.Request) (string, error) {
	var req struct {
		RefID string `json:"ref_id" validate:"required"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return "", err
	}
	return req.RefID, nil
}
