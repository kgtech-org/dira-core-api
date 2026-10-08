package serviceapi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/internal/country"
	"github.com/kgtech-org/dira-core-api/internal/user"
)

// CE QU'UNE VERTICALE REÇOIT — et surtout ce qu'elle NE reçoit pas.

func awa() *user.Person {
	return &user.Person{
		ID: "6ac6c30bccc38d5a6302f0f5", Role: "client",
		Name: "Awa Diallo", FirstName: "Awa", LastName: "Diallo",
		Phone: "+22890000000", AvatarURL: "https://files.dira.llc/a.jpg",
		Gender: "female", Country: "TG",
	}
}

// ⚠️ UN CHAMP QU'ON N'ENVOIE PAS NE FUIT PAS. Filtrer à l'affichage aurait
// fait traverser le fil à un numéro que l'exploitation venait de fermer, pour
// le laisser dormir dans un journal de requêtes ou un cache.
func TestWhatIsNotDisclosedIsNotEvenSent(t *testing.T) {
	got := discloseResponse(awa(), country.DefaultDisclosure(country.VerticalVTC, country.ToAgent))

	assert.Empty(t, got.Name, "la course ne montre rien du passager au chauffeur")
	assert.Empty(t, got.Phone)
	assert.Empty(t, got.FirstName)
	assert.Empty(t, got.LastName)
	assert.Empty(t, got.AvatarURL)
	assert.Empty(t, got.Gender)
	assert.False(t, got.ShowPhone)
	assert.False(t, got.DirectCall)
	// …sauf le klaxon, qui existait déjà.
	assert.True(t, got.InAppAlert)
}

// La livraison, telle qu'elle est aujourd'hui : nom, numéro, appel.
func TestDeliveryStillGetsTheNameAndTheNumber(t *testing.T) {
	got := discloseResponse(awa(), country.DefaultDisclosure(country.VerticalFood, country.ToAgent))

	assert.Equal(t, "Awa Diallo", got.Name)
	assert.Equal(t, "+22890000000", got.Phone)
	assert.True(t, got.ShowPhone)
	assert.True(t, got.DirectCall)
	assert.Empty(t, got.Gender, "le genre n'est servi nulle part")
	assert.Empty(t, got.AvatarURL, "ni la photo")
}

// ⚠️ UN BOUTON D'APPEL A BESOIN DU NUMÉRO. « Appeler sans voir » envoie donc le
// numéro avec `show_phone: false` : c'est la limite honnête du réglage, et elle
// doit être VISIBLE dans la réponse plutôt que découverte.
func TestCallingWithoutSeeingStillSendsTheNumber(t *testing.T) {
	d := country.DefaultDisclosure(country.VerticalFood, country.ToAgent)
	d.Phone, d.DirectCall = false, true

	got := discloseResponse(awa(), d)
	assert.Equal(t, "+22890000000", got.Phone, "sans numéro, aucun bouton ne compose")
	assert.False(t, got.ShowPhone, "mais il ne doit pas s'afficher")
	assert.True(t, got.DirectCall)
}

// Ni affiché ni composable : le numéro ne part pas du tout.
func TestWithNeitherDisplayNorCallTheNumberStaysHome(t *testing.T) {
	d := country.DefaultDisclosure(country.VerticalFood, country.ToAgent)
	d.Phone, d.DirectCall = false, false

	assert.Empty(t, discloseResponse(awa(), d).Phone)
}

// ⚠️ LE TROU QUI ANNULERAIT TOUT CE RÉGLAGE. L'inscription par code pose le
// NUMÉRO comme nom d'affichage tant que la personne n'en a pas donné un :
// servir ce « nom » avec le téléphone fermé donnerait le numéro quand même, à
// l'écran, sous un libellé qui ne le dit pas.
func TestAnOTPAccountsNumberNeverLeavesAsANameParseName(t *testing.T) {
	p := awa()
	p.Name, p.FirstName, p.LastName = p.Phone, "", "" // compte né par code, sans nom

	d := country.DefaultDisclosure(country.VerticalFood, country.ToAgent)
	d.Phone, d.DirectCall = false, false
	got := discloseResponse(p, d)
	assert.Empty(t, got.Name, "le numéro ne sort pas déguisé en nom")
	assert.Empty(t, got.Phone)

	// Et le niveau `first` est le pire piège : sur un numéro sans espace, il
	// rend le numéro ENTIER.
	d.Name = country.NameFirst
	assert.Empty(t, discloseResponse(p, d).Name)

	// En revanche, si le numéro est de toute façon divulgué, il n'y a plus rien
	// à protéger : on garde le nom tel quel plutôt que d'effacer un libellé.
	d = country.DefaultDisclosure(country.VerticalFood, country.ToAgent)
	assert.Equal(t, p.Phone, discloseResponse(p, d).Name)
}

// Le niveau du nom traverse bien jusqu'à la réponse.
func TestTheNameLevelReachesTheResponse(t *testing.T) {
	d := country.DefaultDisclosure(country.VerticalFood, country.ToAgent)
	d.Name = country.NameInitials
	assert.Equal(t, "A. D.", discloseResponse(awa(), d).Name)

	d.Name = country.NameFirst
	assert.Equal(t, "Awa", discloseResponse(awa(), d).Name)
}

// Ce qu'un marché peut OUVRIR, et qui n'est servi nulle part par défaut.
func TestAMarketCanOpenTheCivilNameThePhotoAndTheGender(t *testing.T) {
	d := country.DefaultDisclosure(country.VerticalVTC, country.ToClient)
	d.FirstName, d.LastName, d.Photo, d.Gender = true, true, true, true

	got := discloseResponse(awa(), d)
	assert.Equal(t, "Awa", got.FirstName)
	assert.Equal(t, "Diallo", got.LastName)
	assert.Equal(t, "https://files.dira.llc/a.jpg", got.AvatarURL)
	assert.Equal(t, "female", got.Gender)
}

// La note est une PERMISSION, pas une donnée : la verticale la détient.
func TestTheRatingIsAPermissionNotAValue(t *testing.T) {
	assert.True(t, discloseResponse(awa(),
		country.DefaultDisclosure(country.VerticalVTC, country.ToClient)).ShowRating)
	assert.False(t, discloseResponse(awa(),
		country.DefaultDisclosure(country.VerticalVTC, country.ToAgent)).ShowRating)
}
