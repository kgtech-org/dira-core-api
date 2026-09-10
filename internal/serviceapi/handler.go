// Package serviceapi expose au SOCLE ce dont les verticales ont besoin.
//
// ⚠️ Ces routes ne sont JAMAIS appelées par une personne. Elles portent les
// pouvoirs d'un service — débiter un portefeuille, ouvrir un compte, envoyer
// une notification au nom de quelqu'un — et sont donc gardées par un secret
// partagé, pas par un jeton d'utilisateur.
//
// Elles vivent dans un paquet À PART plutôt que dans chaque module, pour une
// raison précise : c'est la SURFACE de service du socle, et une surface qu'on
// peut lire d'un seul fichier est une surface qu'on peut auditer. Éparpillée
// dans cinq modules, personne ne saurait dire ce qu'une verticale peut faire.
package serviceapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Accounts is what a vertical may ask about a person.
//
// ⚠️ Volontairement ÉTROIT : un nom, un téléphone, un identifiant. Pas le
// profil entier, pas les adresses, pas les préférences. Une verticale n'a pas
// besoin de lire un compte pour livrer un repas ou conduire un passager, et
// une route qui rendrait tout serait utilisée pour tout.
type Accounts interface {
	ContactOf(ctx context.Context, userID string) (name, phone string, err error)
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	EnsureMerchantAccount(ctx context.Context, phone, name, password string) (string, error)
	// EnsureAccount ouvre un compte de N'IMPORTE QUEL rôle.
	//
	// ⚠️ Sert au PROVISIONNEMENT — le jeu de démonstration d'une verticale.
	// C'est un pouvoir plus large que les autres routes de ce paquet, et il
	// est gardé par le même secret : un service qui peut créer un
	// administrateur peut tout.
	EnsureAccount(ctx context.Context, role, phone, name, password string) (string, error)
	IDByPhone(ctx context.Context, phone string) (string, error)
}

// Wallets is the money a vertical moves on a person's behalf.
type Wallets interface {
	CreateWallet(ctx context.Context, ownerID, walletType string) error
	Consume(ctx context.Context, ownerID string, amount int, reason, orderID string, ref map[string]any) error
	Credit(ctx context.Context, ownerID string, amount int, reason string) error
	PayOrder(ctx context.Context, userID string, amountXOF int, orderID string) error
	RefundOrder(ctx context.Context, userID string, amountXOF int, orderID string) error
	CreditEarnings(ctx context.Context, ownerID string, amountXOF int, reason, orderID string, ref map[string]any) error
}

// Payments starts a mobile-money payment ON BEHALF OF a person.
//
// ⚠️ Sert au canal WhatsApp : une commande passée hors de l'application n'a
// pas de session, et personne pour appuyer sur « payer ». C'est la verticale
// qui déclenche, au nom du client.
type Payments interface {
	InitiateFor(ctx context.Context, clientID, purpose, refID string, amountXOF int) (paymentURL string, err error)
}

// Notifier sends one templated message to one person.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars map[string]string, data map[string]string)
}

// Handler exposes the service-to-service routes.
type Handler struct {
	accounts Accounts
	wallets  Wallets
	notifier Notifier
	payments Payments
}

func NewHandler(a Accounts, w Wallets, n Notifier, p Payments) *Handler {
	return &Handler{accounts: a, wallets: w, notifier: n, payments: p}
}

// Mount registers the routes under a middleware that checks the service token.
func (h *Handler) Mount(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)

		g.Post("/internal/accounts/contact", h.contact)
		g.Post("/internal/accounts/names", h.names)
		g.Post("/internal/accounts/ensure-merchant", h.ensureMerchant)
		g.Post("/internal/accounts/ensure", h.ensureAccount)
		g.Post("/internal/accounts/by-phone", h.byPhone)

		g.Post("/internal/wallets/create", h.createWallet)
		g.Post("/internal/wallets/consume", h.consume)
		g.Post("/internal/wallets/credit", h.credit)
		g.Post("/internal/wallets/pay-order", h.payOrder)
		g.Post("/internal/wallets/refund-order", h.refundOrder)
		g.Post("/internal/wallets/credit-earnings", h.creditEarnings)

		g.Post("/internal/notifications/send", h.notify)
		g.Post("/internal/payments/initiate", h.initiatePayment)
	})
}

// --- comptes ---

func (h *Handler) contact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id" validate:"required,len=24,hexadecimal"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	name, phone, err := h.accounts.ContactOf(r.Context(), req.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"name": name, "phone": phone})
}

func (h *Handler) names(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// Borné : une verticale qui demanderait dix mille noms d'un coup fait
		// une jointure, pas une résolution — et c'est une requête qu'on ne
		// veut pas servir.
		IDs []string `json:"ids" validate:"required,min=1,max=200,dive,len=24,hexadecimal"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	names, err := h.accounts.UserNames(r.Context(), req.IDs)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"names": names})
}

func (h *Handler) ensureMerchant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone    string `json:"phone" validate:"required,e164"`
		Name     string `json:"name" validate:"required,min=1,max=120"`
		Password string `json:"password" validate:"omitempty,min=8,max=128"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := h.accounts.EnsureMerchantAccount(r.Context(), req.Phone, req.Name, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"user_id": id})
}

