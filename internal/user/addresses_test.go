package user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addrFixture(t *testing.T) (*Service, string) {
	t.Helper()
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), RegisterRequest{
		Phone: "+22890000111", Password: "motdepasse1", Name: "Awa", Role: "client",
	})
	require.NoError(t, err)
	return svc, reg.User.ID
}

// La PREMIÈRE adresse est celle par défaut sans qu'on le demande : un carnet
// d'une seule adresse dont aucune n'est présélectionnée n'aurait aucun sens.
func TestFirstAddressBecomesTheDefault(t *testing.T) {
	svc, uid := addrFixture(t)
	a, err := svc.SaveAddress(context.Background(), uid, "", AddressRequest{
		Label: "Maison", Address: "Rue 12, Hédzranawoé", Geo: [2]float64{1.22, 6.13},
		Details: "portail bleu",
	})
	require.NoError(t, err)
	assert.True(t, a.IsDefault)
	assert.Equal(t, "portail bleu", a.Details)
}

// Une seule adresse par défaut : poser la nouvelle retire le drapeau de
// l'ancienne, sinon l'application en présélectionnerait une au hasard.
func TestOnlyOneDefaultAddress(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()
	_, err := svc.SaveAddress(ctx, uid, "", AddressRequest{Label: "Maison", Address: "Rue 12"})
	require.NoError(t, err)
	second, err := svc.SaveAddress(ctx, uid, "", AddressRequest{Label: "Bureau", Address: "Bd 13", IsDefault: true})
	require.NoError(t, err)

	list, err := svc.ListAddresses(ctx, uid)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, second.ID, list[0].ID, "l'adresse par défaut sort en tête")
	assert.True(t, list[0].IsDefault)
	assert.False(t, list[1].IsDefault)
}

// Supprimer l'adresse par défaut en promeut une autre : laisser un carnet non
// vide sans présélection ferait retomber le client sur une saisie manuelle
// alors qu'il a des adresses enregistrées.
func TestDeletingTheDefaultPromotesAnother(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()
	first, err := svc.SaveAddress(ctx, uid, "", AddressRequest{Label: "Maison", Address: "Rue 12"})
	require.NoError(t, err)
	_, err = svc.SaveAddress(ctx, uid, "", AddressRequest{Label: "Bureau", Address: "Bd 13"})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteAddress(ctx, uid, first.ID))
	list, err := svc.ListAddresses(ctx, uid)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.True(t, list[0].IsDefault, "la survivante devient celle par défaut")
}

// Le carnet d'autrui n'est ni lisible ni modifiable : le filtre porte sur le
// compte ET l'identifiant, dans la requête.
func TestAnotherAccountsAddressIsInvisible(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()
	mine, err := svc.SaveAddress(ctx, uid, "", AddressRequest{Label: "Maison", Address: "Rue 12"})
	require.NoError(t, err)

	other, err := svc.Register(ctx, RegisterRequest{
		Phone: "+22890000222", Password: "motdepasse1", Name: "Kofi", Role: "client",
	})
	require.NoError(t, err)

	err = svc.DeleteAddress(ctx, other.User.ID, mine.ID)
	assertUserCode(t, err, "address_not_found")

	list, err := svc.ListAddresses(ctx, other.User.ID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// --- préférences ---

// L'ABSENCE vaut OUI : un compte qui n'a jamais touché à ces réglages doit
// continuer de recevoir ce qu'il recevait.
func TestUnsetPreferencesAllowEverything(t *testing.T) {
	var p *Preferences
	assert.True(t, p.Allows(CategoryOrderUpdates))
	assert.True(t, (&Preferences{}).Allows(CategoryChatMessages))
	assert.True(t, (&Preferences{}).Allows("catégorie inconnue"))
}

// Couper une catégorie n'en coupe pas une autre : un client qui refuse les
// promotions doit continuer de savoir que son livreur est en bas.
func TestCuttingOneCategoryLeavesTheOthers(t *testing.T) {
	svc, uid := addrFixture(t)
	no := false
	_, err := svc.UpdatePreferences(context.Background(), uid, UpdatePreferencesRequest{Promotions: &no})
	require.NoError(t, err)

	locale, allowed := svc.NotificationPrefs(context.Background(), uid, CategoryPromotions)
	assert.False(t, allowed)
	_, allowed = svc.NotificationPrefs(context.Background(), uid, CategoryOrderUpdates)
	assert.True(t, allowed, "les mises à jour de commande restent")
	assert.Empty(t, locale)
}

// Les préférences se FUSIONNENT : l'écran envoie l'interrupteur qu'on vient de
// basculer, pas les six autres. Remplacer remettrait le reste à zéro.
func TestPreferencesMergeRatherThanReplace(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()
	no := false
	_, err := svc.UpdatePreferences(ctx, uid, UpdatePreferencesRequest{Promotions: &no})
	require.NoError(t, err)

	dark := ThemeDark
	resp, err := svc.UpdatePreferences(ctx, uid, UpdatePreferencesRequest{Theme: &dark})
	require.NoError(t, err)
	require.NotNil(t, resp.Preferences)
	assert.Equal(t, ThemeDark, resp.Preferences.Theme)
	require.NotNil(t, resp.Preferences.Promotions)
	assert.False(t, *resp.Preferences.Promotions, "le réglage précédent survit")
}

// La langue choisie dans l'application est normalisée : `fr-FR` et `fr`
// désignent la même chose.
func TestChosenLocaleIsNormalized(t *testing.T) {
	svc, uid := addrFixture(t)
	loc := "fr-FR"
	_, err := svc.UpdatePreferences(context.Background(), uid, UpdatePreferencesRequest{Locale: &loc})
	require.NoError(t, err)
	got, _ := svc.NotificationPrefs(context.Background(), uid, CategoryOrderUpdates)
	assert.Equal(t, "fr", got)
}

// Tout ce que le formulaire de profil modifie doit SURVIVRE à une relecture.
//
// C'est le test qui manquait : `avatar_url` était posé en mémoire, rendu dans
// la réponse, et jamais écrit. L'utilisateur voyait sa photo changer, puis
// revenir au rechargement suivant.
func TestProfileFieldsSurviveAReRead(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()

	avatar := "https://cdn.dira.llc/a/awa.jpg"
	first, last := "Awa", "Ndiaye"
	birth := "1994-03-17"
	gender := GenderFemale
	_, err := svc.UpdateProfile(ctx, uid, UpdateMeRequest{
		AvatarURL: &avatar, FirstName: &first, LastName: &last,
		BirthDate: &birth, Gender: &gender,
	})
	require.NoError(t, err)

	again, err := svc.Me(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, avatar, again.AvatarURL, "la photo doit être écrite, pas seulement rendue")
	assert.Equal(t, "Awa", again.FirstName)
	assert.Equal(t, "Ndiaye", again.LastName)
	assert.Equal(t, "1994-03-17", again.BirthDate)
	assert.Equal(t, GenderFemale, again.Gender)
}

// Une date de naissance dans le futur est refusée, et une date mal formée
// aussi : mieux vaut un refus qu'un champ silencieusement ignoré.
func TestBirthDateIsValidated(t *testing.T) {
	svc, uid := addrFixture(t)
	ctx := context.Background()

	bad := "17/03/1994"
	_, err := svc.UpdateProfile(ctx, uid, UpdateMeRequest{BirthDate: &bad})
	assertUserCode(t, err, "validation_failed")

	future := "2999-01-01"
	_, err = svc.UpdateProfile(ctx, uid, UpdateMeRequest{BirthDate: &future})
	assertUserCode(t, err, "validation_failed")
}
