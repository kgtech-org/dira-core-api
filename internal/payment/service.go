package payment

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// DefaultProvider is used when the caller does not name a provider.
const DefaultProvider = "mock"

var (
	errUnknownProvider  = apperr.New("unknown_provider", "unknown payment provider", http.StatusUnprocessableEntity)
	errInvalidSignature = apperr.Unauthorized("invalid_signature", "invalid webhook signature")
)

// repository is the storage contract the service needs (consumer-side).
type repository interface {
	Create(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*Payment, error)
	FindByProviderRef(ctx context.Context, ref string) (*Payment, error)
	SetStatusIfPending(ctx context.Context, id primitive.ObjectID, status string) (bool, error)
}

// auditRecorder is satisfied by *audit.Recorder (injected at wiring time).
type auditRecorder interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Service implements the payment business logic.
// PaymentPreferences rend l'opérateur mobile money habituel d'un client.
// Défini ICI, côté consommateur ; le câblage injecte le service des comptes.
type PaymentPreferences interface {
	PreferredPaymentProvider(ctx context.Context, userID string) string
}

type Service struct {
	repo      repository
	providers map[string]PaymentProvider
	audit     auditRecorder

	// OnOrderPaid is invoked exactly once when an order payment succeeds.
	// Wiring sets it to mark the order paid AND create the delivery
	// (order.MarkPaid + delivery.CreateForOrder). Nil-safe: skipped with a
	// warning when unset.
	// OnRefPaid est invoqué EXACTEMENT UNE FOIS quand le paiement d'un objet
	// d'une verticale aboutit. Le `purpose` dit laquelle prévenir — le socle
	// ne sait ni ce qu'est une commande, ni ce qu'est une course.
	OnRefPaid func(ctx context.Context, purpose, refID, paymentID string) error
	// OnRefPaidDuplicate est invoqué quand un webhook ARRIVE EN DOUBLON sur un
	// paiement déjà abouti.
	//
	// ⚠️ Il existe pour une raison précise, et son absence était un trou. Le
	// rappel de la verticale part désormais en FILE : si la mise en file
	// échoue, le paiement est pourtant déjà marqué « abouti ». Le prestataire
	// réessaie, tombe sur la branche « doublon » — et sans ce point
	// d'accroche, personne ne serait jamais prévenu. La commande resterait
	// payée et jamais confirmée.
	//
	// Nil-safe : sans lui, le doublon reste un simple no-op, ce qu'il était.
	OnRefPaidDuplicate func(ctx context.Context, purpose, refID, paymentID string) error
	// OnTokensPurchased is invoked exactly once when a token purchase
	// succeeds. Wiring sets it to credit the wallet (token.Credit). Nil-safe:
	// skipped with a warning when unset.
	OnTokensPurchased func(ctx context.Context, walletOwnerID string, tokens int) error
	// OnWalletToppedUp crédite le portefeuille d'argent d'un client après
	// confirmation du prestataire.
	OnWalletToppedUp func(ctx context.Context, userID string, amountXOF int, paymentID string) error
	// preferences rend l'opérateur habituel d'un client. Facultatif : sans
	// lui, le défaut s'applique comme avant.
	preferences PaymentPreferences
}

// NewService builds the service with a provider registry keyed by provider
// name (e.g. {"mock": NewMockProvider("")}).
func NewService(repo repository, providers map[string]PaymentProvider, rec auditRecorder) *Service {
	return &Service{repo: repo, providers: providers, audit: rec}
}

