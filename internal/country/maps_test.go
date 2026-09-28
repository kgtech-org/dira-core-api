package country

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const key = "AIzaSyDEMO-cle-de-test-0000-ABCD"

// Un pays qui n'a rien réglé sert NOTRE fond : c'est celui qui ne coûte rien et
// ne dépend de personne.
func TestWithoutAnySettingWeServeOurOwnBasemap(t *testing.T) {
	base, k, mapType := resolveGrant(Maps{}, PlatformAndroid)
	assert.Equal(t, BasemapDira, base)
	assert.Empty(t, k)
	assert.Empty(t, mapType)
}

// La clé de la plateforme qui demande, et elle seule.
func TestEachPlatformGetsItsOwnKey(t *testing.T) {
	m := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{
		PlatformAndroid: {Key: key, MapType: MapTypeSatellite},
	}}
	base, k, mapType := resolveGrant(m, PlatformAndroid)
	assert.Equal(t, BasemapGoogle, base)
	assert.Equal(t, key, k)
	assert.Equal(t, MapTypeSatellite, mapType)
}

// ⚠️ PAS DE CLÉ POUR CETTE PLATEFORME → ON REVIENT À NOTRE FOND. Annoncer
// `google` sans clé donne un rectangle gris, et la personne croit l'application
// cassée — alors que le réglage du pays est parfaitement valide pour Android.
func TestAPlatformWithoutAKeyFallsBackInsteadOfShowingGrey(t *testing.T) {
	m := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{
		PlatformAndroid: {Key: key},
	}}
	base, k, _ := resolveGrant(m, PlatformWeb)
	assert.Equal(t, BasemapDira, base, "le web n'a pas de clé : il garde notre fond")
	assert.Empty(t, k)

	// Et Android, lui, continue de recevoir la sienne.
	base, k, _ = resolveGrant(m, PlatformAndroid)
	assert.Equal(t, BasemapGoogle, base)
	assert.Equal(t, key, k)
}

// Une application qui ne dit pas sur quoi elle tourne ne reçoit aucune clé —
// servir celle du web à une application Android donnerait une carte grise et une
// facture pour rien.
func TestAnUnnamedPlatformGetsNothing(t *testing.T) {
	m := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{PlatformWeb: {Key: key}}}
	for _, platform := range []string{"", "windows", "WEB", "android-tv"} {
		base, k, _ := resolveGrant(m, platform)
		assert.Equal(t, BasemapDira, base, "plateforme %q", platform)
		assert.Empty(t, k, "plateforme %q", platform)
	}
}

// ⚠️ LA CLÉ EST SERVIE MÊME QUAND LE DÉFAUT DU PAYS EST LE NÔTRE. C'est ce qui
// permet à la personne de CHOISIR Google : sans la clé, le choix serait affiché
// et ne marcherait pas. Le réglage du pays dit ce qu'on montre d'abord, pas ce
// qu'on autorise.
func TestTheKeyTravelsEvenWhenOurBasemapIsTheDefault(t *testing.T) {
	m := Maps{Basemap: BasemapDira, Google: map[string]GoogleMaps{PlatformIOS: {Key: key}}}
	base, k, mapType := resolveGrant(m, PlatformIOS)
	assert.Equal(t, BasemapDira, base, "c'est le défaut du pays")
	assert.Equal(t, key, k, "mais le choix doit pouvoir être offert")
	assert.Equal(t, MapTypeRoadmap, mapType, "une vue par défaut, sinon rien ne s'affiche")
}

// ⚠️ LA CONSOLE NE VOIT JAMAIS LA CLÉ. Une clé qu'un écran d'administration
// réaffiche finit dans une capture d'écran, un ticket, un canal de discussion.
func TestTheConsoleNeverSeesTheKey(t *testing.T) {
	m := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{
		PlatformWeb: {Key: key, MapType: MapTypeTerrain, UpdatedAt: time.Now().UTC()},
	}}
	out := mapsResponse(m)

	require.Contains(t, out.Google, PlatformWeb)
	web := out.Google[PlatformWeb]
	assert.True(t, web.Configured)
	assert.Equal(t, MapTypeTerrain, web.MapType)
	require.NotNil(t, web.UpdatedAt, "« depuis quand » se lit sans la clé")
	assert.False(t, web.UpdatedAt.IsZero())

	// La fin suffit à reconnaître laquelle est en place, pas à s'en servir.
	assert.Equal(t, "…ABCD", web.Hint)
	assert.NotContains(t, web.Hint, "AIzaSy")
	assert.Less(t, len(web.Hint), 12)
}

