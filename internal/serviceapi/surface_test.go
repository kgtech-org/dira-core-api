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
	// La RECHERCHE de comptes par nom ou téléphone : des identifiants
	// seulement, pour qu'une verticale filtre ses courses ou ses commandes
	// sur « Kossi » ou « 90 20 » — sa liste ne porte que des identifiants.
	"/internal/accounts/search",
	// LE JOURNAL D'AUDIT UNIQUE : une verticale y range ses entrées, avec le
	// service qui les a écrites ; la console les lit toutes au socle.
	"/internal/audit",
	"/internal/backoffice/payments",
	"/internal/backoffice/refund-order-payment",
	"/internal/backoffice/token-transactions",
	"/internal/backoffice/wallets",
	// Les PAYS OUVERTS, pour une verticale qui veut les afficher ou les
	// vérifier sans tenir sa propre liste. Le catalogue et les frontières,
	// eux, sont dans `pkg/country`, partagés par le code.
	"/internal/countries",
	// Le MATÉRIEL loué ou vendu aux agents : ce qu'un gain doit rendre au
	// contrat, et si la personne est bloquée par un retard.
	"/internal/equipment/collect",
	"/internal/equipment/standing",
	// LA FACTURATION par pays (jetons ou commission, quand débiter le
	// client) que chaque verticale applique, et le JOURNAL comptable où
	// elle déclare ce qui bouge chez elle (le grand livre des chauffeurs).
	"/internal/finance/billing",
	"/internal/finance/events",
	// Les NOMS des flottes privées — et rien d'autre. Une verticale affiche
	// « Flotte Sodigaz » à côté d'une plaque ; lui ouvrir la fiche entière lui
	// confierait un contrat et une commission qu'elle n'a aucune raison de
	// porter, et qu'elle finirait par recopier chez elle.
	"/internal/fleets/names",
	"/internal/notifications/send",
	"/internal/notifications/staff",
	"/internal/payments/initiate",
	// Le SIGNAL d'un appel : réveiller l'application d'un chauffeur dont le
	// socket est mort, par FCM, sans notification système.
	"/internal/push/data",
	"/internal/ratings",
	// Le SOLDE d'un portefeuille : ce que le VTC lit avant d'appeler des
	// chauffeurs pour une course payée sur le solde — débitée à
	// l'acceptation, mais jamais lancée sans de quoi payer.
	"/internal/wallets/balance",
	"/internal/wallets/consume",
	"/internal/wallets/create",
	"/internal/wallets/credit",
	"/internal/wallets/credit-earnings",
	// Ce qu'un solde n'a pas forcément : pris s'il y a, porté à la DETTE
	// sinon — la commission d'une course en espèces, un paiement refusé.
	"/internal/wallets/owe",
	"/internal/wallets/pay",
	"/internal/wallets/refund",
	// ⚠️ DE LA MONNAIE CRÉÉE, comme `credit` crée des jetons : recharger un
	// portefeuille en ARGENT sans qu'un prestataire ait encaissé. Pour les
	// jeux de démonstration et le provisionnement d'exploitation ; gardée
	// par le seul secret de service, et jamais ouverte aux applications.
	"/internal/wallets/topup",
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
