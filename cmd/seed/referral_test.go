package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// LES MONTANTS DU PARRAINAGE — ce qu'on offre, et dans quelle monnaie.

// ⚠️ CHAQUE PAYS OUVERT D'OFFICE DOIT AVOIR SES MONTANTS. C'est le test qui
// compte : le jour où l'on préchargera un pays d'une monnaie nouvelle — le Ghana,
// le Nigeria —, le provisionnement le sauterait en silence et le parrainage
// resterait éteint dans un pays qu'on vient d'ouvrir. Personne ne le verrait
// avant qu'un client ne cherche son code. Ici, c'est la compilation des tests
// qui le dit, avant le déploiement.
func TestEveryPreloadedCountryHasItsReferralAmounts(t *testing.T) {
	for _, code := range country.Preloaded {
		info, ok := country.Lookup(code)
		require.True(t, ok, "%s est préchargé mais absent du catalogue", code)
		pol, ok := referralByCurrency[info.Currency]
		assert.True(t, ok,
			"%s paie en %s, et aucun montant de parrainage n'est chiffré dans cette monnaie — "+
				"décidez-les plutôt que de laisser le pays sans parrainage", code, info.Currency)
		if ok {
			assert.Positive(t, pol.InviteeXOF, "%s : sans remise de filleul, aucun code n'est tiré", code)
		}
	}
}

// ⚠️ LA MONNAIE DONNE L'ÉCHELLE, ET LE FRANC GUINÉEN N'EST PAS LE FRANC CFA.
// 1 000 est une course courte à Lomé et un dixième de course à Conakry. Ce test
// existe parce que l'erreur naturelle est de recopier la ligne du franc CFA :
// elle compile, elle se déploie, et elle offre quinze fois moins que prévu sans
// que rien ne la signale.
func TestTheGuineanFrancIsNotTheCfaFranc(t *testing.T) {
	xof := referralByCurrency["XOF"]
	gnf := referralByCurrency["GNF"]
	require.Positive(t, xof.InviteeXOF)
	require.Positive(t, gnf.InviteeXOF)
	assert.Greater(t, gnf.InviteeXOF, xof.InviteeXOF*5,
		"le franc guinéen vaut ~15 fois moins : un montant du même ordre est une recopie")
}

// ⚠️ ET ON N'INVENTE PAS DE MONTANT DANS UNE MONNAIE QU'ON N'A PAS CHIFFRÉE.
// Le cedi et le naira se stockent en CENTIÈMES : un nombre pensé en francs y
// vaut cent fois moins, et le même nombre pensé en unités y dépasse le
// garde-fou du socle. Le provisionnement laisse donc ces pays éteints et le
// dit — c'est la console qui décidera.
func TestAnUnpricedCurrencyGetsNothingRatherThanAGuess(t *testing.T) {
	for _, cy := range []string{"GHS", "NGN", "EUR", "USD", ""} {
		_, ok := referralByCurrency[cy]
		assert.False(t, ok, "%s n'a pas été chiffré : le pays doit rester éteint", cy)
	}
}

// ⚠️ ET TOUT RESTE SOUS LE GARDE-FOU DU SOCLE (50 000 par côté). Il arrête
// « 2000000 » ; un montant de provisionnement qui le dépasserait ferait échouer
// l'écriture… ou passerait, puisque le provisionnement écrit par le dépôt et non
// par la méthode validée. C'est précisément pour cela qu'on le vérifie ici.
func TestSeededAmountsStayUnderThePlatformGuardrail(t *testing.T) {
	const guardrail = 50_000 // internal/country : maxReferralXOF
	for cy, pol := range referralByCurrency {
		assert.LessOrEqual(t, pol.InviteeXOF, guardrail, "%s : remise du filleul", cy)
		assert.LessOrEqual(t, pol.SponsorXOF, guardrail, "%s : crédit du parrain", cy)
		// Le parrain reçoit moins que le filleul : c'est un crédit sur un solde
		// (il reviendra), pas une remise pour convaincre quelqu'un d'essayer.
		assert.LessOrEqual(t, pol.SponsorXOF, pol.InviteeXOF, "%s", cy)
		assert.Positive(t, pol.MaxSponsored,
			"%s : sans plafond, un code de parrainage posté sur un groupe de mille personnes "+
				"n'est plus un parrainage", cy)
		assert.Positive(t, pol.ValidDays, "%s : un code tiré doit avoir une fin", cy)
	}
}
