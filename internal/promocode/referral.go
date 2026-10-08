package promocode

// LE PARRAINAGE — le code qu'un client donne à ses proches.
//
// ⚠️ C'EST UN CODE PROMO COMME UN AUTRE, et c'est tout l'intérêt : même
// enveloppe, mêmes limites, même registre, même suivi. Un second mécanisme
// « parrainage » aurait dupliqué les compteurs, les refus et les écrans — pour
// la seule différence qui compte, qui tient en deux lignes : il est tiré À LA
// DEMANDE pour chaque client, et il récompense DEUX personnes.
//
// ⚠️ LE PARRAIN EST PAYÉ QUAND LA REMISE EST VRAIMENT CONSOMMÉE, jamais à la
// saisie. Récompenser à la saisie aurait payé des parrainages que personne n'a
// honorés : il suffit de commander puis d'annuler. C'est pour cela que le
// crédit du parrain vit dans `Settle` et pas dans `Redeem`.

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// ReferralPolicy est ce qu'un parrainage donne, de chaque côté.
//
// ⚠️ DEUX MONTANTS ET NON UN, parce que ce ne sont pas deux fois la même
// chose : la remise du FILLEUL est une réduction sur ce qu'il paie (il ne
// connaît pas encore Dira, il faut qu'il essaie), et le crédit du PARRAIN est
// de l'argent sur son solde (il connaît Dira, il faut qu'il revienne). Les
// confondre aurait forcé à offrir la même somme aux deux, alors que leur coût
// et leur effet n'ont rien à voir.
type ReferralPolicy struct {
	// InviteeXOF : la remise du filleul, sur sa première opération.
	InviteeXOF int
	// SponsorXOF : le crédit du parrain, versé quand la remise est consommée.
	SponsorXOF int
	// MaxSponsored : combien de filleuls un parrain peut récolter. Zéro =
	// sans limite, et c'est rarement ce qu'on veut.
	MaxSponsored int
	// ValidDays : la durée de vie du code d'un parrain.
	ValidDays int
}

// DefaultReferral : ce qu'un parrainage donne quand le pays n'a rien réglé.
//
// ⚠️ TOUT À ZÉRO, DONC AUCUN PARRAINAGE, et c'est volontaire. Un parrainage
// actif par défaut distribuerait de l'argent dans cinq pays le jour du
// déploiement, sans qu'aucune direction ne l'ait décidé ni budgété.
var DefaultReferral = ReferralPolicy{ValidDays: 365}

// ReferralReader lit la politique de parrainage d'un pays.
//
// Déclarée côté consommateur. FACULTATIVE : sans elle, le parrainage est
// éteint — et le journal le dit, parce qu'un bouton « inviter un ami » qui ne
// donne rien est pire que pas de bouton.
type ReferralReader interface {
	ReferralOf(ctx context.Context, countryCode string) ReferralPolicy
}

// SetReferral branche la politique de parrainage (câblage).
func (s *Service) SetReferral(r ReferralReader) { s.referral = r }

// Credits crédite le solde de quelqu'un — le parrain, quand son filleul a
// vraiment commandé.
//
// Déclarée côté consommateur : c'est le portefeuille du socle.
type Credits interface {
	PromoCredit(ctx context.Context, userID string, amountXOF int, reason string) error
}

// SetCredits branche le portefeuille (câblage).
func (s *Service) SetCredits(c Credits) { s.credits = c }

// MyReferral rend le code de parrainage d'un client, en le TIRANT au besoin.
//
// ⚠️ TIRÉ À LA DEMANDE, et pas à l'inscription. Tirer un code pour chaque
// compte créé aurait rempli la base de millions de codes dont personne ne
// parlera jamais — et aurait fait de chaque inscription une écriture de plus,
// sur le chemin le plus sensible de la plateforme.
func (s *Service) MyReferral(ctx context.Context, userID string) (*Code, ReferralPolicy, error) {
	pol := s.referralPolicy(ctx)
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, pol, apperr.Validation("invalid user id")
	}
	existing, err := s.repo.List(ctx, KindReferral, &oid, 1)
	if err != nil {
		return nil, pol, apperr.Internal(err)
	}
	if len(existing) > 0 {
		return &existing[0], pol, nil
	}
	// ⚠️ PAS DE PARRAINAGE RÉGLÉ = PAS DE CODE. On ne tire pas un code qui ne
	// donnerait rien : la personne le partagerait, et ses proches liraient
	// « code invalide » sur un code qu'elle leur a donné de bonne foi.
	if pol.InviteeXOF <= 0 {
		return nil, pol, nil
	}
	c := &Code{
		Kind: KindReferral, OwnerID: &oid, Country: country.FromContext(ctx),
		Label:        "Parrainage",
		DiscountKind: KindAmount, Value: pol.InviteeXOF,
		Limits: promo.Limits{
			MaxUses: pol.MaxSponsored,
			// ⚠️ UNE FOIS PAR PERSONNE, TOUJOURS. Un code de parrainage sans
			// cette limite se partage sur les réseaux en une soirée, et ce
			// n'est plus un parrainage : c'est une campagne que personne n'a
			// budgétée.
			MaxUsesPerUser: 1,
		},
		StartsAt: time.Now().UTC(),
		EndsAt:   time.Now().UTC().AddDate(0, 0, max(pol.ValidDays, 1)),
		Active:   true,
	}
	if err := s.draw(ctx, c, ""); err != nil {
		return nil, pol, err
	}
	slog.InfoContext(ctx, "promocode: referral code drawn", "user_id", userID, "code", c.Code)
	return c, pol, nil
}

// rewardSponsor crédite le parrain, une fois la remise VRAIMENT consommée.
//
// ⚠️ AU MIEUX, ET JOURNALISÉ FORT. Une course livrée ne doit pas échouer parce
// qu'un crédit de parrainage n'a pas pu s'écrire ; mais un parrain non payé est
// une promesse rompue, et il le dira — donc un ERROR, pas un WARN.
func (s *Service) rewardSponsor(ctx context.Context, use promo.Use) {
	code, err := s.repo.ByCode(ctx, use.PromoID)
	if err != nil || code == nil || code.Kind != KindReferral || code.OwnerID == nil {
		return
	}
	pol := s.referralPolicy(ctx)
	if pol.SponsorXOF <= 0 {
		return
	}
	if s.credits == nil {
		slog.ErrorContext(ctx, "promocode: SPONSOR NOT PAID — no wallet wired",
			"code", code.Code, "sponsor_id", code.OwnerID.Hex(), "amount_xof", pol.SponsorXOF)
		return
	}
	if err := s.credits.PromoCredit(ctx, code.OwnerID.Hex(), pol.SponsorXOF, "referral"); err != nil {
		slog.ErrorContext(ctx, "promocode: SPONSOR NOT PAID — credit failed",
			"code", code.Code, "sponsor_id", code.OwnerID.Hex(),
			"amount_xof", pol.SponsorXOF, "error", err)
		return
	}
	slog.InfoContext(ctx, "promocode: sponsor rewarded",
		"code", code.Code, "sponsor_id", code.OwnerID.Hex(), "amount_xof", pol.SponsorXOF)
}

func (s *Service) referralPolicy(ctx context.Context) ReferralPolicy {
	if s.referral == nil {
		return DefaultReferral
	}
	pol := s.referral.ReferralOf(ctx, country.FromContext(ctx))
	if pol.ValidDays <= 0 {
		pol.ValidDays = DefaultReferral.ValidDays
	}
	return pol
}
