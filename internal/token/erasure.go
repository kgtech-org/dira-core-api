package token

// CE QU'UN COMPTE DÉTIENT ENCORE — la question que pose une suppression de
// compte avant de l'accepter.
//
// ⚠️ ON NE DÉTRUIT PAS DE L'ARGENT EN SILENCE. Quelqu'un qui touche
// « supprimer mon compte » avec douze mille francs dans Dira Cash ne fait pas
// un don à la plateforme : il fait une erreur, et il la découvrira trop tard,
// quand aucune écriture ne pourra plus la rattraper. Le socle refuse donc, et
// nomme ce qui bloque, pour que la personne vide son portefeuille ou demande
// un remboursement au support.
//
// ⚠️ ET UNE DETTE BLOQUE AUSSI, pour la raison inverse : si « supprimer mon
// compte » effaçait ce qu'on doit, ce bouton deviendrait la sortie de secours
// de toute commission en espèces impayée.

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// NonZeroBalance dit si ce propriétaire détient encore quelque chose, et quoi.
//
// Le mot rendu est un CODE, pas une phrase : il voyage dans le `meta` d'un
// refus et c'est l'application qui le traduit. Trois cas seulement, parce que
// trois gestes différents les dénouent — vider, dépenser, payer.
//
// ⚠️ LE CRÉDIT OFFERT (`promo_xof`) NE BLOQUE PAS. Ce n'est pas l'argent de la
// personne : elle ne l'a jamais versé, il ne se rembourse pas, et refuser une
// suppression pour un geste commercial qu'on lui a fait serait la retenir avec
// ce qu'on lui a donné.
func (s *Service) NonZeroBalance(ctx context.Context, ownerID string) (string, bool) {
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return "", false
	}
	w, err := s.repo.FindWalletByOwner(ctx, oid)
	if err != nil {
		// ⚠️ UNE BASE QUI NE RÉPOND PAS NE DOIT PAS BLOQUER UNE SUPPRESSION
		// pour toujours — mais elle ne doit pas non plus la laisser passer en
		// silence. Le journal crie, l'appelant décide.
		slog.WarnContext(ctx, "token: balance not read before an erasure", "owner_id", ownerID, "error", err)
		return "", false
	}
	if w == nil {
		return "", false
	}
	switch {
	case w.DebtXOF > 0:
		return "debt", true
	case w.BalanceXOF > 0:
		return "money", true
	case w.Balance > 0:
		return "tokens", true
	}
	return "", false
}