// Initiate creates a pending payment and starts the provider flow.
func (s *Service) Initiate(ctx context.Context, userID string, req InitiatePaymentRequest) (*InitiatePaymentResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	refID, err := primitive.ObjectIDFromHex(req.RefID)
	if err != nil {
		return nil, apperr.Validation("invalid ref_id").WithCause(err)
	}

	providerName := req.Provider
	if providerName == "" && s.preferences != nil {
		// L'opérateur habituel du client, quand il en a déclaré un. Une
		// PRÉSÉLECTION, pas un paiement automatique : aucun jeton n'est
		// conservé, et le prestataire demandera confirmation comme toujours.
		//
		// Le repli sur le défaut ne vaut QUE pour cet opérateur-là. Un
		// opérateur EXPLICITEMENT demandé et inconnu reste une erreur : le
		// client a choisi, et le faire payer ailleurs sans le dire serait
		// pire qu'un refus.
		if remembered := s.preferences.PreferredPaymentProvider(ctx, userID); remembered != "" {
			if _, known := s.providers[remembered]; known {
				providerName = remembered
			}
		}
	}
	if providerName == "" {
		providerName = DefaultProvider
	}
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, errUnknownProvider
	}

	var meta map[string]any
	if req.Purpose == PurposeTokenPurchase {
		if req.Tokens <= 0 {
			return nil, apperr.Validation("tokens count is required for token purchases")
		}
		meta = map[string]any{
			MetaWalletOwnerID: refID.Hex(),
			MetaTokens:        req.Tokens,
		}
	}

	p := &Payment{
		ID:       primitive.NewObjectID(),
		UserID:   uid,
		Purpose:  req.Purpose,
		RefID:    refID,
		Provider: providerName,
		Amount:   req.Amount,
		Currency: DefaultCurrency,
		Status:   StatusPending,
		Meta:     meta,
	}

	result, err := provider.Initiate(ctx, InitiateRequest{
		PaymentID: p.ID.Hex(),
		UserID:    userID,
		Purpose:   req.Purpose,
		Amount:    req.Amount,
		Currency:  p.Currency,
		Meta:      meta,
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	p.ProviderRef = result.ProviderRef

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return &InitiatePaymentResponse{
		Payment:    toPaymentResponse(p),
		PaymentURL: result.InstructionsURL,
	}, nil
}

// InitiatePurchase is the provider contract used by the token module's
// POST /wallet/purchase: it starts a token-purchase payment with the default
// provider and returns the payment id.
func (s *Service) InitiatePurchase(ctx context.Context, userID, walletOwnerID string, tokens int, amountXOF int) (string, error) {
	resp, err := s.Initiate(ctx, userID, InitiatePaymentRequest{
		Purpose: PurposeTokenPurchase,
		Amount:  amountXOF,
		RefID:   walletOwnerID,
		Tokens:  tokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Payment.ID, nil
}

// Get returns a payment; only its owner or an admin may read it.
func (s *Service) Get(ctx context.Context, requesterID, requesterRole, paymentID string) (*PaymentResponse, error) {
	id, err := primitive.ObjectIDFromHex(paymentID)
	if err != nil {
		return nil, errPaymentNotFound.WithCause(err)
	}
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if requesterRole != auth.RoleAdmin && p.UserID.Hex() != requesterID {
		return nil, apperr.Forbidden("forbidden", "not the payment owner")
	}
	resp := toPaymentResponse(p)
	return &resp, nil
}

// HandleWebhook verifies and processes a provider webhook. It is strictly
// idempotent: a payment already confirmed is acknowledged without any side
// effect, and the pending->final status transition is the atomic gate that
// guarantees hooks run at most once.
func (s *Service) HandleWebhook(ctx context.Context, providerName string, payload []byte, signature string) error {
	provider, ok := s.providers[providerName]
	if !ok {
		return apperr.NotFound("unknown_provider", "unknown payment provider")
	}

	event, err := provider.VerifyWebhook(payload, signature)
	if err != nil {
		return errInvalidSignature.WithCause(err)
	}

	p, err := s.repo.FindByProviderRef(ctx, event.ProviderRef)
	if err != nil {
		return err
	}
	// Idempotency: already confirmed -> 200.
	//
	// ⚠️ On RÉ-ENFILE le rappel au passage. C'est ce qui rend récupérable un
	// échec de mise en file : le paiement est abouti, mais rien ne dit que la
	// verticale l'a su. Le rappel étant idempotent chez elle, en envoyer un de
	// trop ne coûte rien — en oublier un coûte une commande perdue.
	if p.Status == StatusSucceeded {
		s.replayRefPaid(ctx, p)
		return nil
	}

	switch event.Status {
	case StatusSucceeded:
		return s.confirmSucceeded(ctx, p)
	case StatusFailed:
		return s.confirmFailed(ctx, p)
	default:
		return apperr.Validation("unknown webhook event status")
	}
}

// replayRefPaid remet en file l'annonce d'un paiement déjà abouti.
//
// ⚠️ AU MIEUX, et volontairement silencieux en cas d'échec : on est sur la
// branche « doublon », le prestataire a déjà eu sa réponse la première fois.
// Faire échouer le webhook ici le ferait réessayer indéfiniment pour un
// paiement qui, lui, est bien enregistré.
func (s *Service) replayRefPaid(ctx context.Context, p *Payment) {
	if s.OnRefPaidDuplicate == nil {
		return
	}
	if p.Purpose != PurposeOrder && p.Purpose != PurposeRide {
		return
	}
	if err := s.OnRefPaidDuplicate(ctx, p.Purpose, p.RefID.Hex(), p.ID.Hex()); err != nil {
		slog.WarnContext(ctx, "payment: could not re-queue the vertical callback for a duplicate webhook",
			"payment_id", p.ID.Hex(), "purpose", p.Purpose, "error", err)
	}
}

func (s *Service) confirmSucceeded(ctx context.Context, p *Payment) error {
	transitioned, err := s.repo.SetStatusIfPending(ctx, p.ID, StatusSucceeded)
	if err != nil {
		return err
	}
	if !transitioned {
		// Course perdue avec un webhook concurrent : c'est un doublon, et il
		// ré-enfile pour la même raison que ci-dessus.
		s.replayRefPaid(ctx, p)
		return nil
	}

	s.record(ctx, "payment.succeeded", p, StatusSucceeded)

	switch p.Purpose {
	case PurposeOrder, PurposeRide:
		if s.OnRefPaid == nil {
			slog.WarnContext(ctx, "payment: OnRefPaid hook not configured, skipping",
				"payment_id", p.ID.Hex(), "purpose", p.Purpose, "ref_id", p.RefID.Hex())
			return nil
		}
		// L'identifiant du PAIEMENT part avec : la verticale en a besoin pour
		// sa trace d'audit, et il est le seul moyen de remonter au
		// prestataire depuis une commande ou une course contestée.
		if err := s.OnRefPaid(ctx, p.Purpose, p.RefID.Hex(), p.ID.Hex()); err != nil {
			return apperr.Internal(err)
		}
	case PurposeWalletTopup:
		if s.OnWalletToppedUp == nil {
			slog.WarnContext(ctx, "payment: OnWalletToppedUp hook not configured, skipping", "payment_id", p.ID.Hex())
			return nil
		}
		// Le montant CRÉDITÉ est celui du paiement confirmé, jamais celui
		// qu'annonçait la requête : entre les deux, c'est le prestataire qui
		// fait foi.
		if err := s.OnWalletToppedUp(ctx, p.RefID.Hex(), p.Amount, p.ID.Hex()); err != nil {
			return apperr.Internal(err)
		}
	case PurposeTokenPurchase:
		if s.OnTokensPurchased == nil {
			slog.WarnContext(ctx, "payment: OnTokensPurchased hook not configured, skipping", "payment_id", p.ID.Hex())
			return nil
		}
		ownerID, tokens := tokenPurchaseContext(p)
		if tokens <= 0 {
			return apperr.Internal(errInvalidTokenMeta(p))
		}
		if err := s.OnTokensPurchased(ctx, ownerID, tokens); err != nil {
			return apperr.Internal(err)
		}
	}
	return nil
}

func (s *Service) confirmFailed(ctx context.Context, p *Payment) error {
	transitioned, err := s.repo.SetStatusIfPending(ctx, p.ID, StatusFailed)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}
	// The order (or purchase intent) is left untouched: the payment can be
	// retried with a new initiation.
	s.record(ctx, "payment.failed", p, StatusFailed)
	return nil
}

func (s *Service) record(ctx context.Context, action string, p *Payment, newStatus string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "payment", p.ID.Hex(),
		map[string]any{"status": p.Status},
		map[string]any{"status": newStatus, "purpose": p.Purpose, "amount": p.Amount, "provider_ref": p.ProviderRef},
	)
}

// tokenPurchaseContext extracts the wallet owner and tokens count from the
// payment meta, tolerating the numeric types Mongo decodes into.
func tokenPurchaseContext(p *Payment) (string, int) {
	ownerID := p.RefID.Hex()
	if v, ok := p.Meta[MetaWalletOwnerID].(string); ok && v != "" {
		ownerID = v
	}
	return ownerID, asInt(p.Meta[MetaTokens])
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

type invalidTokenMetaError struct{ paymentID string }

func (e invalidTokenMetaError) Error() string {
	return "payment: token purchase " + e.paymentID + " has no valid tokens count in meta"
}

func errInvalidTokenMeta(p *Payment) error { return invalidTokenMetaError{paymentID: p.ID.Hex()} }

// ConfirmSandboxPayment forces a pending payment to succeed by forging the
// provider webhook it would have received.
//
// ⚠️ CETTE MÉTHODE SIMULE DE L'ARGENT REÇU. Elle existe pour dérouler le
// parcours de recharge de bout en bout hors production — sans elle, un achat
// de jetons reste éternellement en attente sur un environnement de test.
//
// Deux verrous INDÉPENDANTS la protègent :
//   - la route qui l'expose n'est montée qu'en dehors de la production ;
//   - seul un fournisseur capable de signer ses propres webhooks est
//     acceptable. Un vrai prestataire ne l'est pas : sa signature ne peut pas
//     être forgée, et c'est exactement la propriété qu'on veut conserver.
//
// Le chemin emprunté est le VRAI : même vérification de signature, même
// traitement, même idempotence. Rien n'est court-circuité.
func (s *Service) ConfirmSandboxPayment(ctx context.Context, paymentID string) error {
	id, err := primitive.ObjectIDFromHex(paymentID)
	if err != nil {
		return errPaymentNotFound.WithCause(err)
	}
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if p.Status == StatusSucceeded {
		return nil // déjà encaissé : rejouer ne doit rien changer
	}

	provider, ok := s.providers[p.Provider]
	if !ok {
		return apperr.NotFound("unknown_provider", "unknown payment provider")
	}
	signer, ok := provider.(interface{ Sign([]byte) string })
	if !ok {
		return apperr.New("provider_not_simulatable",
			"ce prestataire ne peut pas être simulé : sa signature de webhook n'est pas forgeable",
			http.StatusConflict)
	}

	payload, err := json.Marshal(map[string]string{
		"provider_ref": p.ProviderRef,
		"status":       StatusSucceeded,
	})
	if err != nil {
		return apperr.Internal(err)
	}
	return s.HandleWebhook(ctx, p.Provider, payload, signer.Sign(payload))
}

// SetPreferences branche l'opérateur habituel des clients (câblage).
func (s *Service) SetPreferences(p PaymentPreferences) { s.preferences = p }

// InitiateFor starts a payment ON BEHALF OF a client, at a vertical's request.
//
// ⚠️ Sert au canal WhatsApp : une commande passée hors de l'application n'a pas
// de session, et personne pour appuyer sur « payer ». La verticale déclenche,
// le client reçoit un lien.
//
// Rend l'URL SEULE et non la réponse entière : l'appelant n'a besoin que de
// cela, et lui rendre l'objet complet ferait traverser au réseau des champs
// qu'aucune verticale n'a à connaître.
func (s *Service) InitiateFor(ctx context.Context, clientID, purpose, refID string, amountXOF int) (string, error) {
	resp, err := s.Initiate(ctx, clientID, InitiatePaymentRequest{
		Purpose: purpose, RefID: refID, Amount: amountXOF,
	})
	if err != nil {
		return "", err
	}
	return resp.PaymentURL, nil
}
