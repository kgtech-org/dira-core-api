package marker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Les trois genres, dans l'ordre où la console et les applications les
// montrent — et rien d'autre : un genre inconnu n'est pas réglable.
//
// ⚠️ `stop` A DISPARU : un arrêt de course et l'adresse d'un client sont le
// même objet vu à deux moments. Les régler séparément obligeait à envoyer
// deux fois la même image pour que la carte reste cohérente.
func TestKinds(t *testing.T) {
	assert.Equal(t, []string{"courier", "client", "merchant"}, Kinds)
	for _, k := range Kinds {
		assert.True(t, validKind(k), k)
	}
	assert.False(t, validKind("stop"), "fusionné dans `client`")
	assert.False(t, validKind("driver"), "le chauffeur VTC est dessiné par son mode de véhicule")
	assert.False(t, validKind(""))
}

// Le client et le marchand portent des pins numérotés ; le livreur non — il
// n'y en a qu'un par course.
func TestOnlyWhatCanBeSeveralIsNumbered(t *testing.T) {
	assert.True(t, Numbered(KindClient))
	assert.True(t, Numbered(KindMerchant))
	assert.False(t, Numbered(KindCourier))
	assert.Equal(t, 4, MaxNumbered)
}

// ⚠️ LES PINS SONT SERVIS AU COMPLET, 1..4, même vides. Une liste trouée
// obligerait l'application à deviner si le rang 3 manque parce qu'il n'est
// pas réglé ou parce qu'il n'existe pas.
func TestNumberedPinsAreAlwaysServedInFull(t *testing.T) {
	m := fill(Marker{Kind: KindMerchant, Numbered: []Pin{{Index: 2, MapIconURL: "https://x/2.png"}}})
	require.Len(t, m.Numbered, 4)
	assert.Equal(t, 4, m.MaxNumbered)
	for i, p := range m.Numbered {
		assert.Equal(t, i+1, p.Index, "dans l'ordre, du premier au quatrième")
	}
	assert.Equal(t, "https://x/2.png", m.Numbered[1].MapIconURL)
	assert.Empty(t, m.Numbered[0].MapIconURL)
}

// Le livreur n'en porte pas, et n'en reçoit pas non plus une liste vide :
// un champ absent se lit « sans objet », une liste vide se lit « rien de
// réglé ».
func TestTheCourierCarriesNoNumberedPin(t *testing.T) {
	m := fill(Marker{Kind: KindCourier, Numbered: []Pin{{Index: 1, MapIconURL: "https://x/1.png"}}})
	assert.Nil(t, m.Numbered)
	assert.Zero(t, m.MaxNumbered)
}

// ⚠️ UN RANG HORS BORNES OU EN DOUBLE EST REFUSÉ, jamais ignoré. Un pin
// silencieusement écarté se remarque des semaines plus tard, quand quelqu'un
// s'étonne que la carte n'ait pas changé.
func TestABadIndexIsRefusedNotDropped(t *testing.T) {
	_, err := cleanPins(KindMerchant, []Pin{{Index: 0, MapIconURL: "https://x/a.png"}})
	assert.Error(t, err, "il n'y a pas de pin « 0 » : le premier point porte le chiffre 1")

	_, err = cleanPins(KindMerchant, []Pin{{Index: 5, MapIconURL: "https://x/a.png"}})
	assert.Error(t, err)

	_, err = cleanPins(KindMerchant, []Pin{
		{Index: 2, MapIconURL: "https://x/a.png"},
		{Index: 2, MapIconURL: "https://x/b.png"},
	})
	assert.Error(t, err, "deux images pour le même rang : laquelle ?")
}

// Un pin SANS image est écarté — c'est ainsi qu'on en retire un, et la
// console envoie toujours les quatre rangs.
func TestAnEmptyPinIsHowYouRemoveOne(t *testing.T) {
	out, err := cleanPins(KindClient, []Pin{
		{Index: 1, MapIconURL: "https://x/1.png"},
		{Index: 2, MapIconURL: ""},
		{Index: 3, MapIconURL: "https://x/3.png"},
		{Index: 4, MapIconURL: ""},
	})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, 1, out[0].Index)
	assert.Equal(t, 3, out[1].Index, "rangés par rang, quel que soit l'ordre d'envoi")
}

// Les pins arrivent parfois en désordre ; ils repartent triés.
func TestPinsAreStoredInOrder(t *testing.T) {
	out, err := cleanPins(KindClient, []Pin{
		{Index: 3, MapIconURL: "https://x/3.png"},
		{Index: 1, MapIconURL: "https://x/1.png"},
	})
	require.NoError(t, err)
	assert.Equal(t, []Pin{{Index: 1, MapIconURL: "https://x/1.png"}, {Index: 3, MapIconURL: "https://x/3.png"}}, out)
}

// Donner des pins numérotés au livreur est une erreur de l'appelant, pas
// quelque chose à avaler.
func TestNumberingWhatCannotBeNumberedIsRefused(t *testing.T) {
	_, err := cleanPins(KindCourier, []Pin{{Index: 1, MapIconURL: "https://x/1.png"}})
	assert.Error(t, err)
	// Mais ne rien envoyer reste valable : c'est le cas normal.
	out, err := cleanPins(KindCourier, nil)
	assert.NoError(t, err)
	assert.Nil(t, out)
}

// Tout retirer rend `nil` plutôt qu'une liste vide : rien à stocker.
func TestRemovingEveryPinStoresNothing(t *testing.T) {
	out, err := cleanPins(KindClient, []Pin{{Index: 1}, {Index: 2}, {Index: 3}, {Index: 4}})
	require.NoError(t, err)
	assert.Nil(t, out)
}
