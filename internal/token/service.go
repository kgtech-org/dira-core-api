package token

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// DefaultTokenPriceXOF is the unit token price used at wiring time.
// ⚠️ Business decision to validate: 100 XOF per token.
const DefaultTokenPriceXOF = 100

// DefaultBoostCost is the dish boost cost in tokens.
// ⚠️ Business decision to validate: 5 tokens per boost.
const DefaultBoostCost = 5

// Repo abstracts persistence for the token service. Implemented by
// *Repository; faked in tests (fakes must reproduce the conditional-update
// and duplicate-key semantics).
type Repo interface {
	CreateWallet(ctx context.Context, w *Wallet) error
	FindWalletByOwner(ctx context.Context, ownerID primitive.ObjectID) (*Wallet, error)
	ConsumeAtomic(ctx context.Context, walletID primitive.ObjectID, amount int) (bool, error)
	Credit(ctx context.Context, walletID primitive.ObjectID, amount int) error
	CreditField(ctx context.Context, walletID primitive.ObjectID, field string, amount int) error
	InsertTransaction(ctx context.Context, t *Transaction) error
	// SpendMoney débite l'argent d'un portefeuille, le promotionnel d'abord,
	// en UNE opération atomique.
	SpendMoney(ctx context.Context, walletID primitive.ObjectID, amount int) (fromPromo, fromCash int, err error)
	ListTransactions(ctx context.Context, walletID primitive.ObjectID, limit int, cursor string) ([]Transaction, string, error)
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error

	// ClaimOperation réserve un mouvement d'argent, ou dit qu'il a déjà eu
	// lieu. Appelée DANS la transaction qui l'applique — voir idempotency.go.
	ClaimOperation(ctx context.Context, key string) error
}

// PurchaseInitiator starts a mobile-money payment for a token purchase.
// Wiring injects the payment service.
type PurchaseInitiator interface {
	InitiatePurchase(ctx context.Context, userID, walletOwnerID string, tokens int, amountXOF int) (paymentID string, err error)
}

// DishBooster marks a dish as boosted. Wiring injects the dish service.
type DishBooster interface {
	SetBoosted(ctx context.Context, dishID string, boosted bool) error
}

// StoreOwnership checks that a user owns a store. Wiring injects the merchant
// service.
type StoreOwnership interface {
	OwnsStore(ctx context.Context, userID, storeID string) (bool, error)
}

var errInsufficientTokens = apperr.PaymentRequired("insufficient_tokens", "insufficient token balance")

// Service implements the token business logic and the cross-module provider
// contract (CreateWallet, Consume, Credit).
type Service struct {
	repo     Repo
	audit    *audit.Recorder
	payments PurchaseInitiator
	booster  DishBooster
	stores   StoreOwnership

	tokenPriceXOF int            // ⚠️ unit price of one token, XOF
	boostCost     int            // ⚠️ dish boost cost, tokens
	optionCatalog map[string]int // ⚠️ option name -> cost in tokens
}

// NewService builds the token service. tokenPriceXOF, boostCost and
// optionCatalog are business parameters pending validation (⚠️); use
// DefaultTokenPriceXOF / DefaultBoostCost at wiring time until decided.
func NewService(repo Repo, auditRec *audit.Recorder, payments PurchaseInitiator, booster DishBooster, stores StoreOwnership, tokenPriceXOF, boostCost int, optionCatalog map[string]int) *Service {
	if tokenPriceXOF <= 0 {
		tokenPriceXOF = DefaultTokenPriceXOF
	}
	if boostCost <= 0 {
		boostCost = DefaultBoostCost
	}
	return &Service{
		repo:          repo,
		audit:         auditRec,
		payments:      payments,
		booster:       booster,
		stores:        stores,
		tokenPriceXOF: tokenPriceXOF,
		boostCost:     boostCost,
		optionCatalog: optionCatalog,
	}
}

// --- Provider contract (cross-module) ---

// CreateWallet creates a wallet for an owner (user id for "driver", store id
// for "merchant"). Idempotent: a duplicate owner is not an error.
func (s *Service) CreateWallet(ctx context.Context, ownerID, walletType string) error {
	if walletType != WalletTypeDriver && walletType != WalletTypeMerchant {
		return apperr.Validation("wallet type must be driver or merchant")
	}
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return apperr.Validation("invalid owner id").WithCause(err)
	}
	wallet := &Wallet{OwnerID: oid, Type: walletType, Balance: 0, UpdatedAt: time.Now().UTC()}
	if err := s.repo.CreateWallet(ctx, wallet); err != nil {
		if errors.Is(err, ErrDuplicateWallet) {
			return nil // idempotent
		}
		return apperr.Internal(err)
	}
	return nil
}

