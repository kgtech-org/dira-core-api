package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// CE QUE DONNE « INVITE UN AMI » — et d'abord : rien, tant que personne n'a
// décidé.

// ⚠️ LE PARRAINAGE EST ÉTEINT TANT QU'UN PAYS N'A RIEN RÉGLÉ. C'est le seul
// défaut tenable pour un réglage qui SORT DE L'ARGENT : un parrainage actif
// d'office aurait commencé à payer dans cinq pays le jour du déploiement, sans
// budget, et on l'aurait découvert sur le grand livre.
func TestReferralGivesNothingUntilACountrySetsIt(t *testing.T) {
	got := referralResponse(Referral{})
	assert.False(t, got.Active, "un pays jamais touché ne parraine pas")
	assert.Zero(t, got.InviteeXOF)
	assert.Zero(t, got.SponsorXOF)
	assert.Equal(t, defaultReferralValidDays, got.ValidDays,
		"mais un code tiré doit quand même avoir une fin")
}

// ⚠️ « ACTIF » SE DÉDUIT DE LA REMISE DU FILLEUL, et n'est pas un interrupteur
// enregistré à côté. Deux états absurdes seraient devenus possibles : actif à
// zéro franc — un bouton « inviter un ami » qui ne donne rien —, et éteint avec
// des montants réglés que plus personne ne voit.
func TestActiveIsDeducedFromWhatTheInviteeGets(t *testing.T) {
	assert.True(t, referralResponse(Referral{InviteeXOF: 1000}).Active)
	// Le parrain seul ne suffit pas : sans remise, aucun code n'est tiré, donc
	// aucun filleul ne l'utilise, donc aucun parrain n'est jamais payé. Un
	// « actif » dans cet état aurait promis à la console une mécanique qui ne
	// peut pas démarrer.
	assert.False(t, referralResponse(Referral{SponsorXOF: 500}).Active)
}

// ⚠️ LA DURÉE DE VIE EST TOUJOURS RENDUE, même quand la base ne la porte pas :
// la console affiche un champ, et un zéro dans « validité » lui aurait fait
// écrire « 0 jour » pour un code qui vit un an.
func TestTheValidityIsAlwaysAnswered(t *testing.T) {
	assert.Equal(t, 30, referralResponse(Referral{InviteeXOF: 500, ValidDays: 30}).ValidDays)
	assert.Equal(t, defaultReferralValidDays,
		referralResponse(Referral{InviteeXOF: 500}).ValidDays)
}

// ⚠️ UN GARDE-FOU D'ORDRE DE GRANDEUR, SUR LES DEUX CÔTÉS. Un crédit de
// parrain monstrueux coûte exactement autant qu'une remise de filleul
// monstrueuse ; borner un seul des deux champs n'aurait protégé que la moitié
// de la dépense.
//
// ⚠️ ET IL NE PRÉTEND PAS ATTRAPER LES FAUTES DE FRAPPE : « 20 000 » au lieu de
// « 2 000 » passe, parce que 20 000 F est une décision possible là où le panier
// est gros. C'est le journal d'audit qui répond à celle-là, après coup.
func TestAWildlyWrongAmountIsCaughtOnBothSides(t *testing.T) {
	assert.False(t, Referral{InviteeXOF: 2000, SponsorXOF: 1000}.tooLarge())
	assert.False(t, Referral{InviteeXOF: maxReferralXOF}.tooLarge(), "la borne elle-même passe")
	assert.False(t, Referral{InviteeXOF: 20000}.tooLarge(), "dix fois trop, mais possible")
	assert.True(t, Referral{InviteeXOF: 2_000_000, SponsorXOF: 1000}.tooLarge())
	assert.True(t, Referral{SponsorXOF: 500_000}.tooLarge())
	// ⚠️ Et le nombre de filleuls n'est PAS borné par cette règle : ce n'est
	// pas un montant. Dix mille filleuls à 1 000 F est une campagne qu'on peut
	// vouloir ; c'est l'enveloppe du code qui la borne, pas ce champ.
	assert.False(t, Referral{InviteeXOF: 1000, MaxSponsored: 100000}.tooLarge())
}
