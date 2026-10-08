package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CE QUE LE PAYS DÉCIDE DU BOUTON D'ALERTE.

// ⚠️ LE TEST LE PLUS IMPORTANT DE CE FICHIER : AUCUN NUMÉRO N'EST PRÉCHARGÉ.
// C'est contraire à l'habitude du reste de la base, où l'on précharge des
// défauts raisonnables — et c'est délibéré. Un numéro d'urgence approximatif
// serait COMPOSÉ PAR QUELQU'UN EN DANGER : « probablement le 17 » n'est pas une
// valeur par défaut acceptable. Un bouton absent envoie chercher le secours
// autrement ; un bouton qui compose un mauvais numéro fait perdre les trente
// secondes qui comptent.
func TestNoEmergencyNumberIsEverGuessed(t *testing.T) {
	out := sosResponse(SOS{})
	assert.Empty(t, out.Numbers, "aucun numéro inventé, pour aucun pays")
	// ⚠️ ET UNE TRANCHE VIDE, JAMAIS `nil` : `numbers: null` en JSON fait
	// planter une application qui boucle dessus sans vérifier — et c'est
	// l'écran d'urgence.
	assert.NotNil(t, out.Numbers)
}

// Le bouton existe partout. Les détections ont des défauts, et le vocal est le
// seul éteint — il demande d'écouter le micro en permanence.
func TestTheButtonIsAlwaysOnAndOnlyVoiceIsOffByDefault(t *testing.T) {
	out := sosResponse(SOS{})
	assert.True(t, out.Button)
	assert.True(t, out.Shake, "on secoue un téléphone qu'on ne peut pas regarder")
	assert.True(t, out.Crash, "un accident est le cas où personne n'appuiera")
	assert.False(t, out.Voice, "écouter le micro en permanence s'allume explicitement")
	assert.Equal(t, 10, out.CountdownSeconds)
}

// ⚠️ « NON RÉGLÉ » ET « ÉTEINT » NE SONT PAS LA MÊME CHOSE. Des pointeurs,
// parce qu'un `bool` nu aurait éteint la secousse dans tout pays enregistré
// avant ce réglage — c'est-à-dire tous.
func TestACountryThatTurnedADetectionOffKeepsItOff(t *testing.T) {
	off := false
	out := sosResponse(SOS{Crash: &off})
	assert.False(t, out.Crash, "le pays l'a VOULU éteint")
	assert.True(t, out.Shake, "et n'a rien dit du reste")

	on := true
	assert.True(t, sosResponse(SOS{Voice: &on}).Voice)
}

// ⚠️ UN NUMÉRO VIDE EST REFUSÉ, alors que tout le reste du SOS est tolérant. Un
// bouton d'appel sans numéro est le seul cas vraiment indéfendable : il a l'air
// de marcher, on appuie, et rien ne se passe. L'absence de bouton est meilleure.
func TestABlankNumberIsRefusedRatherThanShownAsAButton(t *testing.T) {
	_, err := normaliseNumbers([]EmergencyNumber{{Kind: EmergencyPolice, Number: "   "}})
	require.Error(t, err)
	_, err = normaliseNumbers([]EmergencyNumber{{Kind: EmergencyPolice, Number: "police secours"}})
	assert.Error(t, err, "des lettres ne se composent pas")
}

// ⚠️ UN NUMÉRO COURT RESTE COURT. « 17 » ne doit pas être « corrigé » en
// +228 17 : les numéros d'urgence ne sont pas des numéros E.164, et les
// normaliser comme tels les rendrait incomposables.
func TestAShortEmergencyNumberIsNotTurnedIntoAnInternationalOne(t *testing.T) {
	out, err := normaliseNumbers([]EmergencyNumber{
		{Kind: EmergencyPolice, Number: "17"},
		{Kind: EmergencyAmbulance, Number: "15 15"},
		{Kind: EmergencyPlatform, Number: "+228 90 00 00 01"},
	})
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "17", out[0].Number)
	assert.Equal(t, "1515", out[1].Number, "les espaces de présentation partent")
	assert.Equal(t, "+22890000001", out[2].Number, "le + reste")
}

// ⚠️ UN SEUL NUMÉRO PAR GENRE. Deux « police » feraient deux boutons identiques
// sur l'écran d'urgence, et personne ne saurait lequel appuyer — au moment
// précis où il ne faut pas réfléchir.
func TestOnlyOneNumberPerKindSurvives(t *testing.T) {
	out, err := normaliseNumbers([]EmergencyNumber{
		{Kind: EmergencyPolice, Number: "17"},
		{Kind: EmergencyPolice, Number: "117"},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "17", out[0].Number, "le premier gagne")
}

// Un genre inventé est refusé : l'application dessine une icône par genre, et
// `fire_brigade` n'en aurait aucune.
func TestAnInventedKindIsRefused(t *testing.T) {
	_, err := normaliseNumbers([]EmergencyNumber{{Kind: "gendarmerie", Number: "17"}})
	assert.Error(t, err)
}

// ⚠️ LE NUMÉRO DE L'EXPLOITATION COMPTE AUTANT QUE LA POLICE. Quelqu'un dont la
// course tourne mal n'a pas toujours affaire à la police : un passager
// agressif, une dispute sur un prix, une route bloquée. Sans ce genre,
// l'application n'aurait que la police à proposer — soit un appel de trop, soit
// aucun appel.
func TestThePlatformIsOneOfTheNumbersOneCanCall(t *testing.T) {
	assert.Contains(t, EmergencyKinds, EmergencyPlatform)
	out, err := normaliseNumbers([]EmergencyNumber{
		{Kind: EmergencyPlatform, Label: "Astreinte Dira", Number: "+22890000001"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Astreinte Dira", out[0].Label)
}
