package promo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func env(budget, uses, perUser int) Limits {
	return Limits{BudgetXOF: budget, MaxUses: uses, MaxUsesPerUser: perUser}
}

// ⚠️ ZÉRO VEUT DIRE « PAS DE LIMITE ». L'inverse aurait éteint, en silence et
// le jour du déploiement, toute promotion créée avant que ces champs
// n'existent.
func TestNoLimitMeansNoLimit(t *testing.T) {
	reason, ok := Allows(Limits{}, Counters{UsesSpent: 10_000, AmountSpent: 9_000_000}, 500, 1000)
	assert.True(t, ok)
	assert.Equal(t, ReasonNone, reason)
	assert.False(t, Exhausted(Limits{}, Counters{AmountSpent: 9_000_000}))
	assert.Equal(t, -1, Limits{}.Remaining(Counters{}), "pas de plafond n'est pas zéro restant")
	assert.Equal(t, -1, Progress(Limits{}, Counters{}))
}

// ⚠️ UNE REMISE QUI NE TIENT PAS DANS L'ENVELOPPE EST REFUSÉE, PAS RABOTÉE.
// Une offre tronquée — « −137 F » au lieu de « −500 F » — est une offre que
// personne n'a annoncée, et le client la lit comme une erreur.
func TestADiscountThatDoesNotFitIsRefusedNotTrimmed(t *testing.T) {
	l := env(10_000, 0, 0)
	c := Counters{AmountSpent: 9_800}

	reason, ok := Allows(l, c, 0, 500)
	assert.False(t, ok)
	assert.Equal(t, ReasonBudget, reason)

	// Mais une petite remise passe encore : l'enveloppe s'éteint d'abord
	// pour les gros montants, ce qui est exactement ce qu'on veut.
	_, ok = Allows(l, c, 0, 200)
	assert.True(t, ok)
	assert.Equal(t, 200, l.Remaining(c))
}

// Le PROMIS compte autant que le DÉPENSÉ : mille personnes ne doivent pas
// consommer une enveloppe de dix pendant que les courses roulent.
func TestWhatIsPromisedCountsToo(t *testing.T) {
	l := env(1_000, 0, 0)
	_, ok := Allows(l, Counters{AmountReserved: 900}, 0, 200)
	assert.False(t, ok, "900 réservés + 200 demandés dépassent 1 000")
	assert.Equal(t, 100, l.Remaining(Counters{AmountReserved: 900}))
}

// ⚠️ LA LIMITE PAR PERSONNE PROTÈGE DE L'ABUS, PAS LE BUDGET. Sans elle, une
// seule personne consomme l'enveloppe entière — et elle le fera : une remise
// sans limite par personne se partage sur les réseaux en quelques heures.
func TestThePerPersonLimitIsWhatStopsAbuse(t *testing.T) {
	l := env(1_000_000, 0, 2)
	_, ok := Allows(l, Counters{}, 1, 500)
	assert.True(t, ok, "première et deuxième fois")
	reason, ok := Allows(l, Counters{}, 2, 500)
	assert.False(t, ok)
	assert.Equal(t, ReasonPerUser, reason)
}

// ⚠️ UNE OFFRE « UNE FOIS PAR PERSONNE » SERVIE À UN INCONNU EST UNE OFFRE
// SANS LIMITE. Quand l'appelant ne sait pas qui demande, on refuse.
func TestAnAnonymousCallerGetsNoPerPersonOffer(t *testing.T) {
	reason, ok := Allows(env(0, 0, 1), Counters{}, -1, 500)
	assert.False(t, ok)
	assert.Equal(t, ReasonNoWallet, reason)

	// Sans limite par personne, l'inconnu reste servi : le problème n'est
	// pas de le connaître, c'est de le compter.
	_, ok = Allows(env(0, 0, 0), Counters{}, -1, 500)
	assert.True(t, ok)
}

func TestTheTotalUseLimitCountsBothStates(t *testing.T) {
	l := env(0, 3, 0)
	_, ok := Allows(l, Counters{UsesSpent: 2}, 0, 500)
	assert.True(t, ok)
	reason, ok := Allows(l, Counters{UsesSpent: 2, UsesReserved: 1}, 0, 500)
	assert.False(t, ok)
	assert.Equal(t, ReasonUses, reason)
}

// ⚠️ LES USAGES RENDUS NE COMPTENT DANS AUCUNE LIMITE. Une annulation qui
// garderait sa réservation ferait fondre un budget sans qu'un franc ne soit
// sorti, et personne ne comprendrait pourquoi l'offre s'est arrêtée.
func TestAReleasedUseCountsForNothing(t *testing.T) {
	c := Counters{UsesSpent: 1, AmountSpent: 500, UsesReleased: 9}
	assert.Equal(t, 1, c.Uses())
	assert.Equal(t, 500, c.Committed())
	_, ok := Allows(env(1_000, 2, 0), c, 0, 400)
	assert.True(t, ok)
}

// Une remise nulle ou négative n'est pas une remise.
func TestNoDiscountIsNotADiscount(t *testing.T) {
	_, ok := Allows(Limits{}, Counters{}, 0, 0)
	assert.False(t, ok)
	_, ok = Allows(Limits{}, Counters{}, 0, -100)
	assert.False(t, ok)
}

// Épuisée = plus rien ne passera, quelle que soit la remise. C'est ce qui
// permet de l'éteindre dans une liste plutôt que de la laisser paraître
// vivante.
func TestExhaustedIsAboutTheOfferNotOneDiscount(t *testing.T) {
	assert.True(t, Exhausted(env(1_000, 0, 0), Counters{AmountSpent: 1_000}))
	assert.True(t, Exhausted(env(0, 5, 0), Counters{UsesSpent: 5}))
	assert.False(t, Exhausted(env(1_000, 0, 0), Counters{AmountSpent: 999}))
	// Une limite par personne n'épuise PAS l'offre : elle la ferme pour
	// quelqu'un, pas pour tout le monde.
	assert.False(t, Exhausted(env(0, 0, 1), Counters{UsesSpent: 4_000}))
}

func TestProgressIsWhatAConsoleDraws(t *testing.T) {
	assert.Equal(t, 0, Progress(env(1_000, 0, 0), Counters{}))
	assert.Equal(t, 50, Progress(env(1_000, 0, 0), Counters{AmountSpent: 500}))
	assert.Equal(t, 75, Progress(env(1_000, 0, 0), Counters{AmountSpent: 500, AmountReserved: 250}))
	assert.Equal(t, 100, Progress(env(1_000, 0, 0), Counters{AmountSpent: 5_000}), "jamais au-delà de 100")
}