// Les trois plateformes sont toujours nommées, même vides : la console dessine
// trois cases, pas « celles qui existent déjà ».
func TestTheConsoleAlwaysSeesTheThreePlatforms(t *testing.T) {
	out := mapsResponse(Maps{})
	assert.Equal(t, BasemapDira, out.Basemap)
	for _, platform := range []string{PlatformWeb, PlatformAndroid, PlatformIOS} {
		require.Contains(t, out.Google, platform)
		assert.False(t, out.Google[platform].Configured)
		assert.Empty(t, out.Google[platform].Hint)
		// ⚠️ PAS DE DATE POUR UNE PLATEFORME SANS CLÉ. Une date de l'an 1 se
		// lit comme une vraie date, et la console l'afficherait.
		assert.Nil(t, out.Google[platform].UpdatedAt)
	}
}

// ⚠️ RIEN POUR UNE CLÉ TROP COURTE. Quatre caractères sur une clé de six en
// diraient les deux tiers ; une clé si courte est de toute façon une faute de
// saisie, et la console dira seulement « configurée ».
func TestAShortKeyGivesNoHintAtAll(t *testing.T) {
	out := mapsResponse(Maps{Google: map[string]GoogleMaps{PlatformWeb: {Key: "abc123"}}})
	assert.True(t, out.Google[PlatformWeb].Configured)
	assert.Empty(t, out.Google[PlatformWeb].Hint)
}

// La garde du rectangle gris, vue de l'écriture : on ne met pas un pays sur
// Google sans qu'une seule plateforme puisse l'afficher.
func TestACountryNeedsAtLeastOneKeyBeforeSwitching(t *testing.T) {
	assert.False(t, hasAnyKey(Maps{}))
	assert.False(t, hasAnyKey(Maps{Google: map[string]GoogleMaps{PlatformWeb: {Key: ""}}}))
	assert.True(t, hasAnyKey(Maps{Google: map[string]GoogleMaps{PlatformIOS: {Key: key}}}))
}

// Le refus a bien un code nommé et un message : sans eux, la console afficherait
// « une erreur est survenue » devant un réglage qu'on croit avoir enregistré.
func TestRefusingGoogleWithoutAKeyIsNamed(t *testing.T) {
	assert.Equal(t, "maps_google_without_key", errGoogleWithoutKey.Code)
	assert.NotEmpty(t, errGoogleWithoutKey.Message)
	assert.True(t, strings.Contains(strings.ToLower(errGoogleWithoutKey.Message), "key"))
}

// ⚠️ RETIRER LA DERNIÈRE CLÉ DOIT RAMENER LE PAYS À NOTRE FOND. La garde de
// lecture empêche déjà le rectangle gris ; ce qu'elle n'empêche pas, c'est que
// l'écran de réglage affiche « GOOGLE » alors que personne ne l'obtient — et on
// cherche alors pendant une heure pourquoi le fond ne change pas. Un écran
// d'administration qui affirme une chose fausse est pire qu'un écran vide.
//
// Trouvé en éprouvant le réglage en direct : le pays est resté sur `google`
// après le retrait, et seule la lecture rattrapait.
func TestRemovingTheLastKeyMustBringTheCountryBack(t *testing.T) {
	// L'état qu'on ne veut plus voir : réglé sur Google, sans aucune clé.
	orphan := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{}}
	assert.False(t, hasAnyKey(orphan), "c'est la condition du rattrapage")

	// Ce que la lecture en fait de toute façon — la carte, elle, est sauve.
	base, k, _ := resolveGrant(orphan, PlatformWeb)
	assert.Equal(t, BasemapDira, base)
	assert.Empty(t, k)

	// Et tant qu'une clé reste, on ne touche à rien : retirer celle du web ne
	// doit pas éteindre Google pour Android.
	kept := Maps{Basemap: BasemapGoogle, Google: map[string]GoogleMaps{PlatformAndroid: {Key: key}}}
	assert.True(t, hasAnyKey(kept))
	base, k, _ = resolveGrant(kept, PlatformAndroid)
	assert.Equal(t, BasemapGoogle, base)
	assert.Equal(t, key, k)
}
