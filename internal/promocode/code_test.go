package promocode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// LES CODES PROMO — ce qu'un code accorde, et ce qu'il refuse.

func percentCode(pct, cap int) *Code {
	return &Code{
		Code: "DIRA10", Kind: KindCampaign, DiscountKind: KindPercent, Value: pct,
		MaxDiscountXOF: cap, Active: true,
		StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour),
	}
}

// --- CE QU'UN CODE ACCORDE ------------------------------------------------

func TestAPercentageCodeTakesItsShare(t *testing.T) {
	assert.Equal(t, 400, percentCode(10, 0).DiscountOn(4000))
}

// ⚠️ UN POURCENTAGE SANS PLAFOND EST UNE PORTE OUVERTE : 20 % d'une location à
// 55 000 F font 11 000 F offerts sur UNE opération. Une offre pensée pour les
// petits paniers se paie alors sur les gros.
func TestTheCapBoundsAPercentage(t *testing.T) {
	assert.Equal(t, 1000, percentCode(20, 1000).DiscountOn(55000))
	assert.Equal(t, 400, percentCode(20, 1000).DiscountOn(2000), "sous le plafond, le pourcentage s'applique")
}

// ⚠️ JAMAIS PLUS QUE LE MONTANT. Une remise de 2 000 F sur une course de 600 F
// rendrait un prix négatif, et quelque part quelqu'un paierait le client pour
// rouler.
func TestADiscountNeverExceedsTheAmount(t *testing.T) {
	c := &Code{DiscountKind: KindAmount, Value: 2000}
	assert.Equal(t, 600, c.DiscountOn(600))
}

// ⚠️ ARRONDIE VERS LE BAS, exprès : arrondir une remise vers le haut ferait
// sortir de l'enveloppe un franc que personne n'a budgété — et sur cent mille
// usages, cela se voit.
func TestADiscountIsRoundedDownToFifty(t *testing.T) {
	assert.Equal(t, 350, percentCode(9, 0).DiscountOn(4000), "9 % de 4 000 = 360 → 350")
	assert.Zero(t, percentCode(1, 0).DiscountOn(4000), "40 F ne vaut rien : mieux vaut aucune remise")
}

// Un montant minimum protège d'offrir une opération entière.
func TestBelowTheMinimumNothingIsGranted(t *testing.T) {
	c := percentCode(50, 0)
	c.MinAmountXOF = 2000
	assert.Zero(t, c.DiscountOn(1500))
	assert.Equal(t, 1000, c.DiscountOn(2000))
}

// --- OÙ UN CODE VAUT -----------------------------------------------------

// ⚠️ VIDE VAUT « PARTOUT », et non « nulle part ». Un code créé sans préciser
// doit marcher : l'inverse aurait fait des codes muets dont on cherche le
// réglage manquant.
func TestACodeWithoutVerticalsWorksEverywhere(t *testing.T) {
	c := percentCode(10, 0)
	assert.True(t, c.Serves(VerticalVTC))
	assert.True(t, c.Serves(VerticalFood))
}

func TestACodeRestrictedToOneServiceRefusesTheOther(t *testing.T) {
	c := percentCode(10, 0)
	c.Verticals = []string{VerticalFood}
	assert.True(t, c.Serves(VerticalFood))
	assert.False(t, c.Serves(VerticalVTC))
}

func TestACodeOutsideItsWindowIsNotLive(t *testing.T) {
	c := percentCode(10, 0)
	assert.True(t, c.Live(time.Now()))

	c.EndsAt = time.Now().Add(-time.Minute)
	assert.False(t, c.Live(time.Now()), "terminé")

	c.StartsAt, c.EndsAt = time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)
	assert.False(t, c.Live(time.Now()), "pas encore commencé")

	c.StartsAt, c.EndsAt = time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	c.Active = false
	assert.False(t, c.Live(time.Now()), "éteint à la main")
}

// --- COMMENT UN CODE S'ÉCRIT ET SE SAISIT --------------------------------

// ⚠️ ON NORMALISE CE QUE LES GENS TAPENT plutôt que de le refuser. Ils
// recopient « DIRA-10 » depuis une affiche, et refuser ce qu'on leur a montré
// est notre faute, pas la leur.
func TestWhatPeopleTypeIsNormalisedNotRefused(t *testing.T) {
	for _, in := range []string{"dira10", "DIRA 10", "dira-10", " Dira_10 ", "DIRA10"} {
		assert.Equal(t, "DIRA10", Normalise(in), "saisi : %q", in)
	}
}

func TestACodeIsLettersAndDigitsOnly(t *testing.T) {
	assert.True(t, ValidCode("DIRA10"))
	assert.True(t, ValidCode("AWA7K2M"))
	assert.False(t, ValidCode("ABC"), "trop court : trois caractères se devinent")
	assert.False(t, ValidCode("DIRA 10"), "un espace ne se dicte pas")
	assert.False(t, ValidCode("dira10"), "la normalisation passe avant la validation")
	assert.False(t, ValidCode("DIRÀ10"), "un accent ne se tape pas sur tous les claviers")
}

