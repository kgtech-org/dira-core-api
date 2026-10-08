package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// QUI VOIT QUOI DE QUI — et d'abord : rien ne change pour personne.

// ⚠️ LE CONTRAT DE NON-RÉGRESSION. Chaque ligne reproduit une décision déjà en
// production avant ce réglage. Un pays qui n'a rien touché doit continuer à
// échanger EXACTEMENT ce qu'il échangeait : un déploiement qui change un
// comportement en silence est une panne qu'on met des semaines à relier à sa
// cause.
func TestTheDefaultsReproduceWhatWasAlreadyExchanged(t *testing.T) {
	cases := []struct {
		vertical, audience string
		name               string
		phone, call, alert bool
		rating             bool
	}{
		// La course : le passager voit le nom et la note de son chauffeur,
		// jamais son numéro — la conversation est le canal.
		{VerticalVTC, ToClient, NameFull, false, false, false, true},
		// La course : le chauffeur ne voit RIEN du passager… sauf qu'il peut
		// le faire sonner (le klaxon, en place depuis longtemps).
		{VerticalVTC, ToAgent, NameHidden, false, false, true, false},
		// La livraison : nom, note, numéro et appel, dans les deux sens.
		{VerticalFood, ToClient, NameFull, true, true, false, true},
		{VerticalFood, ToAgent, NameFull, true, true, false, false},
	}
	for _, c := range cases {
		got := DefaultDisclosure(c.vertical, c.audience)
		assert.Equal(t, c.name, got.Name, "%s/%s : nom", c.vertical, c.audience)
		assert.Equal(t, c.phone, got.Phone, "%s/%s : téléphone affiché", c.vertical, c.audience)
		assert.Equal(t, c.call, got.DirectCall, "%s/%s : appel direct", c.vertical, c.audience)
		assert.Equal(t, c.alert, got.InAppAlert, "%s/%s : alerte sonore", c.vertical, c.audience)
		assert.Equal(t, c.rating, got.Rating, "%s/%s : note", c.vertical, c.audience)
		// Rien de ce qui n'était servi nulle part ne s'ouvre tout seul.
		assert.False(t, got.FirstName, "%s/%s : prénom", c.vertical, c.audience)
		assert.False(t, got.LastName, "%s/%s : nom de famille", c.vertical, c.audience)
		assert.False(t, got.Photo, "%s/%s : photo", c.vertical, c.audience)
		assert.False(t, got.Gender, "%s/%s : genre", c.vertical, c.audience)
	}
}

// ⚠️ UN MÉTIER INCONNU NE DIVULGUE RIEN. La règle se FERME : le jour où une
// troisième verticale appellera cette porte sans que personne n'ait réglé sa
// politique, elle ne doit pas hériter de celle de la livraison — qui donne les
// numéros.
func TestAnUnknownVerticalDisclosesNothing(t *testing.T) {
	for _, c := range []struct{ vertical, audience string }{
		{"pharmacie", ToClient}, {VerticalFood, "support"}, {"", ""},
	} {
		got := DefaultDisclosure(c.vertical, c.audience)
		assert.Equal(t, NameHidden, got.Name)
		assert.False(t, got.Phone)
		assert.False(t, got.DirectCall)
		assert.False(t, got.InAppAlert)
		assert.False(t, got.Rating)
	}
}

// ⚠️ « NON RÉGLÉ » ET « REFUSÉ » NE SONT PAS LA MÊME CHOSE. Un booléen nu
// aurait fait tomber tous les pays enregistrés avant ce réglage sur « tout
// refusé » — c'est-à-dire aurait coupé les téléphones de la livraison le jour
// du déploiement.
func TestAnExplicitRefusalIsNotTheSameAsNotSet(t *testing.T) {
	// Non réglé : le défaut du métier, qui donne le numéro en livraison.
	assert.True(t, disclosureResponse(VerticalFood, ToAgent, Disclosure{}).Phone)
	// Réglé à faux : on le respecte.
	assert.False(t, disclosureResponse(VerticalFood, ToAgent, Disclosure{Phone: no()}).Phone)
	// Et un champ réglé n'entraîne pas les autres.
	d := disclosureResponse(VerticalFood, ToAgent, Disclosure{Phone: no()})
	assert.Equal(t, NameFull, d.Name, "fermer le téléphone ne ferme pas le nom")
	assert.True(t, d.DirectCall, "ni l'appel : ce sont trois permissions distinctes")
}

// --- LE MASQUAGE D'UN NOM -------------------------------------------------

func TestANameIsMaskedAtTheChosenLevel(t *testing.T) {
	assert.Equal(t, "Awa Diallo", MaskName(NameFull, "Awa Diallo", "Awa"))
	assert.Equal(t, "Awa", MaskName(NameFirst, "Awa Diallo", "Awa"))
	assert.Equal(t, "A. D.", MaskName(NameInitials, "Awa Diallo", "Awa"))
	assert.Empty(t, MaskName(NameHidden, "Awa Diallo", "Awa"))
}

// ⚠️ UNE BONNE PARTIE DES COMPTES N'A PAS DE PRÉNOM D'ÉTAT CIVIL. Couper le nom
// d'affichage à l'espace est le repli — mais il ne doit pas inventer : sur un
// nom d'un seul mot, « prénom seul » rend ce mot.
func TestWithoutACivilFirstNameTheDisplayNameIsCut(t *testing.T) {
	assert.Equal(t, "Awa", MaskName(NameFirst, "Awa Diallo", ""))
	assert.Equal(t, "Mensah", MaskName(NameFirst, "Mensah", ""))
	assert.Equal(t, "M.", MaskName(NameInitials, "Mensah", ""))
}

// ⚠️ DEUX INITIALES AU PLUS. « A. B. C. D. » sur un compte à quatre mots ne
// masque plus rien et ne se lit pas : la personne reste identifiable dans un
// quartier, ce qui est exactement ce que ce niveau existe pour éviter.
func TestInitialsStopAtTwo(t *testing.T) {
	assert.Equal(t, "J. P.", MaskName(NameInitials, "Jean Pierre Ndong Obame", ""))
}

// Un niveau inconnu rend le nom tel quel plutôt que rien : une politique
// illisible ne doit pas effacer une identité dont un chauffeur a besoin pour
// reconnaître son passager. C'est l'écriture qui refuse un niveau inconnu
// (`validate:"oneof=…"`), pas la lecture.
func TestAnUnknownLevelFallsBackToTheNameAsItIs(t *testing.T) {
	assert.Equal(t, "Awa Diallo", MaskName("chiffré", "Awa Diallo", "Awa"))
}

func TestAnEmptyPrivacyUpdateIsRefused(t *testing.T) {
	assert.Equal(t, "validation_failed", errNoPrivacyUpdate.Code)
	assert.Contains(t, errNoPrivacyUpdate.Message, "direct_call")
	assert.Contains(t, errNoPrivacyUpdate.Message, "in_app_alert")
}
