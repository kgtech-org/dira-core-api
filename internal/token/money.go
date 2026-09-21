package token

import (
	"context"
	"errors"
	"fmt"
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

// ensureClientWallet ouvre le portefeuille d'un client s'il n'existe pas
// encore, puis le rend.
//
// L'ouverture à l'inscription est « au mieux » et, pendant des mois, a
// échoué pour TOUS les clients (voir `CreateWallet`). Les comptes de cette
// période n'ont pas de portefeuille, et rien ne les distingue d'un compte
// neuf. Plutôt qu'une migration qu'il faudrait rejouer à chaque nouvelle
// cause d'échec, chaque geste d'ARGENT d'un client passe par ici : un
// portefeuille absent est un portefeuille vide, pas une erreur — et une
// recharge confirmée par le prestataire ne doit JAMAIS être perdue faute de
// portefeuille où la poser.
func (s *Service) ensureClientWallet(ctx context.Context, userID string) (*Wallet, error) {
	wallet, err := s.findWallet(ctx, userID)
	if err == nil {
		return wallet, nil
	}
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != "wallet_not_found" {
		return nil, err
	}
	if err := s.CreateClientWallet(ctx, userID); err != nil {
		return nil, err
	}
	return s.findWallet(ctx, userID)
}

// TopUp crédite le portefeuille d'un client, après confirmation du paiement.
//
// Appelé par le module de paiement, jamais par le client : créditer sur la
// seule réponse HTTP d'une initiation reviendrait à offrir l'argent.
func (s *Service) TopUp(ctx context.Context, userID string, amountXOF int, ref map[string]any) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	if _, err := s.ensureClientWallet(ctx, userID); err != nil {
		return err
	}
	// La recharge porte déjà son unicité : le module de paiement n'appelle ceci
	// qu'une fois par paiement abouti, et l'index unique sur `provider_ref`
	// l'empêche d'aboutir deux fois.
	if err := s.moveMoney(ctx, userID, "balance_xof", amountXOF, KindPurchase, ReasonWalletTopup, "", "", "", ref); err != nil {
		return err
	}
	// Une dette (un paiement refusé à la livraison) se rembourse sur la
	// recharge qui suit, d'office. Rejouable sans risque : ce qui est pris
	// est borné par la dette qui reste.
	return s.RepayDebt(ctx, userID)
}

// RepayDebt prend sur le solde en argent ce que le portefeuille doit, au
// plus ce qu'il y a. Rend ce qui a été remboursé.
func (s *Service) RepayDebt(ctx context.Context, ownerID string) error {
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil || wallet.DebtXOF <= 0 {
		return err
	}
	return s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		_, err := s.repayDebt(txCtx, wallet, nil, "", time.Now().UTC())
		return err
	})
}

// CreditPromo offre un crédit promotionnel — geste commercial, compensation,
// campagne. Dépensé AVANT l'argent réel.
func (s *Service) CreditPromo(ctx context.Context, userID string, amountXOF int, ref map[string]any) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	if _, err := s.ensureClientWallet(ctx, userID); err != nil {
		return err
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
	// Un client sans portefeuille n'a « pas de quoi payer », pas « pas de
	// portefeuille » : c'est ce que l'application doit lui dire.
	wallet, err := s.ensureClientWallet(ctx, userID)
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
				Reason:    reasonOfPayment(refKind),
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
	if _, err := s.ensureClientWallet(ctx, userID); err != nil {
		return err
	}
	return s.moveMoney(ctx, userID, "balance_xof", amountXOF, KindPurchase, ReasonRefund, refKind, refID,
		refundKey(refKind, refID), nil)
}

