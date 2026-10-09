package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CE QUE LE PAYS DÉCIDE DU BOUTON D'ALERTE.

// ⚠️⚠️ LE TEST LE PLUS IMPORTANT DE CE FICHIER : LES APPLICATIONS NE REÇOIVENT
// AUCUN NUMÉRO À COMPOSER.
//
// Ce n'est pas une précaution sur la qualité des données, c'est le PROTOCOLE :
// l'alerte part au service client, un opérateur appelle d'abord la personne, et
// c'est lui qui appelle les secours. Le téléphone de quelqu'un en danger ne
// compose rien.
//
// ⚠️ ET C'EST VÉRIFIÉ SUR LA FORME, PAS SEULEMENT ÉCRIT DANS UNE SPEC. Un champ
// servi « au cas où », avec la consigne de ne pas l'afficher, aurait fini par un
// bouton d'appel : un champ qui existe se câble. Le type rendu aux applications
// n'a donc PAS de champ `numbers` du tout — ce test échoue à la compilation si
// quelqu'un l'y remet.
//
// ⚠️ CE TEST A AFFIRMÉ L'INVERSE pendant une journée : « aucun numéro n'est
// préchargé, pour aucun pays », parce qu'un numéro approximatif serait composé
// par quelqu'un en panique. Le raisonnement était juste ; ce qui l'a renversé
// est que PERSONNE EN DANGER NE COMPOSE PLUS. Les numéros sont maintenant au
// catalogue (`pkg/country.Emergency`), et un opérateur qui tombe sur un numéro
// faux l'entend, raccroche et prend le suivant.
func TestApplicationsNeverReceiveANumberToDial(t *testing.T) {
	out := sosResponse(SOS{})
	assert.True(t, out.Button)
	// ⚠️ CE QUE L'APPLICATION PROMET À LA PLACE : « on vous rappelle ». C'est
	// la seule chose que la personne cherche à savoir après avoir appuyé.
	assert.True(t, out.CallsBack)
}

// LA CONSOLE, ELLE, REÇOIT LES NUMÉROS DU CATALOGUE — c'est elle qui appelle.
//
// ⚠️ LE CATALOGUE D'ABORD, exactement comme la monnaie d'un pays : ouvrir un
// pays un vendredi soir ne doit pas le laisser sans numéro tout le week-end.
func TestTheConsoleGetsTheCountrySNumbersFromTheCatalogue(t *testing.T) {
	out := sosAdminResponse("SN", SOS{})
	require.NotEmpty(t, out.Numbers, "le Sénégal a ses numéros au catalogue")
	assert.NotNil(t, out.Numbers, "une tranche vide, jamais `nil`")
	byKind := map[string]string{}
	for _, n := range out.Numbers {
		byKind[n.Kind] = n.Number
		assert.NotEmpty(t, n.Label, "chaque ligne se nomme sur un bouton")
	}
	assert.Equal(t, "17", byKind[EmergencyPolice])
	assert.Equal(t, "18", byKind[EmergencyFire])
	// ⚠️ ET `confirmed: false` TANT QUE L'EXPLOITATION N'A RIEN VALIDÉ. Celui
	// qui compose doit savoir s'il est le premier à essayer : les sources
	// publiques se contredisent, et un numéro officiel peut être hors service.
	assert.False(t, out.Confirmed)
}

// ⚠️ LE RÉGLAGE DE L'EXPLOITATION REMPLACE LE CATALOGUE, IL NE S'AJOUTE PAS.
// Deux « police » sur l'écran d'un opérateur, c'est une hésitation d'une seconde
// au moment où il n'en a pas.
func TestAConfirmedNumberReplacesTheCatalogueOne(t *testing.T) {
	out := sosAdminResponse("SN", SOS{Numbers: []EmergencyNumber{
		{Kind: EmergencyPolice, Label: "Police — Dakar", Number: "800112233"},
	}})
	assert.True(t, out.Confirmed, "l'exploitation a tranché")
	police := 0
	for _, n := range out.Numbers {
		if n.Kind == EmergencyPolice {
			police++
			assert.Equal(t, "800112233", n.Number, "celui de l'exploitation gagne")
		}
	}
	assert.Equal(t, 1, police, "un seul par genre")
	// Les genres qu'elle n'a pas réglés restent ceux du catalogue.
	kinds := map[string]bool{}
	for _, n := range out.Numbers {
		kinds[n.Kind] = true
	}
	assert.True(t, kinds[EmergencyFire], "les pompiers viennent toujours du catalogue")
}

// ⚠️ UN NUMÉRO ABSENT DU CATALOGUE NE DONNE PAS DE LIGNE. Les sources ne
// concordent pas pour l'ambulance de la Guinée : une ligne « Ambulance : » sans
// numéro serait un bouton qui ne mène à rien, ce qui est pire que son absence.
func TestAMissingCatalogueNumberMakesNoButton(t *testing.T) {
	out := sosAdminResponse("GN", SOS{})
	for _, n := range out.Numbers {
		assert.NotEqual(t, EmergencyAmbulance, n.Kind,
			"la Guinée n'a pas de numéro d'ambulance fiable : pas de bouton")
		assert.NotEmpty(t, n.Number)
	}
}

// Un pays hors catalogue ne fabrique rien.
func TestAnUnknownCountryYieldsNoNumbers(t *testing.T) {
	out := sosAdminResponse("ZZ", SOS{})
	assert.Empty(t, out.Numbers)
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