// Consume atomically debits amount tokens from the owner's wallet and records
// the matching transaction in the same MongoDB transaction. Returns
// insufficient_tokens (402) when the balance is lower than amount.
func (s *Service) Consume(ctx context.Context, ownerID string, amount int, reason, refKind, refID string, ref map[string]any) error {
	if amount <= 0 {
		return apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return err
	}

	refOID, err := parseRef(refKind, refID)
	if err != nil {
		return err
	}

	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		// Une dépense rattachée à une COMMANDE se fait une fois : un livreur
		// accepte une course une fois, un marchand propulse un plat une fois.
		// Sans commande, aucune clé naturelle — deux dépenses identiques
		// peuvent être deux gestes voulus.
		if refID != "" {
			if err := s.repo.ClaimOperation(txCtx, consumeKey(ownerID, refKind, refID, reason)); err != nil {
				return err
			}
		}
		ok, err := s.repo.ConsumeAtomic(txCtx, wallet.ID, amount)
		if err != nil {
			return apperr.Internal(err)
		}
		if !ok {
			return errInsufficientTokens
		}
		tx := &Transaction{
			WalletID:  wallet.ID,
			Kind:      KindConsume,
			Reason:    reason,
			Amount:    -amount,
			RefID:     refOID,
			RefKind:   refKind,
			Ref:       ref,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.repo.InsertTransaction(txCtx, tx); err != nil {
			return apperr.Internal(err)
		}
		return nil
	})
	if errors.Is(err, errOperationApplied) {
		// DÉJÀ DÉPENSÉ : un succès. Rendre `insufficient_tokens` sur un
		// réessai ferait refuser une course déjà payée.
		slog.InfoContext(ctx, "token: consume replayed, not applied twice",
			"owner_id", ownerID, "ref_kind", refKind, "ref_id", refID, "reason", reason)
		return nil
	}
	if err != nil {
		return err
	}

	s.record(ctx, "token.consume", wallet.ID.Hex(), map[string]any{
		"amount": -amount, "reason": reason,
		"ref_kind": refKind, "ref_id": refID, "owner_id": ownerID,
	})
	return nil
}

