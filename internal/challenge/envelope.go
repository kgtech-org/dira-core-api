package challenge

// PRENDRE UNE PLACE DANS L'ENVELOPPE — le moment où un objectif coûte de
// l'argent.
//
// ⚠️ C'EST LE SEUL ENDROIT DE CE PAQUET OÙ UNE COURSE PEUT SE PERDRE, et donc
// le seul qui mérite d'être atomique. Deux chauffeurs qui franchissent la cible
// à la même seconde sur une enveloppe à une place : sans écriture conditionnelle,
// les deux la prennent, et l'entreprise paie deux fois ce qu'elle avait budgété
// une. Vérifier puis écrire en deux temps ne suffit pas — c'est précisément la
// fenêtre que `FindOneAndUpdate` referme.

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// claimFilter construit le filtre qui garde l'enveloppe.
//
// ⚠️ EXTRAITE POUR ÊTRE ÉPROUVÉE EN DONNÉES, et pas par goût de la pureté. Un
// filtre MongoDB est exactement le genre de code qui a l'air juste, qui répond
// sans erreur, et qui ne garde pas ce qu'il devait garder — on l'a déjà payé sur
// le verrou des rapports, où une doublure de test disait vrai pendant que la
// base disait le contraire. Ici, la ligne qui compte est celle qui empêche de
// dépasser le budget : si elle tombe, l'entreprise paie sans plafond et rien ne
// le signale avant le relevé bancaire.
func claimFilter(id primitive.ObjectID, l promo.Limits, rewardXOF int, now time.Time) bson.M {
	f := bson.M{
		"_id":    id,
		"status": StatusLive,
		// ⚠️ LA FENÊTRE EST DANS LE FILTRE. Un objectif dont la fenêtre est
		// passée ne paie plus, même si une verticale rejoue un appel en retard
		// — et une verticale rejoue, c'est tout l'intérêt de sa file hors ligne.
		"window.from": bson.M{"$lte": now},
		"window.to":   bson.M{"$gt": now},
	}
	var and []bson.M
	if l.BudgetXOF > 0 {
		// Engagé + ce bonus-ci ne doit pas dépasser l'enveloppe.
		and = append(and, bson.M{"$expr": bson.M{"$lte": bson.A{
			bson.M{"$add": bson.A{
				bson.M{"$ifNull": bson.A{"$counters.amount_reserved_xof", 0}},
				bson.M{"$ifNull": bson.A{"$counters.amount_spent_xof", 0}},
				rewardXOF,
			}},
			l.BudgetXOF,
		}}})
	}
	if l.MaxUses > 0 {
		and = append(and, bson.M{"$expr": bson.M{"$lt": bson.A{
			bson.M{"$add": bson.A{
				bson.M{"$ifNull": bson.A{"$counters.uses_reserved", 0}},
				bson.M{"$ifNull": bson.A{"$counters.uses_spent", 0}},
			}},
			l.MaxUses,
		}}})
	}
	if len(and) > 0 {
		f["$and"] = and
	}
	return f
}

// claimUpdate engage une place : un usage et le montant du bonus.
//
// ⚠️ `reserved` ET NON `spent`, parce que l'argent n'est pas encore sorti. Le
// versement peut échouer — portefeuille illisible, verticale muette —, et
// compter tout de suite comme dépensé aurait fermé l'enveloppe sur de l'argent
// qui n'a jamais quitté le compte. `Settle` déplace la ligne quand le versement
// aboutit ; c'est la même mécanique en deux temps que les promotions, et pour
// la même raison.
func claimUpdate(rewardXOF int, now time.Time) bson.M {
	return bson.M{
		"$inc": bson.M{
			"counters.uses_reserved":       1,
			"counters.amount_reserved_xof": rewardXOF,
		},
		"$set": bson.M{"updated_at": now},
	}
}

// settleUpdate déplace une place de « promis » à « dépensé ».
func settleUpdate(rewardXOF int, now time.Time) bson.M {
	return bson.M{
		"$inc": bson.M{
			"counters.uses_reserved":       -1,
			"counters.amount_reserved_xof": -rewardXOF,
			"counters.uses_spent":          1,
			"counters.amount_spent_xof":    rewardXOF,
		},
		"$set": bson.M{"updated_at": now},
	}
}

// releaseUpdate rend une place quand le versement a définitivement échoué.
//
// ⚠️ ELLE EXISTE POUR QUE L'ENVELOPPE NE SE BLOQUE PAS SUR UN ÉCHEC. Une place
// promise et jamais versée resterait engagée pour toujours : au bout de
// quelques pannes, l'objectif n'accepterait plus personne alors que l'argent est
// intact. ⚠️ `uses_released` est gardé pour être LU — il ne compte dans aucune
// limite, et c'est ce qui permet de voir qu'on a eu des échecs.
func releaseUpdate(rewardXOF int, now time.Time) bson.M {
	return bson.M{
		"$inc": bson.M{
			"counters.uses_reserved":       -1,
			"counters.amount_reserved_xof": -rewardXOF,
			"counters.uses_released":       1,
		},
		"$set": bson.M{"updated_at": now},
	}
}