func (h *Handler) ensureAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role     string `json:"role" validate:"required,oneof=client driver merchant admin"`
		Phone    string `json:"phone" validate:"required,e164"`
		Name     string `json:"name" validate:"required,min=1,max=120"`
		Password string `json:"password" validate:"required,min=8,max=128"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := h.accounts.EnsureAccount(r.Context(), req.Role, req.Phone, req.Name, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"user_id": id})
}

func (h *Handler) byPhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone" validate:"required,e164"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := h.accounts.IDByPhone(r.Context(), req.Phone)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"user_id": id})
}

// --- portefeuilles ---

// walletRequest porte tout ce qu'un mouvement d'argent demande.
//
// ⚠️ `order_id` n'est pas décoratif : c'est lui qui rend le mouvement
// TRAÇABLE, et c'est la seule chose qui permette d'expliquer un débit six mois
// plus tard. Un mouvement sans référence est indiscernable d'une erreur.
type walletRequest struct {
	OwnerID string         `json:"owner_id" validate:"required,len=24,hexadecimal"`
	Amount  int            `json:"amount" validate:"required,gt=0"`
	Reason  string         `json:"reason" validate:"omitempty,max=60"`
	OrderID string         `json:"order_id" validate:"omitempty,len=24,hexadecimal"`
	Ref     map[string]any `json:"ref"`
	Type    string         `json:"type" validate:"omitempty,oneof=driver merchant client"`
}

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var req walletRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.wallets.CreateWallet(r.Context(), req.OwnerID, req.Type); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) consume(w http.ResponseWriter, r *http.Request) {
	h.move(w, r, func(ctx context.Context, req walletRequest) error {
		return h.wallets.Consume(ctx, req.OwnerID, req.Amount, req.Reason, req.OrderID, req.Ref)
	})
}

func (h *Handler) credit(w http.ResponseWriter, r *http.Request) {
	h.move(w, r, func(ctx context.Context, req walletRequest) error {
		return h.wallets.Credit(ctx, req.OwnerID, req.Amount, req.Reason)
	})
}

func (h *Handler) payOrder(w http.ResponseWriter, r *http.Request) {
	h.move(w, r, func(ctx context.Context, req walletRequest) error {
		return h.wallets.PayOrder(ctx, req.OwnerID, req.Amount, req.OrderID)
	})
}

func (h *Handler) refundOrder(w http.ResponseWriter, r *http.Request) {
	h.move(w, r, func(ctx context.Context, req walletRequest) error {
		return h.wallets.RefundOrder(ctx, req.OwnerID, req.Amount, req.OrderID)
	})
}

func (h *Handler) creditEarnings(w http.ResponseWriter, r *http.Request) {
	h.move(w, r, func(ctx context.Context, req walletRequest) error {
		return h.wallets.CreditEarnings(ctx, req.OwnerID, req.Amount, req.Reason, req.OrderID, req.Ref)
	})
}

// move décode, exécute, et rend le code d'erreur MÉTIER tel quel.
//
// ⚠️ Le code compte : `insufficient_funds` doit traverser le réseau et arriver
// intact chez la verticale, qui le rend à son tour au client. Le remplacer par
// un 500 générique ferait afficher « erreur serveur » à un client dont le solde
// est simplement insuffisant.
func (h *Handler) move(w http.ResponseWriter, r *http.Request, do func(context.Context, walletRequest) error) {
	var req walletRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := do(r.Context(), req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- notifications ---

func (h *Handler) notify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string            `json:"user_id" validate:"required,len=24,hexadecimal"`
		Key    string            `json:"key" validate:"required,max=60"`
		Vars   map[string]string `json:"vars"`
		Data   map[string]string `json:"data"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// `Notify` ne rend PAS d'erreur, et c'est délibéré côté socle : une
	// notification perdue ne doit jamais faire échouer ce qui l'a déclenchée.
	// La verticale n'a donc rien à attendre non plus.
	h.notifier.Notify(r.Context(), req.UserID, req.Key, req.Vars, req.Data)
	w.WriteHeader(http.StatusAccepted)
}

// --- paiements ---

func (h *Handler) initiatePayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id" validate:"required,len=24,hexadecimal"`
		Purpose  string `json:"purpose" validate:"required,max=30"`
		RefID    string `json:"ref_id" validate:"required,len=24,hexadecimal"`
		Amount   int    `json:"amount" validate:"required,gt=0"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	url, err := h.payments.InitiateFor(r.Context(), req.ClientID, req.Purpose, req.RefID, req.Amount)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{"payment_url": url})
}