// reasonOfPayment nomme un débit d'après ce qu'il paie : un pourboire n'est
// pas un « paiement » au relevé du passager, même s'il en a la mécanique.
func reasonOfPayment(refKind string) string {
	if refKind == RefTip {
		return ReasonTip
	}
	return ReasonPayment
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
	case RefOrder, RefRide, RefTip, RefRideAdjustment, RefEquipment:
	default:
		return nil, apperr.Validation("reference kind must be order, ride, tip, ride_adjustment or equipment")
	}
	oid, err := primitive.ObjectIDFromHex(refID)
	if err != nil {
		return nil, apperr.Validation("invalid reference id").WithCause(err)
	}
	return &oid, nil
}

// ChargeEquipment takes money OUT of a driver's wallet for an equipment
// contract: an instalment on its due date, a deduction from an earning that
// was just credited, a rental period.
//
// `allowPartial` : prendre ce qu'il y a quand le solde ne couvre pas tout —
// réglage du contrat, jamais un défaut. Rend ce qui a été PRIS (zéro quand
// le solde est vide) ; la clé rend le mouvement rejouable sans double
// débit, comme tout mouvement d'argent.
// errNothingTaken annule la transaction d'un prélèvement qui n'a rien pris,
// pour que sa clé d'idempotence reste disponible.
var errNothingTaken = errors.New("token: nothing taken")

