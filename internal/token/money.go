package token

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Le grand livre porte deux unités. Les mouvements d'ARGENT passent tous par
// ici : un solde qui bougerait sans écrire son mouvement rendrait tout écart
// inexplicable, et c'est exactement ce qu'un support doit pouvoir expliquer.

// CreditEarnings pays money INTO a wallet — le produit d'une vente pour un
// marchand, une commission pour un livreur, la part d'une course pour un
// chauffeur.
func (s *Service) CreditEarnings(ctx context.Context, ownerID string, amountXOF int, reason, refKind, refID string, ref map[string]any) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	return s.moveMoney(ctx, ownerID, "balance_xof", amountXOF, KindPurchase, reason, refKind, refID,
		earningsKey(ownerID, refKind, refID, reason), ref)
}

// moveMoney applies one money movement and writes its ledger entry, together.
//
// `key` vide = AUCUNE garde. Réservé aux mouvements qui n'ont pas de clé
// naturelle — un geste commercial, une recharge confirmée par un prestataire
// qui porte déjà sa propre unicité. Tout mouvement rattaché à une commande en
// a une, et doit la passer.
func (s *Service) moveMoney(ctx context.Context, ownerID, field string, amount int, kind, reason, refKind, refID, key string, ref map[string]any) error {
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return err
	}
	refOID, err := parseRef(refKind, refID)
	if err != nil {
		return err
	}
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if key != "" {
			if err := s.repo.ClaimOperation(txCtx, key); err != nil {
				return err
			}
		}
		if err := s.repo.CreditField(txCtx, wallet.ID, field, amount); err != nil {
			return apperr.Internal(err)
		}
		tx := &Transaction{
			WalletID:  wallet.ID,
			Kind:      kind,
			Reason:    reason,
			Amount:    amount,
			Unit:      UnitXOF,
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
		slog.InfoContext(ctx, "token: money movement replayed, not applied twice",
			"key", key, "owner_id", ownerID, "reason", reason)
		return nil
	}
	if err != nil {
		return err
	}
	s.record(ctx, "token.money", wallet.ID.Hex(), map[string]any{
		"amount_xof": amount, "field": field, "reason": reason, "owner_id": ownerID,
	})
	return nil
}

// ErrInsufficientFunds dit qu'un portefeuille n'a pas de quoi payer.
var ErrInsufficientFunds = apperr.New("insufficient_funds",
	"this wallet does not hold enough to pay for the order", 402)

// CreateClientWallet ouvre le portefeuille d'argent d'un client. Idempotent.
func (s *Service) CreateClientWallet(ctx context.Context, userID string) error {
	return s.CreateWallet(ctx, userID, WalletTypeClient)
}

// TopUp crédite le portefeuille d'un client, après confirmation du paiement.
//
// Appelé par le module de paiement, jamais par le client : créditer sur la
// seule réponse HTTP d'une initiation reviendrait à offrir l'argent.
func (s *Service) TopUp(ctx context.Context, userID string, amountXOF int, ref map[string]any) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	// La recharge porte déjà son unicité : le module de paiement n'appelle ceci
	// qu'une fois par paiement abouti, et l'index unique sur `provider_ref`
	// l'empêche d'aboutir deux fois.
	return s.moveMoney(ctx, userID, "balance_xof", amountXOF, KindPurchase, ReasonWalletTopup, "", "", "", ref)
}

// CreditPromo offre un crédit promotionnel — geste commercial, compensation,
// campagne. Dépensé AVANT l'argent réel.
func (s *Service) CreditPromo(ctx context.Context, userID string, amountXOF int, ref map[string]any) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	// Un geste commercial n'a pas de clé naturelle : il n'est rattaché à rien,
	// et deux crédits identiques peuvent être deux gestes voulus.
	return s.moveMoney(ctx, userID, "promo_xof", amountXOF, KindPurchase, ReasonPromoCredit, "", "", "", ref)
}