// ⚠️ NI `O` NI `0`, NI `I` NI `1`, NI `L` DANS UN CODE TIRÉ. Un code se dicte
// au téléphone et se recopie depuis une affiche : chaque caractère ambigu est
// un code que quelqu'un n'arrivera pas à saisir, et un appel au support qui
// coûte plus cher que la remise.
func TestADrawnCodeHasNoAmbiguousCharacters(t *testing.T) {
	for _, bad := range []string{"O", "0", "I", "1", "L"} {
		assert.NotContains(t, drawAlphabet, bad, "%q est ambigu", bad)
	}
	for range 50 {
		c := randomCode(8)
		assert.True(t, ValidCode(c), "tiré : %q", c)
		assert.Len(t, c, 8)
	}
}

// ⚠️ UN TIRAGE DE MOINS DE QUATRE CARACTÈRES SE DEVINE. Le plancher est posé
// dans le tirage lui-même, pas laissé à l'appelant.
func TestADrawnCodeIsNeverShorterThanFour(t *testing.T) {
	assert.Len(t, randomCode(1), 4)
	assert.Len(t, randomCode(0), 4)
}

// --- L'ENVELOPPE, QUI VIENT DU SOCLE -------------------------------------

// ⚠️ LE MOTEUR N'EST PAS RÉÉCRIT : enveloppe, compteurs en deux temps et
// limite par personne viennent de `pkg/promo`, celui que les deux verticales
// utilisent déjà. Ce test le CONSTATE — si un jour quelqu'un recopie ces
// règles ici, il tombera.
func TestTheEnvelopeComesFromTheSharedEngine(t *testing.T) {
	c := percentCode(10, 0)
	c.Limits = promo.Limits{BudgetXOF: 1000, MaxUsesPerUser: 1}

	reason, ok := promo.Allows(c.Limits, c.Counters, 0, 400)
	assert.True(t, ok)
	assert.Equal(t, promo.ReasonNone, reason)

	// Déjà utilisé une fois par cette personne.
	reason, ok = promo.Allows(c.Limits, c.Counters, 1, 400)
	assert.False(t, ok)
	assert.Equal(t, promo.ReasonPerUser, reason)

	// L'enveloppe ne tient plus la remise : refusée, pas rabotée.
	c.Counters = promo.Counters{AmountSpent: 800}
	reason, ok = promo.Allows(c.Limits, c.Counters, 0, 400)
	assert.False(t, ok)
	assert.Equal(t, promo.ReasonBudget, reason)
}

// ⚠️ UNE LIMITE PAR PERSONNE SANS PERSONNE CONNUE REFUSE. Une offre « une fois
// par personne » servie à un inconnu est une offre sans limite.
func TestAPerPersonLimitWithoutAPersonRefuses(t *testing.T) {
	c := percentCode(10, 0)
	c.Limits = promo.Limits{MaxUsesPerUser: 1}
	reason, ok := promo.Allows(c.Limits, c.Counters, -1, 400)
	assert.False(t, ok)
	assert.Equal(t, promo.ReasonNoWallet, reason)
}

// --- LES PSEUDONYMES D'INFLUENCEURS --------------------------------------

// ⚠️ SANS LE `@`, et en minuscules. Les gens le recopient avec l'arobase depuis
// un profil ; le garder ferait deux fiches pour la même personne, et la
// recherche n'en trouverait qu'une.
func TestAHandleIsNormalised(t *testing.T) {
	for _, in := range []string{"@Awa.Lome", "awa.lome", " @awa.lome ", "AWA.LOME"} {
		assert.Equal(t, "awa.lome", NormaliseHandle(in), "saisi : %q", in)
	}
}

// Un réseau vide est admis : on inscrit quelqu'un avant de savoir où il publie.
func TestAnUnknownNetworkIsRefusedButAnEmptyOneIsNot(t *testing.T) {
	assert.True(t, ValidNetwork(""))
	assert.True(t, ValidNetwork("tiktok"))
	assert.False(t, ValidNetwork("mastodon"))
}

// --- LE PARRAINAGE -------------------------------------------------------

// ⚠️ LE DÉFAUT NE DISTRIBUE RIEN. Un parrainage actif par défaut distribuerait
// de l'argent dans cinq pays le jour du déploiement, sans qu'aucune direction
// ne l'ait décidé ni budgété.
func TestReferralIsOffUntilSomeoneSetsIt(t *testing.T) {
	assert.Zero(t, DefaultReferral.InviteeXOF)
	assert.Zero(t, DefaultReferral.SponsorXOF)
	assert.Positive(t, DefaultReferral.ValidDays, "mais un code tiré doit avoir une fin")
}