// Credit adds amount tokens to the owner's wallet (called after a confirmed
// payment) and records the matching transaction atomically.
func (s *Service) Credit(ctx context.Context, ownerID string, amount int, reason string) error {
	if amount <= 0 {
		return apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return err
	}

	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Credit(txCtx, wallet.ID, amount); err != nil {
			return apperr.Internal(err)
		}
		tx := &Transaction{
			WalletID:  wallet.ID,
			Kind:      KindPurchase,
			Reason:    reason,
			Amount:    amount,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.repo.InsertTransaction(txCtx, tx); err != nil {
			return apperr.Internal(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.record(ctx, "token.credit", wallet.ID.Hex(), map[string]any{
		"amount": amount, "reason": reason, "owner_id": ownerID,
	})
	return nil
}

// --- HTTP-facing operations ---

// GetWallet returns the caller's wallet: drivers read their own wallet,
// merchants read a store wallet they own (storeID required).
func (s *Service) GetWallet(ctx context.Context, userID, role, storeID string) (WalletResponse, error) {
	ownerID, err := s.resolveOwner(ctx, userID, role, storeID)
	if err != nil {
		return WalletResponse{}, err
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return WalletResponse{}, err
	}
	return newWalletResponse(wallet, s.tokenPriceXOF), nil
}

// WalletOf returns a wallet designated by its OWNER, whatever its type.
// Réservée à l'exploitation : aucune vérification de propriété, la route qui
// l'expose est restreinte à l'administrateur.
func (s *Service) WalletOf(ctx context.Context, ownerID string) (WalletResponse, error) {
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return WalletResponse{}, err
	}
	return newWalletResponse(wallet, s.tokenPriceXOF), nil
}

// ListTransactions returns one page of transactions for the resolved wallet.
func (s *Service) ListTransactions(ctx context.Context, userID, role, storeID string, page httpx.Page) ([]TransactionResponse, string, error) {
	ownerID, err := s.resolveOwner(ctx, userID, role, storeID)
	if err != nil {
		return nil, "", err
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return nil, "", err
	}
	items, next, err := s.repo.ListTransactions(ctx, wallet.ID, page.Limit, page.Cursor)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	resp := make([]TransactionResponse, 0, len(items))
	for _, t := range items {
		resp = append(resp, newTransactionResponse(t))
	}
	return resp, next, nil
}

// Purchase initiates a mobile-money payment for a token top-up. The wallet is
// credited later, when the payment module confirms the payment and calls
// Credit. Merchants top up a store wallet they own (store_id required).
func (s *Service) Purchase(ctx context.Context, userID, role string, req PurchaseRequest) (PurchaseResponse, error) {
	// ⚠️ Un CLIENT ne peut pas acheter de jetons, alors qu'il possède bien un
	// portefeuille : les jetons sont le droit d'entrée d'un livreur et
	// l'outil de promotion d'un marchand. Le sien ne porte que de l'argent,
	// et se recharge par `TopUp`.
	if role == auth.RoleClient {
		return PurchaseResponse{}, apperr.Forbidden("forbidden", "tokens are not sold to clients")
	}
	ownerID, err := s.resolveOwner(ctx, userID, role, req.StoreID)
	if err != nil {
		return PurchaseResponse{}, err
	}
	return s.purchaseForOwner(ctx, userID, ownerID, req.Tokens)
}

// PurchaseForOwner buys tokens for a wallet designated by its OWNER, whoever
// that owner is — un livreur comme un point de vente.
//
// C'est la porte d'entrée de la console : la ligne qu'un opérateur a sous les
// yeux est un portefeuille, pas un rôle. Passer par le propriétaire évite
// d'avoir une route par type de portefeuille, et surtout d'oublier les
// livreurs — qui achètent des jetons eux aussi.
func (s *Service) PurchaseForOwner(ctx context.Context, actorID, ownerID string, tokens int) (PurchaseResponse, error) {
	if tokens <= 0 {
		return PurchaseResponse{}, apperr.Validation("tokens must be positive")
	}
	return s.purchaseForOwner(ctx, actorID, ownerID, tokens)
}

// purchaseForOwner initiates the payment; the wallet is credited later, when
// the provider confirms the settlement.
func (s *Service) purchaseForOwner(ctx context.Context, actorID, ownerID string, tokens int) (PurchaseResponse, error) {
	// The wallet must exist before initiating a payment for it.
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return PurchaseResponse{}, err
	}

	amountXOF := tokens * s.tokenPriceXOF
	paymentID, err := s.payments.InitiatePurchase(ctx, actorID, ownerID, tokens, amountXOF)
	if err != nil {
		return PurchaseResponse{}, apperr.From(err)
	}

	s.record(ctx, "token.purchase_initiated", wallet.ID.Hex(), map[string]any{
		"tokens": tokens, "amount_xof": amountXOF, "payment_id": paymentID, "owner_id": ownerID,
	})
	return PurchaseResponse{PaymentID: paymentID, Tokens: tokens, AmountXOF: amountXOF}, nil
}

// BoostDish consumes the store wallet then marks the dish boosted. If the
// boost cannot be applied after the debit, the tokens are refunded.
func (s *Service) BoostDish(ctx context.Context, userID, role, storeID, dishID string) (BoostResponse, error) {
	if err := s.requireStoreAccess(ctx, userID, role, storeID); err != nil {
		return BoostResponse{}, err
	}
	ref := map[string]any{"dish_id": dishID}
	if err := s.Consume(ctx, storeID, s.boostCost, ReasonBoostDish, "", "", ref); err != nil {
		return BoostResponse{}, err
	}
	if err := s.booster.SetBoosted(ctx, dishID, true); err != nil {
		// Compensate the debit so the merchant does not lose tokens.
		if creditErr := s.Credit(ctx, storeID, s.boostCost, ReasonBoostDish); creditErr != nil {
			return BoostResponse{}, apperr.Internal(fmt.Errorf("boost failed (%w) and refund failed: %w", err, creditErr))
		}
		return BoostResponse{}, apperr.From(err)
	}
	return BoostResponse{DishID: dishID, Cost: s.boostCost}, nil
}

// BuyOption consumes store tokens for an option from the catalog.
func (s *Service) BuyOption(ctx context.Context, userID, storeID, option string) (BuyOptionResponse, error) {
	if err := s.requireStoreAccess(ctx, userID, "", storeID); err != nil {
		return BuyOptionResponse{}, err
	}
	cost, ok := s.optionCatalog[option]
	if !ok {
		return BuyOptionResponse{}, apperr.NotFound("option_not_found", "unknown option")
	}
	ref := map[string]any{"option": option}
	if err := s.Consume(ctx, storeID, cost, ReasonBuyOption, "", "", ref); err != nil {
		return BuyOptionResponse{}, err
	}
	return BuyOptionResponse{Option: option, Cost: cost}, nil
}

// --- helpers ---

// resolveOwner maps the caller to the wallet owner id: drivers own their
// wallet directly; merchants operate on a store wallet they own.
func (s *Service) resolveOwner(ctx context.Context, userID, role, storeID string) (string, error) {
	switch role {
	case auth.RoleDriver, auth.RoleClient:
		// Le portefeuille suit LE COMPTE, sans point de vente à nommer. Celui
		// d'un livreur porte des jetons, celui d'un client de l'argent — la
		// résolution est la même, seul le contenu diffère.
		return userID, nil
	case auth.RoleMerchant:
		if storeID == "" {
			return "", apperr.Validation("store_id is required for merchants")
		}
		if err := s.requireStoreAccess(ctx, userID, "", storeID); err != nil {
			return "", err
		}
		return storeID, nil
	case auth.RoleAdmin:
		// Même dérogation d'exploitation que requireStoreAccess : l'admin agit
		// POUR un point de vente depuis la console. Le portefeuille visé doit
		// être nommé explicitement — il n'y a pas de portefeuille d'admin.
		if storeID == "" {
			return "", apperr.Validation("store_id is required when acting for a store")
		}
		return storeID, nil
	default:
		return "", apperr.Forbidden("forbidden", "role not allowed")
	}
}

// requireStoreAccess autorise le propriétaire de la boutique, et l'admin qui
// agit POUR lui depuis la console (émulateur marchand) — dérogation
// d'exploitation, tracée par la route qui l'appelle.
func (s *Service) requireStoreAccess(ctx context.Context, userID, role, storeID string) error {
	if role == auth.RoleAdmin {
		return nil
	}
	owns, err := s.stores.OwnsStore(ctx, userID, storeID)
	if err != nil {
		return apperr.From(err)
	}
	if !owns {
		return apperr.Forbidden("forbidden", "you do not own this store")
	}
	return nil
}

func (s *Service) findWallet(ctx context.Context, ownerID string) (*Wallet, error) {
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, apperr.Validation("invalid owner id").WithCause(err)
	}
	wallet, err := s.repo.FindWalletByOwner(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if wallet == nil {
		return nil, apperr.NotFound("wallet_not_found", "wallet not found")
	}
	return wallet, nil
}

func (s *Service) record(ctx context.Context, action, walletID string, after map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "wallet", walletID, nil, after)
}

