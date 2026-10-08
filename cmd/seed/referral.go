package main

// ALLUMER LE PARRAINAGE DANS CHAQUE PAYS OUVERT.
//
// Le socle sait parrainer depuis #138, mais la politique naît VIDE : tant que
// personne n'a posé de montants, « inviter un ami » ne donne rien et aucun code
// n'est tiré. C'est le bon défaut pour du code — il ne distribue pas d'argent
// que personne n'a décidé — et c'est un mauvais état pour une plateforme en
// service, où la fonctionnalité existe sans rien faire.
//
// La décision a été prise : le parrainage est actif dans tous les pays ouverts.
// Elle est écrite ICI, dans le provisionnement, et non dans `DefaultReferral` —
// et la différence compte :
//
// ⚠️ UN DÉFAUT DE CODE S'APPLIQUE À TOUT PAYS QUI APPARAÎT, Y COMPRIS CEUX
// QU'ON OUVRIRA DANS SIX MOIS. Rendre `DefaultReferral` généreux ferait
// distribuer de l'argent le jour de l'ouverture d'Abidjan, sans que personne
// n'ait regardé le panier moyen ni le budget d'acquisition de ce marché. Un pas
// de provisionnement, lui, est un GESTE : il se voit dans un journal, il se
// rejoue, et il ne s'applique qu'aux pays qu'on lui nomme.
//
// ⚠️ ET IL N'ÉCRASE JAMAIS UNE DÉCISION DE LA CONSOLE. Un pays dont quelqu'un a
// déjà réglé le parrainage — y compris pour l'ÉTEINDRE — est laissé tel quel. Un
// provisionnement qui rallume ce qu'une direction vient de couper est pire
// qu'inutile : il faudrait couper deux fois, et la seconde ne tiendrait pas non
// plus.

import (
	"context"
	"log/slog"

	icountry "github.com/kgtech-org/dira-core-api/internal/country"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// referralByCurrency : ce qu'un parrainage donne, PAR MONNAIE et non par pays.
//
// ⚠️ PAR MONNAIE, PARCE QUE C'EST LA MONNAIE QUI DONNE L'ÉCHELLE. 1 000 est une
// course courte en franc CFA et un dixième de course en franc guinéen : une
// table par pays aurait répété quatre fois le même nombre et invité à recopier
// le franc CFA sous Conakry — l'erreur qui offre dix fois moins que prévu sans
// que rien ne la signale.
//
// Les montants visent la même chose partout : une course courte offerte au
// filleul, la moitié créditée au parrain.
var referralByCurrency = map[string]icountry.Referral{
	// Franc CFA (UEMOA puis CEMAC) : une course urbaine courte vaut 1 500 à
	// 2 500 F.
	"XOF": {InviteeXOF: 1000, SponsorXOF: 500, MaxSponsored: 10, ValidDays: 365},
	"XAF": {InviteeXOF: 1000, SponsorXOF: 500, MaxSponsored: 10, ValidDays: 365},
	// Franc guinéen : ~15 fois le franc CFA. 15 000 FG est l'équivalent de
	// 1 000 F CFA, pas un geste quinze fois plus généreux.
	"GNF": {InviteeXOF: 15000, SponsorXOF: 7500, MaxSponsored: 10, ValidDays: 365},
}

// seedReferral allume le parrainage des pays ouverts qui n'ont pas de politique.
func seedReferral(ctx context.Context, logger *slog.Logger, repo *icountry.Repository) error {
	installed, err := repo.All(ctx)
	if err != nil {
		return err
	}
	svc := icountry.NewService(repo, "")
	for _, inst := range installed {
		if !inst.Enabled {
			// Un pays fermé n'a personne à parrainer. Le jour où il réouvre,
			// un passage du provisionnement l'allumera.
			continue
		}
		// ⚠️ LA LECTURE DU SERVICE, et non le champ nu : c'est elle qui dit
		// « actif » au sens où la console l'entend (la remise du filleul est
		// non nulle). Tester `inst.Referral != (Referral{})` aurait pris pour
		// « rien de réglé » un pays réglé à zéro exprès.
		cur, err := svc.Referral(ctx, inst.Code)
		if err != nil {
			return err
		}
		if cur.InviteeXOF > 0 || cur.SponsorXOF > 0 || cur.MaxSponsored > 0 {
			logger.Info("seed: referral left alone — already decided",
				"country", inst.Code, "invitee", cur.InviteeXOF, "sponsor", cur.SponsorXOF)
			continue
		}
		cy := currencyOf(inst)
		pol, ok := referralByCurrency[cy]
		if !ok {
			// ⚠️ DIT ET LAISSÉ ÉTEINT. Inventer un montant dans une monnaie
			// qu'on n'a pas chiffrée, c'est se tromper d'un facteur cent une
			// fois sur deux — le cedi et le naira se stockent en centièmes.
			// Le pays reste sans parrainage, et le journal dit quoi faire.
			logger.Warn("seed: NO REFERRAL for this currency — decide it from the console",
				"country", inst.Code, "currency", cy)
			continue
		}
		if err := repo.SetReferral(ctx, inst.Code, pol); err != nil {
			return err
		}
		logger.Info("seed: referral switched on",
			"country", inst.Code, "currency", cy,
			"invitee", pol.InviteeXOF, "sponsor", pol.SponsorXOF,
			"max_sponsored", pol.MaxSponsored, "valid_days", pol.ValidDays)
	}
	return nil
}

// currencyOf rend la monnaie EFFECTIVE d'un pays : celle que l'exploitation a
// réglée, sinon celle du catalogue.
//
// ⚠️ LE RÉGLAGE D'ABORD, et ce n'est pas un détail ici : un pays peut avoir
// changé de monnaie sans qu'on redéploie, et poser un montant en franc CFA dans
// un pays passé au franc guinéen offrirait quinze fois moins que prévu.
func currencyOf(inst icountry.Installation) string {
	if inst.Currency != "" {
		return country.NormalizeCurrency(inst.Currency)
	}
	if info, ok := country.Lookup(inst.Code); ok {
		return country.NormalizeCurrency(info.Currency)
	}
	return ""
}
