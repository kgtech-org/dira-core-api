package serviceapi_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredSurface est la LISTE COMPLÈTE des routes de service du socle.
//
// Toute route `/internal/...` porte les pouvoirs d'une verticale : débiter un
// portefeuille, ouvrir un compte, lire le grand livre. Ce test existe pour que
// cette liste reste la vérité — en ajouter une sans l'écrire ici fait échouer
// la construction.
//
// ⚠️ Le paquet `serviceapi` promet une surface qu'on peut lire d'un seul
// fichier. Cette promesse ne tient pas toute seule : `rating` monte sa propre
// route de dépôt, à côté de ses lectures publiques. Plutôt que de déplacer une
// route bien commentée là où elle se comprend moins bien, ce test rend la
// promesse VÉRIFIABLE où que la route soit déclarée.
var declaredSurface = []string{
	"/internal/accounts/by-phone",
	"/internal/accounts/contact",
	"/internal/accounts/ensure",
	"/internal/accounts/ensure-merchant",
	"/internal/accounts/get",
	"/internal/accounts/names",
	"/internal/accounts/rows",
	"/internal/backoffice/payments",
	"/internal/backoffice/token-transactions",
	"/internal/backoffice/wallets",
	"/internal/notifications/send",
	"/internal/payments/initiate",
	"/internal/ratings",
	"/internal/wallets/consume",
	"/internal/wallets/create",
	"/internal/wallets/credit",
	"/internal/wallets/credit-earnings",
	"/internal/wallets/pay-order",
	"/internal/wallets/refund-order",
}

var internalRoute = regexp.MustCompile(`"(/internal/[a-z0-9/{}-]*)"`)

// TestServiceSurfaceIsDeclared parcourt les SOURCES plutôt que le routeur :
// monter le routeur demanderait une base de données, et un test qu'on ne peut
// pas lancer sans infrastructure est un test qu'on finit par ignorer.
func TestServiceSurfaceIsDeclared(t *testing.T) {
	found := map[string]string{} // route -> fichier où elle est déclarée

	for _, root := range []string{"../", "../../cmd"} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range internalRoute.FindAllStringSubmatch(string(body), -1) {
				found[m[1]] = path
			}
			return nil
		})
		require.NoError(t, err)
	}

	// Un balayage qui ne trouve RIEN signale un chemin cassé, pas un socle
	// sans surface de service. Sans cette ligne, le test passerait au vert le
	// jour où il cesse de regarder au bon endroit.
	require.NotEmpty(t, found, "aucune route de service trouvée : le balayage regarde-t-il au bon endroit ?")

	actual := make([]string, 0, len(found))
	for route := range found {
		actual = append(actual, route)
	}
	sort.Strings(actual)

	assert.Equal(t, declaredSurface, actual,
		"la surface de service a changé : toute route /internal/... doit être "+
			"inscrite dans declaredSurface, pour qu'on puisse lire d'un coup ce "+
			"qu'une verticale a le droit de faire")
}
