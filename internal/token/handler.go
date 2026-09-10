package token

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler exposes the token HTTP endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the PORTEFEUILLE routes on the /api/v1 router.
// authMW is the JWT middleware built at wiring time.
//
// ⚠️ LA PROPULSION N'EST PAS ICI — voir `MountCatalogueSpending`. Dépenser des
// jetons sur un PLAT ou un POINT DE VENTE demande le catalogue de la
// livraison, que le socle ne connaît pas. Les deux gestes vivaient dans le même
// module tant qu'un seul service existait ; le partage passe entre les deux.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		// Le client y lit son « Dira Cash » : les mêmes routes, un contenu
		// différent — de l'argent au lieu de jetons.
		g.Use(middleware.RequireRole(auth.RoleDriver, auth.RoleMerchant, auth.RoleClient))
		g.Get("/wallet", h.getWallet)
		g.Get("/wallet/transactions", h.listTransactions)
		g.Post("/wallet/purchase", h.purchase)
	})

	// L'administration des portefeuilles. GÉNÉRIQUE : un propriétaire est un
	// point de vente, un livreur ou un chauffeur — le socle ne fait pas la
	// différence, et n'a pas à la faire.
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Use(middleware.RequireRole(auth.RoleAdmin))
		// Solde d'UN portefeuille, prix unitaire compris. La console lisait
		// jusqu'ici la liste complète des portefeuilles pour y chercher le
		// sien : correct sur un jeu de démo, faux dès la deuxième page.
		g.Get("/admin/wallets/{ownerID}", h.adminWallet)
		// Les deux gestes de recharge, indexés sur le PROPRIÉTAIRE du
		// portefeuille : un opérateur a une ligne de portefeuille sous les
		// yeux, pas un rôle — et les livreurs achètent des jetons eux aussi.
		//
		// `purchase` initie un vrai paiement à honorer ; `credit` accorde des
		// jetons SANS contrepartie financière, justificatif exigé.
		g.Post("/admin/wallets/{ownerID}/purchase", h.adminPurchase)
		g.Post("/admin/wallets/{ownerID}/credit", h.adminCredit)
	})
}

// MountCatalogueSpending monte les dépenses qui portent sur le CATALOGUE d'une
// verticale : propulser un plat, acheter une option de point de vente.
//
// ⚠️ Montée SÉPARÉMENT, et seulement si les collaborateurs sont branchés. Ces
// routes ont besoin de savoir ce qu'est un plat et à qui appartient une
// boutique — deux choses que le socle ignore. Les monter avec des
// collaborateurs absents servirait des routes qui échouent, ce qui est pire
// que des routes qui n'existent pas : la console croirait le geste possible.
//
// Elles restent ici, et non dans la livraison, parce que c'est le PORTEFEUILLE
// qu'elles débitent — et il est au socle. Le jour où la livraison expose son
// catalogue au socle, elles s'allument sans bouger d'un fichier.
func (h *Handler) MountCatalogueSpending(r chi.Router, authMW func(http.Handler) http.Handler) {
	if h.svc == nil || !h.svc.CanSpendOnCatalogue() {
		return
	}
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Use(middleware.RequireRole(auth.RoleMerchant))
		g.Post("/stores/{id}/dishes/{dish_id}/boost", h.boostDish)
		g.Post("/stores/{id}/options", h.buyOption)
	})
	// Émulateur marchand de la console : même méthode de service, propriété
	// levée pour l'administrateur. Le jeton est bien débité au point de vente.
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Use(middleware.RequireRole(auth.RoleAdmin))
		g.Post("/admin/stores/{id}/dishes/{dish_id}/boost", h.boostDish)
	})
}

// GET /admin/wallets/{ownerID} — solde d'un portefeuille, quel que soit son
// type : le propriétaire est un point de vente ou un livreur.
func (h *Handler) adminWallet(w http.ResponseWriter, r *http.Request) {
	_, _, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	wallet, err := h.svc.WalletOf(r.Context(), chi.URLParam(r, "ownerID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, wallet)
}

// POST /admin/wallets/{ownerID}/purchase — achat de jetons pour le compte du
// propriétaire d'un portefeuille. Initie un vrai paiement : le solde ne
// bougera qu'à sa confirmation par le prestataire.
func (h *Handler) adminPurchase(w http.ResponseWriter, r *http.Request) {
	userID, _, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req PurchaseRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Le portefeuille visé vient de l'URL : le corps ne doit pas pouvoir en
	// désigner un autre.
	resp, err := h.svc.PurchaseForOwner(r.Context(), userID, chi.URLParam(r, "ownerID"), req.Tokens)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, resp)
}

// POST /admin/wallets/{ownerID}/credit — crédit opérateur, sans paiement.
func (h *Handler) adminCredit(w http.ResponseWriter, r *http.Request) {
	userID, _, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req OperatorCreditRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	wallet, err := h.svc.CreditByOperator(r.Context(), userID, chi.URLParam(r, "ownerID"), req.Amount, req.Justification)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, wallet)
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	userID, role, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	wallet, err := h.svc.GetWallet(r.Context(), userID, role, r.URL.Query().Get("store_id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, wallet)
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	userID, role, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	page := httpx.PageFromRequest(r)
	items, next, err := h.svc.ListTransactions(r.Context(), userID, role, r.URL.Query().Get("store_id"), page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) purchase(w http.ResponseWriter, r *http.Request) {
	userID, role, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req PurchaseRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.Purchase(r.Context(), userID, role, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, resp)
}

func (h *Handler) boostDish(w http.ResponseWriter, r *http.Request) {
	userID, role, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.BoostDish(r.Context(), userID, role, chi.URLParam(r, "id"), chi.URLParam(r, "dish_id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) buyOption(w http.ResponseWriter, r *http.Request) {
	userID, _, err := caller(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var req BuyOptionRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.BuyOption(r.Context(), userID, chi.URLParam(r, "id"), req.Option)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func caller(r *http.Request) (userID, role string, err error) {
	userID, ok := auth.UserFromContext(r.Context())
	if !ok {
		return "", "", apperr.Unauthorized("missing_token", "missing bearer token")
	}
	role, _ = auth.RoleFromContext(r.Context())
	return userID, role, nil
}