// PayOrder débite le portefeuille d'un client du montant d'une commande.
//
// Le promotionnel d'abord, puis l'argent réel. DEUX écritures au grand livre
// quand les deux postes servent : ce que la plateforme a offert ne doit jamais
// se confondre avec ce que le client a payé — le chiffre d'affaires en dépend.
func (s *Service) Pay(ctx context.Context, userID string, amountXOF int, refKind, refID string) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, userID)
	if err != nil {
		return err
	}
	oid, err := parseRef(refKind, refID)
	if err != nil {
		return err
	}
	if oid == nil {
		// Un paiement SANS référence ne pourrait pas être rendu idempotent, et
		// un débit rejouable est un client débité deux fois.
		return apperr.Validation("a payment needs a reference")
	}
	var fromPromo, fromCash int
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		// ⚠️ RÉSERVER AVANT DE DÉBITER, dans la même transaction. Une commande
		// se paie une fois, et son identifiant est la clé : un réessai après
		// une réponse perdue retombe ici et ne débite pas une seconde fois.
		if err := s.repo.ClaimOperation(txCtx, payKey(refKind, refID)); err != nil {
			return err
		}
		fromPromo, fromCash, err = s.repo.SpendMoney(txCtx, wallet.ID, amountXOF)
		if err != nil {
			return err
		}
		for _, mv := range []struct {
			amount int
			ref    map[string]any
		}{
			{fromPromo, map[string]any{"source": "promo"}},
			{fromCash, map[string]any{"source": "cash"}},
		} {
			if mv.amount == 0 {
				continue
			}
			if err := s.repo.InsertTransaction(txCtx, &Transaction{
				WalletID:  wallet.ID,
				Kind:      KindConsume,
				Reason:    ReasonPayment,
				Amount:    mv.amount,
				Unit:      UnitXOF,
				RefID:     oid,
				RefKind:   refKind,
				Ref:       mv.ref,
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				return apperr.Internal(err)
			}
		}
		return nil
	})
	if errors.Is(err, errOperationApplied) {
		// DÉJÀ PAYÉE : un succès, pas une erreur. L'appelant voulait que
		// l'argent bouge ; il a bougé. Lui rendre une erreur le pousserait à
		// annuler une commande parfaitement payée.
		slog.InfoContext(ctx, "token: payment replayed, not applied twice",
			"ref_kind", refKind, "ref_id", refID, "user_id", userID)
		return nil
	}
	if err != nil {
		return err
	}
	s.record(ctx, "token.payment", wallet.ID.Hex(), map[string]any{
		"ref_kind": refKind, "ref_id": refID,
		"from_promo": fromPromo, "from_cash": fromCash,
	})
	return nil
}

// RefundOrder rend au portefeuille ce qu'une commande annulée avait pris.
//
// ⚠️ Rendu en ARGENT, pas en promotionnel, même si la commande avait été
// payée avec un crédit offert. Distinguer coûterait de retrouver la
// répartition d'origine ; rendre en argent est le choix qui ne lèse jamais le
// client — au pire il garde une somme dépensable qu'on lui avait offerte.
func (s *Service) Refund(ctx context.Context, userID string, amountXOF int, refKind, refID string) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	return s.moveMoney(ctx, userID, "balance_xof", amountXOF, KindPurchase, ReasonRefund, refKind, refID,
		refundKey(refKind, refID), nil)
}

// parseRef valide le couple (genre, identifiant) d'une référence.
//
// Les deux vont ENSEMBLE : un identifiant sans genre ne dit pas ce qu'il
// désigne, et un genre sans identifiant ne désigne rien. Les accepter
// séparément laisserait au grand livre des lignes qu'on ne saurait plus
// rattacher.
func parseRef(refKind, refID string) (*primitive.ObjectID, error) {
	if refKind == "" && refID == "" {
		return nil, nil
	}
	if refKind == "" || refID == "" {
		return nil, apperr.Validation("a reference needs both a kind and an id")
	}
	switch refKind {
	case RefOrder, RefRide:
	default:
		return nil, apperr.Validation("reference kind must be order or ride")
	}
	oid, err := primitive.ObjectIDFromHex(refID)
	if err != nil {
		return nil, apperr.Validation("invalid reference id").WithCause(err)
	}
	return &oid, nil
}
