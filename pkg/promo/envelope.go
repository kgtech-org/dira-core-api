// Package promo porte L'ENVELOPPE d'une promotion : ce que l'entreprise
// s'engage à dépenser, ce qui en a déjà été consommé, et quand l'offre doit
// s'arrêter.
//
// ⚠️ IL EST AU SOCLE PARCE QUE LE PROBLÈME EST LE MÊME DES DEUX CÔTÉS. Une
// remise sur un plat et une remise sur une course ne portent pas sur le même
// objet, mais elles coûtent de l'argent à la même entreprise, et cet argent
// se compte de la même façon : une enveloppe, un nombre d'usages, une limite
// par personne, et un registre de ce qui a été engagé. Deux implémentations
// auraient donné deux façons de compter — et c'est exactement le genre
// d'écart qu'on ne découvre qu'en additionnant deux rapports qui ne tombent
// pas juste.
//
// Ce paquet ne connaît ni les plats ni les courses : il compte des francs et
// des usages.
package promo

// Limits est CE QUE L'ENTREPRISE S'ENGAGE À DÉPENSER, et jusqu'où.
//
// ⚠️ ZÉRO VEUT DIRE « PAS DE LIMITE », et c'est un choix assumé qu'il faut
// voir dans la console. L'inverse — zéro veut dire « rien » — aurait éteint
// toute promotion créée par une console d'une version précédente, en
// silence, le jour du déploiement.
type Limits struct {
	// BudgetXOF est l'enveloppe TOTALE de l'opération, en francs.
	BudgetXOF int `bson:"budget_xof,omitempty" json:"budget_xof,omitempty"`
	// MaxUses est le nombre total d'utilisations.
	MaxUses int `bson:"max_uses,omitempty" json:"max_uses,omitempty"`
	// MaxUsesPerUser borne ce qu'UNE personne peut en tirer.
	//
	// ⚠️ C'est la limite qui protège de l'abus, pas le budget. Sans elle,
	// une seule personne peut consommer l'enveloppe entière — et elle le
	// fera : une remise sans limite par personne se partage sur les réseaux
	// en quelques heures.
	MaxUsesPerUser int `bson:"max_uses_per_user,omitempty" json:"max_uses_per_user,omitempty"`
}

// Counters est ce qui a été CONSOMMÉ, en deux temps.
//
// ⚠️ DEUX TEMPS, PAS UN. Une remise accordée n'est pas encore une remise
// payée : la course peut être annulée, la commande refusée. Compter tout de
// suite comme dépensé fermerait l'offre sur de l'argent qui n'est jamais
// sorti ; ne compter qu'à la fin laisserait mille personnes consommer une
// enveloppe de dix pendant que les courses roulent.
//
// Les deux comptent donc pour les LIMITES, et seul `Spent` compte pour la
// dépense RÉELLE — celle qu'on met dans un rapport.
type Counters struct {
	// Reserved : accordé, pas encore abouti.
	UsesReserved   int `bson:"uses_reserved,omitempty" json:"uses_reserved"`
	AmountReserved int `bson:"amount_reserved_xof,omitempty" json:"amount_reserved_xof"`
	// Spent : abouti, l'argent est sorti.
	UsesSpent   int `bson:"uses_spent,omitempty" json:"uses_spent"`
	AmountSpent int `bson:"amount_spent_xof,omitempty" json:"amount_spent_xof"`
	// Released : annulé, l'argent est revenu dans l'enveloppe. Gardé pour
	// être LU, pas pour être soustrait : il ne compte dans aucune limite.
	UsesReleased int `bson:"uses_released,omitempty" json:"uses_released"`
}

// Uses est le nombre d'utilisations qui comptent pour les limites.
func (c Counters) Uses() int { return c.UsesReserved + c.UsesSpent }

// Committed est ce qui est ENGAGÉ : dépensé, plus promis.
func (c Counters) Committed() int { return c.AmountReserved + c.AmountSpent }

// Remaining est ce qu'il reste dans l'enveloppe. Sans budget, -1 : « pas de
// plafond » n'est pas « zéro restant », et les confondre aurait éteint les
// promotions sans budget.
func (l Limits) Remaining(c Counters) int {
	if l.BudgetXOF <= 0 {
		return -1
	}
	return max(0, l.BudgetXOF-c.Committed())
}

// Refus possibles, dans l'ordre où on les vérifie.
const (
	ReasonNone     = ""
	ReasonBudget   = "budget_exhausted"
	ReasonUses     = "uses_exhausted"
	ReasonPerUser  = "user_limit_reached"
	ReasonNoWallet = "no_user" // une limite par personne sans personne connue
)

// Allows dit si une remise de `discountXOF` peut ENCORE être accordée.
//
// ⚠️ UNE REMISE QUI NE TIENT PAS DANS L'ENVELOPPE EST REFUSÉE, PAS RABOTÉE.
// Une remise tronquée — « −137 F » au lieu de « −500 F » — est une offre que
// personne n'a annoncée, et le client la lit comme une erreur. L'enveloppe
// peut donc se terminer avec un reste inutilisé : c'est la contrepartie, et
// elle s'explique.
//
// Conséquence utile en fin d'enveloppe : les PETITES remises passent encore
// quand les grandes ne passent plus. Une promotion en pourcentage s'éteint
// donc d'abord pour les longs trajets — ce qui est exactement ce qu'on veut
// d'un budget qui se termine.
func Allows(l Limits, c Counters, userUses, discountXOF int) (reason string, ok bool) {
	if discountXOF <= 0 {
		return ReasonNone, false
	}
	if l.MaxUses > 0 && c.Uses() >= l.MaxUses {
		return ReasonUses, false
	}
	if l.BudgetXOF > 0 && c.Committed()+discountXOF > l.BudgetXOF {
		return ReasonBudget, false
	}
	// ⚠️ La limite par personne se vérifie avec un `userUses` NÉGATIF quand
	// l'appelant ne sait pas qui demande. Refuser est alors le bon défaut :
	// une offre « une fois par personne » servie à un inconnu est une offre
	// sans limite.
	if l.MaxUsesPerUser > 0 {
		if userUses < 0 {
			return ReasonNoWallet, false
		}
		if userUses >= l.MaxUsesPerUser {
			return ReasonPerUser, false
		}
	}
	return ReasonNone, true
}

// Exhausted dit qu'une promotion est TERMINÉE — plus rien ne passera, quelle
// que soit la remise. Sert à l'éteindre dans une liste plutôt que de la
// laisser paraître vivante.
func Exhausted(l Limits, c Counters) bool {
	if l.MaxUses > 0 && c.Uses() >= l.MaxUses {
		return true
	}
	return l.BudgetXOF > 0 && c.Committed() >= l.BudgetXOF
}

// Progress rend la part de l'enveloppe consommée, en pourcentage, ou -1 sans
// budget. C'est ce qu'une console dessine en barre.
func Progress(l Limits, c Counters) int {
	if l.BudgetXOF <= 0 {
		return -1
	}
	return min(100, c.Committed()*100/l.BudgetXOF)
}
