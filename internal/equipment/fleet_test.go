package equipment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ⚠️⚠️ LE TEST QUI PROTÈGE UN CHAUFFEUR : LE MATÉRIEL D'UNE SOCIÉTÉ NE SE
// PRÉLÈVE JAMAIS SUR SES GAINS. `fleetPlan` ferme les canaux de recouvrement
// automatiques, parce qu'il n'existe aucun grand livre de flotte — et qu'un
// canal resté ouvert aurait pris, sur la course d'un chauffeur, le loyer du
// gilet de son patron.
func TestAFleetContractNeverCollectsFromAnyonesEarnings(t *testing.T) {
	// Un plan « tout ouvert », comme un contrat d'agent le plus agressif.
	p := Plan{
		CollectFromEarnings: true, CollectFromWallet: true, AllowPartial: true,
		EarningsPercent: 30, EarningsFixedXOF: 500, BlockAfterDays: 7,
		Schedule: "installments", Installments: 4, Period: "week",
	}
	f := fleetPlan(p)

	assert.False(t, f.CollectFromEarnings, "aucune retenue sur les gains de quiconque")
	assert.False(t, f.CollectFromWallet, "une société n'a pas de solde Dira")
	assert.Zero(t, f.EarningsPercent)
	assert.Zero(t, f.EarningsFixedXOF)
	// ⚠️ LE BLOCAGE AUSSI EST RETIRÉ : il coupe la mise en ligne d'un AGENT.
	// Sur un contrat de société il n'a personne à couper — mais le laisser
	// ferait croire, en lisant le plan, qu'un impayé de flotte bloque
	// quelqu'un.
	assert.Zero(t, f.BlockAfterDays)

	// ⚠️ ET L'ÉCHÉANCIER RESTE : la société doit bel et bien ces sommes, et
	// l'écran doit pouvoir les montrer. Fermer les canaux de recouvrement
	// n'efface pas la dette — ça dit seulement que personne ne la prélève
	// automatiquement.
	assert.Equal(t, "installments", f.Schedule)
	assert.Equal(t, 4, f.Installments)
	assert.Equal(t, "week", f.Period)
}
