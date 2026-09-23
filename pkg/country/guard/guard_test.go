package guard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func opts(global map[string]string) Options {
	return Options{Collections: []string{"orders"}, Global: global}
}

// Ce que l'analyseur doit voir, et ce qu'il doit laisser passer.
//
// ⚠️ LE CAS QUI COMPTE est `ByDay` : une agrégation par jour porte un `_id`
// dans son `$group`, et c'est ce `_id` qui faisait passer le bandeau du
// tableau de bord pour borné alors qu'il balayait la plateforme entière.
func TestItSeesTheUnboundedReadsAndOnlyThose(t *testing.T) {
	found, err := Scan("testdata", opts(nil))
	require.NoError(t, err)

	names := make([]string, 0, len(found))
	for _, f := range found {
		names = append(names, f.Func)
	}
	assert.Equal(t, []string{"ByDay", "All", "Page"}, names,
		"bornées : par le pays, par un identifiant, par une fonction voisine, ou par l'appelant")
	assert.Equal(t, "shop", found[0].Package)
	assert.Equal(t, "orders", found[0].Collection)
	assert.Contains(t, found[0].String(), "sans pays")
}

// Une collection absente du périmètre n'est pas relue : l'analyseur ne
// devine pas ce qui porte un pays, le service le déclare.
func TestOnlyTheDeclaredCollectionsAreWatched(t *testing.T) {
	found, err := Scan("testdata", Options{Collections: []string{"markers"}})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "Markers", found[0].Func)
}

// Une lecture déclarée globale sort de la liste — un balayage de fond n'a pas
// de requête, donc pas de pays.
func TestADeclaredGlobalReadIsAccepted(t *testing.T) {
	found, err := Scan("testdata", opts(map[string]string{
		"shop.ByDay": "exemple",
		"shop.All":   "exemple",
		"shop.Page":  "exemple",
	}))
	require.NoError(t, err)
	assert.Empty(t, found)
}

// Une exemption qui ne correspond plus à rien est signalée : sans cela elle
// survivrait à la borne qu'on vient de poser, et couvrirait la prochaine
// lecture du même nom.
func TestAStaleExemptionIsReported(t *testing.T) {
	stale, err := Stale("testdata", opts(map[string]string{
		"shop.ByDay":      "exemple",
		"shop.All":        "exemple",
		"shop.Page":       "exemple",
		"shop.ListOrders": "bornée depuis",
		"shop.Disparue":   "n'existe plus",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop.Disparue", "shop.ListOrders"}, stale)
}

// Un nom de champ banal se qualifie par son paquet : plusieurs dépôts
// appellent `col` leur unique collection, et une seule porte un pays.
func TestACollectionCanBeNamedWithItsPackage(t *testing.T) {
	found, err := Scan("testdata", Options{Collections: []string{"shop.markers"}})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "Markers", found[0].Func)

	none, err := Scan("testdata", Options{Collections: []string{"autre.markers"}})
	require.NoError(t, err)
	assert.Empty(t, none)
}
