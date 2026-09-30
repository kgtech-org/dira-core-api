package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// LE FOND D'UN PAYS EST UN SEUL FAIT : `dira` ou `google`.
//
// ⚠️ Et le serveur ne garde AUCUNE clé (v4.40.0) : il en a gardé une par pays
// et par plateforme pendant une semaine, pour la faire tourner sans republier
// les applications. Chaque frontend porte déjà la sienne, restreinte à son
// empreinte — la clé du serveur arrivait trop tard, pour quelqu'un qui n'en
// avait pas besoin.

// Un pays qui n'a rien réglé sert NOTRE fond : celui qui ne coûte rien et ne
// dépend de personne.
func TestWithoutAnySettingWeServeOurOwnBasemap(t *testing.T) {
	assert.Equal(t, BasemapDira, basemapOf(Maps{}))
	assert.Equal(t, BasemapDira, mapsResponse(Maps{}).Basemap)
}

// Le réglage du pays se lit tel quel, et une valeur qu'on ne connaît pas
// retombe sur le nôtre.
//
// ⚠️ LA CONSOLE ET LES APPLICATIONS LISENT LA MÊME FONCTION. Deux chemins de
// décision auraient fini par répondre deux choses à la même question — et c'est
// exactement ce qui fait chercher une heure pourquoi « le réglage n'a pas
// d'effet ».
func TestTheCountrysChoiceIsServedAsIs(t *testing.T) {
	assert.Equal(t, BasemapGoogle, basemapOf(Maps{Basemap: BasemapGoogle}))
	assert.Equal(t, BasemapGoogle, mapsResponse(Maps{Basemap: BasemapGoogle}).Basemap)
	assert.Equal(t, BasemapDira, basemapOf(Maps{Basemap: "openstreetmap"}),
		"une valeur inconnue en base ne doit pas sortir telle quelle")
}

// Un réglage vide est refusé plutôt qu'accepté sans rien faire : un écran qui
// dit « enregistré » sans avoir rien changé est pire qu'un refus.
func TestAnEmptyUpdateIsRefused(t *testing.T) {
	assert.Equal(t, "validation_failed", errNoMapsUpdate.Code)
	assert.Contains(t, errNoMapsUpdate.Message, "basemap")
}

// ⚠️ LE PAYS PAR DÉFAUT NE PEUT PAS ÊTRE UN PAYS D'ESSAI. C'est celui qu'une
// requête sans en-tête reçoit, donc celui où atterrit le trafic réel : l'y
// marquer ferait disparaître la moitié de la plateforme des tableaux de bord et
// des rapports, en silence. Le refus a un code nommé et ses deux traductions.
func TestTheDefaultCountryCannotBeReservedForTesting(t *testing.T) {
	assert.Equal(t, "country_default_testing", errDefaultTesting.Code)
	assert.NotEmpty(t, errDefaultTesting.Message)
}

// ⚠️ AU MIEUX DANS LE SENS QUI PROTÈGE LES CHIFFRES. Un cache vide répond
// « ce pays n'est pas un pays d'essai », donc il COMPTE. Se tromper ainsi fait
// apparaître des courses d'essai dans un tableau, ce qui se voit ; se tromper
// dans l'autre sens ferait DISPARAÎTRE de vraies courses des rapports, et
// personne ne cherche ce qu'il ne voit pas manquer.
func TestAnEmptyCacheMakesACountryCountRatherThanVanish(t *testing.T) {
	var s Service
	assert.False(t, s.Testing("GA"))
	assert.Empty(t, s.TestingCodes())
}

// LE FOND PART AUSSI DANS LE CATALOGUE PUBLIC (v4.41.0).
//
// ⚠️ `maps` de l'authentification ne suffit pas : il voyage avec le jeton, donc
// il dit le fond du pays où l'on s'est CONNECTÉ, une fois. Une application qui
// change de pays sans se reconnecter — ou qui rouvre après un simple `GET /me` —
// garderait le fond de la veille. Le catalogue, lui, se relit à chaque
// ouverture.
func TestThePublicCatalogueCarriesTheCountrysBasemap(t *testing.T) {
	var s Service
	// Cache vide (base injoignable au démarrage) : NOTRE fond, celui qui ne
	// demande ni clé ni facture. Se tromper vers `google` donnerait une carte
	// grise à qui n'a pas de clé.
	assert.Equal(t, BasemapDira, s.BasemapOf("SN"))

	s.basemap = map[string]string{"SN": BasemapGoogle}
	assert.Equal(t, BasemapGoogle, s.BasemapOf("SN"))
	assert.Equal(t, BasemapGoogle, s.BasemapOf("sn"), "le code se normalise")
	assert.Equal(t, BasemapDira, s.BasemapOf("TG"), "un pays non réglé garde le nôtre")

	// Une valeur abîmée en base ne sort pas telle quelle : seul `google` est
	// un fond, tout le reste est le nôtre.
	s.basemap = map[string]string{"TD": "openstreetmap"}
	assert.Equal(t, BasemapDira, s.BasemapOf("TD"))
}
