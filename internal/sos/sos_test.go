package sos

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// LE BOUTON D'ALERTE : ce qui ne doit JAMAIS faire perdre un appel au secours.

// ⚠️ LE TEST QUI COMPTE LE PLUS DE CE FICHIER. Tout ce qu'une application peut
// envoyer de bancal au pire moment — réseau coupé, GPS pas encore fixé, champ
// mal câblé — doit produire une alerte, pas un refus. Un `422` sur un appel au
// secours est indéfendable, et c'est la seule chose que ce module n'a pas le
// droit de faire.
func TestNothingAnAppCanSendThrowsTheAlarmAway(t *testing.T) {
	for name, a := range map[string]*Alert{
		"tout vide":            {},
		"source inventée":      {Source: "double_tap_volume"},
		"position à zéro":      {RaisedPos: &Position{Lat: 0, Lng: 0}},
		"latitude impossible":  {RaisedPos: &Position{Lat: 412, Lng: 1.2}},
		"batterie à 300":       {Battery: 300},
		"batterie négative":    {Battery: -5},
		"verticale inconnue":   {Vertical: "scooter"},
		"note interminable":    {Note: strings.Repeat("au secours ", 5000)},
		"source en majuscules": {Source: "  CRASH  "},
	} {
		Normalise(a)
		assert.True(t, ValidSource(a.Source), "%s : la source doit rester utilisable", name)
		assert.LessOrEqual(t, len(a.Note), 1000, name)
		assert.True(t, a.Battery >= 0 && a.Battery <= 100, name)
		assert.Contains(t, []string{"", "vtc", "food"}, a.Vertical, name)
	}
}

// ⚠️ ON JETTE LA POSITION, PAS L'ALERTE. `0,0` est la valeur d'un capteur qui
// n'a pas encore fixé : l'afficher enverrait un opérateur regarder le golfe de
// Guinée, ce qui est PIRE que « position inconnue » — il croirait savoir.
func TestAnUnfixedSensorLosesItsPositionAndKeepsTheAlarm(t *testing.T) {
	a := &Alert{Source: SourceButton, RaisedPos: &Position{Lat: 0, Lng: 0}}
	Normalise(a)
	assert.Nil(t, a.RaisedPos, "pas de point inventé")
	assert.Equal(t, SourceButton, a.Source, "l'alerte, elle, reste entière")

	// Et une vraie position passe, même juste après une frontière : quelqu'un
	// peut être en danger au Bénin avec un compte togolais.
	ok := &Alert{Source: SourceButton, RaisedPos: &Position{Lat: 6.3654, Lng: 2.4183}}
	Normalise(ok)
	assert.NotNil(t, ok.RaisedPos)
}

// ⚠️ UNE SOURCE INCONNUE DEVIENT `button`, et ne reste PAS telle quelle. Garder
// le mot inconnu aurait fait passer l'alerte à travers tous les comptages par
// source sans qu'on le voie : elle serait traitée, mais invisible dans « combien
// de chocs détectés ce mois-ci ».
func TestAnUnknownTriggerBecomesAPlainButtonPress(t *testing.T) {
	a := &Alert{Source: "secoué_trois_fois"}
	Normalise(a)
	assert.Equal(t, SourceButton, a.Source)
	assert.True(t, a.Confirmed, "un bouton EST la confirmation")
}

// ⚠️ UN GESTE EST TOUJOURS CONFIRMÉ, quoi que dise l'application. Accepter
// `confirmed: false` sur un bouton pressé aurait laissé un client mal câblé
// faire passer de vraies alertes pour des mesures que personne n'a validées —
// et la console les aurait triées comme telles.
func TestAPressedButtonCannotBeReportedAsUnconfirmed(t *testing.T) {
	a := &Alert{Source: SourceButton, Confirmed: false}
	Normalise(a)
	assert.True(t, a.Confirmed)

	// Une DÉTECTION, elle, garde ce que l'application dit : c'est la seule
	// façon de savoir si le compte à rebours a été validé ou s'il s'est écoulé.
	d := &Alert{Source: SourceCrash, Confirmed: false}
	Normalise(d)
	assert.False(t, d.Confirmed)
}

// ⚠️⚠️ LE PIÈGE CENTRAL DE CE MODULE, ET IL SE LIT À L'ENVERS : une détection
// NON confirmée est PLUS grave, pas moins. L'intuition dit « il n'a pas
// confirmé, c'est sûrement un faux » — et après un choc violent, « personne n'a
// annulé » veut souvent dire « personne ne POUVAIT annuler ». Une console qui
// trierait les non confirmées en bas de la file mettrait systématiquement les
// accidents les plus graves en dernier.
func TestAnUnconfirmedCrashIsTheMostUrgentThingInTheQueue(t *testing.T) {
	assert.True(t, Grave(SourceCrash, false), "personne n'a annulé : c'est le pire cas")
	assert.True(t, Grave(SourceCrash, true))
	assert.True(t, Grave(SourceShake, false))
	assert.True(t, Grave(SourceButton, true), "appuyer est une décision")

	// Et le libellé le DIT, pour qu'aucun écran n'ait à le déduire.
	assert.Contains(t, TriggerLabel(SourceCrash, false), "PERSONNE N'A ANNULÉ")
	assert.NotContains(t, TriggerLabel(SourceCrash, true), "PERSONNE")
}

// Une secousse confirmée reste ordinaire : quelqu'un a secoué puis validé, ce
// qui est un geste volontaire comme un autre.
func TestAConfirmedShakeIsAnOrdinaryAlarm(t *testing.T) {
	assert.False(t, Grave(SourceShake, true))
	assert.False(t, Grave(SourceVoice, true))
}

// Les deux listes servies à l'exploitation sont fermées : un dénouement inventé
// ne peut pas entrer, sinon « combien de vraies alertes ce mois-ci » ne se
// calcule plus.
func TestTheOutcomeListIsClosed(t *testing.T) {
	for _, o := range Outcomes {
		assert.True(t, ValidOutcome(o), o)
		assert.NotEqual(t, o, OutcomeLabel(o), "chaque dénouement se dit en clair : %s", o)
	}
	assert.False(t, ValidOutcome("resolu"))
	assert.False(t, ValidOutcome(""))
}

// ⚠️ « INJOIGNABLE » N'EST PAS UNE FAUSSE ALERTE, et les confondre serait le
// pire mensonge de la liste : c'est le dénouement le plus inquiétant de tous.
func TestUnreachableIsItsOwnOutcome(t *testing.T) {
	assert.NotEqual(t, OutcomeLabel(OutcomeUnreachable), OutcomeLabel(OutcomeFalseAlarm))
	assert.Contains(t, OutcomeLabel(OutcomeUnreachable), "injoignable")
}

// Le texte est coupé sur une frontière de rune : couper au milieu d'un
// caractère accentué produirait du charabia dans la file de l'exploitation.
func TestALongNoteIsCutOnARuneBoundary(t *testing.T) {
	a := &Alert{Note: strings.Repeat("é", 2000)}
	Normalise(a)
	assert.LessOrEqual(t, len(a.Note), 1000)
	assert.True(t, utf8Valid(a.Note), "pas de rune coupée en deux")
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