func (s *Service) ChargeEquipment(ctx context.Context, ownerID string, amountXOF int, allowPartial bool, contractID, key string) (int, error) {
	if amountXOF <= 0 {
		return 0, apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	refOID, err := parseRef(RefEquipment, contractID)
	if err != nil {
		return 0, err
	}
	taken := 0
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if key != "" {
			if err := s.repo.ClaimOperation(txCtx, key); err != nil {
				return err
			}
		}
		n, err := s.repo.TakeMoney(txCtx, wallet.ID, amountXOF, allowPartial)
		if err != nil {
			return err
		}
		taken = n
		if n == 0 {
			// Rien pris : la clé n'est PAS consommée — un solde vide ce matin
			// peut être crédité ce soir, et le balayage doit pouvoir réessayer.
			return errNothingTaken
		}
		return s.repo.InsertTransaction(txCtx, &Transaction{
			WalletID: wallet.ID, Kind: KindConsume, Reason: ReasonEquipment, Amount: n, Unit: UnitXOF,
			RefID: refOID, RefKind: RefEquipment, CreatedAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, errNothingTaken) {
		return 0, nil
	}
	if errors.Is(err, errOperationApplied) {
		// Le balayage repasse toutes les minutes : une clé déjà servie est
		// la norme, pas un événement — en debug seulement.
		slog.DebugContext(ctx, "token: equipment charge replayed, not applied twice", "key", key, "owner_id", ownerID)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if taken > 0 {
		s.record(ctx, "token.equipment", wallet.ID.Hex(), map[string]any{"amount_xof": taken, "contract_id": contractID, "owner_id": ownerID})
	}
	return taken, nil
}

// RefundEquipment gives money BACK to a driver's wallet — a deposit returned,
// an over-collection corrected.
func (s *Service) RefundEquipment(ctx context.Context, ownerID string, amountXOF int, contractID, key string) error {
	if amountXOF <= 0 {
		return apperr.Validation("amount must be positive")
	}
	return s.moveMoney(ctx, ownerID, "balance_xof", amountXOF, KindPurchase, ReasonEquipment, RefEquipment, contractID, key, nil)
}

// --- la COMMISSION et la DETTE ---
//
// En mode `commission`, la plateforme se paie sur ce qu'elle verse : un gain
// arrive, sa part est retenue dans le même mouvement. Quand l'argent ne passe
// pas par elle (une course en espèces), la commission est prise sur le solde
// s'il y en a, et le reste devient une DETTE, remboursée d'office sur le
// prochain crédit.

// CreditEarningsNet crédite un gain et retient la commission dans la même
// transaction — puis rembourse la dette éventuelle sur ce qui reste.
// Idempotent par (bénéficiaire, référence, motif). Rend la commission
// retenue et la dette remboursée.
func (s *Service) CreditEarningsNet(ctx context.Context, ownerID string, amountXOF, commissionXOF int, reason, refKind, refID string, ref map[string]any) (retained, repaid int, err error) {
	if amountXOF <= 0 {
		return 0, 0, apperr.Validation("amount must be positive")
	}
	if commissionXOF < 0 || commissionXOF > amountXOF {
		return 0, 0, apperr.Validation("commission must be between 0 and the amount")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return 0, 0, err
	}
	refOID, err := parseRef(refKind, refID)
	if err != nil {
		return 0, 0, err
	}
	now := time.Now().UTC()
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.ClaimOperation(txCtx, earningsKey(ownerID, refKind, refID, reason)); err != nil {
			return err
		}
		if err := s.repo.CreditField(txCtx, wallet.ID, "balance_xof", amountXOF); err != nil {
			return apperr.Internal(err)
		}
		if err := s.repo.InsertTransaction(txCtx, &Transaction{
			WalletID: wallet.ID, Kind: KindPurchase, Reason: reason, Amount: amountXOF, Unit: UnitXOF,
			RefID: refOID, RefKind: refKind, Ref: ref, CreatedAt: now,
		}); err != nil {
			return apperr.Internal(err)
		}
		if commissionXOF > 0 {
			taken, err := s.repo.TakeMoney(txCtx, wallet.ID, commissionXOF, false)
			if err != nil {
				return apperr.Internal(err)
			}
			if taken != commissionXOF {
				return apperr.Internal(fmt.Errorf("token: commission not retained on a fresh credit"))
			}
			retained = taken
			if err := s.repo.InsertTransaction(txCtx, &Transaction{
				WalletID: wallet.ID, Kind: KindConsume, Reason: ReasonCommission, Amount: taken, Unit: UnitXOF,
				RefID: refOID, RefKind: refKind, Ref: map[string]any{"on": reason}, CreatedAt: now,
			}); err != nil {
				return apperr.Internal(err)
			}
		}
		repaid, err = s.repayDebt(txCtx, wallet, refOID, refKind, now)
		return err
	})
	if errors.Is(err, errOperationApplied) {
		slog.InfoContext(ctx, "token: earnings replayed, not applied twice", "owner_id", ownerID, "ref_kind", refKind, "ref_id", refID)
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	s.record(ctx, "token.money", wallet.ID.Hex(), map[string]any{
		"amount_xof": amountXOF, "commission_xof": retained, "debt_repaid_xof": repaid, "reason": reason, "owner_id": ownerID,
	})
	return retained, repaid, nil
}

// repayDebt prend sur le solde en argent ce que le portefeuille doit, au plus
// ce qu'il y a — dans la transaction de l'appelant.
func (s *Service) repayDebt(txCtx context.Context, wallet *Wallet, refOID *primitive.ObjectID, refKind string, now time.Time) (int, error) {
	if wallet.DebtXOF <= 0 {
		return 0, nil
	}
	taken, err := s.repo.TakeMoney(txCtx, wallet.ID, wallet.DebtXOF, true)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	if taken == 0 {
		return 0, nil
	}
	if err := s.repo.AdjustDebt(txCtx, wallet.ID, -taken); err != nil {
		return 0, apperr.Internal(err)
	}
	if err := s.repo.InsertTransaction(txCtx, &Transaction{
		WalletID: wallet.ID, Kind: KindConsume, Reason: ReasonDebtRepaid, Amount: taken, Unit: UnitXOF,
		RefID: refOID, RefKind: refKind, Ref: map[string]any{"debt_delta": -taken}, CreatedAt: now,
	}); err != nil {
		return 0, apperr.Internal(err)
	}
	wallet.DebtXOF -= taken
	return taken, nil
}

// Owe fait payer une somme que le solde n'a pas forcément : ce qu'il y a
// est pris tout de suite (`reason`), le reste devient une DETTE
// (`dueReason`). Idempotent par (motif, référence). Rend ce qui a été pris
// et ce qui est devenu dette.
//
// Un mouvement de dette porte `ref.debt_delta` (ce qu'il ajoute ou retire à
// la dette) et `ref.no_balance: true` quand il ne touche PAS le solde :
// c'est ce que le balayage d'intégrité lit pour recalculer les deux.
func (s *Service) Owe(ctx context.Context, ownerID string, amountXOF int, reason, dueReason, refKind, refID string) (paid, owed int, err error) {
	if amountXOF <= 0 {
		return 0, 0, apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return 0, 0, err
	}
	refOID, err := parseRef(refKind, refID)
	if err != nil {
		return 0, 0, err
	}
	if refOID == nil {
		return 0, 0, apperr.Validation("an amount owed needs a reference")
	}
	now := time.Now().UTC()
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.ClaimOperation(txCtx, "owe:"+reason+":"+refKind+":"+refID); err != nil {
			return err
		}
		taken, err := s.repo.TakeMoney(txCtx, wallet.ID, amountXOF, true)
		if err != nil {
			return apperr.Internal(err)
		}
		paid = taken
		if taken > 0 {
			if err := s.repo.InsertTransaction(txCtx, &Transaction{
				WalletID: wallet.ID, Kind: KindConsume, Reason: reason, Amount: taken, Unit: UnitXOF,
				RefID: refOID, RefKind: refKind, CreatedAt: now,
			}); err != nil {
				return apperr.Internal(err)
			}
		}
		if rest := amountXOF - taken; rest > 0 {
			owed = rest
			if err := s.repo.AdjustDebt(txCtx, wallet.ID, rest); err != nil {
				return apperr.Internal(err)
			}
			if err := s.repo.InsertTransaction(txCtx, &Transaction{
				WalletID: wallet.ID, Kind: KindConsume, Reason: dueReason, Amount: rest, Unit: UnitXOF,
				RefID: refOID, RefKind: refKind, Ref: map[string]any{"debt_delta": rest, "no_balance": true}, CreatedAt: now,
			}); err != nil {
				return apperr.Internal(err)
			}
		}
		return nil
	})
	if errors.Is(err, errOperationApplied) {
		slog.InfoContext(ctx, "token: amount owed replayed, not applied twice", "owner_id", ownerID, "ref_kind", refKind, "ref_id", refID)
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	s.record(ctx, "token.owe", wallet.ID.Hex(), map[string]any{
		"amount_xof": amountXOF, "paid_xof": paid, "owed_xof": owed, "reason": reason, "owner_id": ownerID,
	})
	return paid, owed, nil
}

// SettleDebt enregistre un remboursement de dette réglé HORS solde — à
// l'agence, en espèces ou mobile money. Réservé à l'exploitation.
func (s *Service) SettleDebt(ctx context.Context, actorID, ownerID string, amountXOF int, note string) (*WalletResponse, error) {
	if amountXOF <= 0 {
		return nil, apperr.Validation("amount must be positive")
	}
	wallet, err := s.findWallet(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if amountXOF > wallet.DebtXOF {
		return nil, apperr.Validation("amount above the debt").WithMeta(map[string]any{"debt_xof": wallet.DebtXOF})
	}
	now := time.Now().UTC()
	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.AdjustDebt(txCtx, wallet.ID, -amountXOF); err != nil {
			return apperr.Internal(err)
		}
		return s.repo.InsertTransaction(txCtx, &Transaction{
			WalletID: wallet.ID, Kind: KindConsume, Reason: ReasonDebtRepaid, Amount: amountXOF, Unit: UnitXOF,
			Ref: map[string]any{"debt_delta": -amountXOF, "no_balance": true, "settled": "agency", "note": note, "by": actorID}, CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "token.debt.settle", wallet.ID.Hex(), map[string]any{"amount_xof": amountXOF, "note": note, "owner_id": ownerID, "by": actorID})
	resp, err := s.WalletOf(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