// maxJustificationLen borne le motif d'un crédit opérateur. Assez long pour
// une phrase utile, assez court pour rester lisible dans un journal d'audit.
const maxJustificationLen = 500

// CreditByOperator grants tokens to a wallet WITHOUT any payment.
//
// C'est de la monnaie créée à la main : geste commercial, compensation d'un
// litige, dédommagement. Le justificatif est donc OBLIGATOIRE — un crédit sans
// motif est indéfendable en revue de comptes — et il part dans le journal
// d'audit, pas dans le grand livre : le grand livre dit combien et pourquoi
// catégoriellement (ReasonOperatorCredit), l'audit dit qui et sur quelle
// justification.
func (s *Service) CreditByOperator(ctx context.Context, actorID, ownerID string, amount int, justification string) (*WalletResponse, error) {
	if amount <= 0 {
		return nil, apperr.Validation("amount must be positive")
	}
	justification = strings.TrimSpace(justification)
	if justification == "" {
		return nil, apperr.Validation("justification is required for an operator credit")
	}
	if len(justification) > maxJustificationLen {
		return nil, apperr.Validation("justification is too long")
	}
	// Le portefeuille doit exister AVANT le crédit : créditer un propriétaire
	// inconnu créerait un solde orphelin que personne ne réclamerait.
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	if err := s.Credit(ctx, ownerID, amount, ReasonOperatorCredit); err != nil {
		return nil, err
	}

	s.record(ctx, "token.operator_credit", wallet.ID.Hex(), map[string]any{
		"owner_id":      ownerID,
		"amount":        amount,
		"justification": justification,
		"actor_id":      actorID,
	})

	// Relecture : le solde renvoyé est celui d'après crédit, pas une addition
	// faite ici — le compte fait foi, pas notre arithmétique.
	updated, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	resp := newWalletResponse(updated, s.tokenPriceXOF)
	return &resp, nil
}

// CanSpendOnCatalogue dit si les dépenses portant sur le catalogue d'une
// verticale sont servables.
//
// ⚠️ Elles demandent DEUX collaborateurs que le socle n'a pas : de quoi
// marquer un plat propulsé, et de quoi vérifier qu'une boutique appartient
// bien à celui qui paie. Sans eux, propulser débiterait des jetons sans rien
// propulser — et sans le second, n'importe quel marchand dépenserait sur le
// point de vente d'un autre.
func (s *Service) CanSpendOnCatalogue() bool {
	return s != nil && s.booster != nil && s.stores != nil
}
