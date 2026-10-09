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
	// UN CHAUFFEUR VTC N'EST JAMAIS LIVREUR : la verticale RÉCLAME
	// l'appartenance métier d'un compte au moment où elle garantit son
	// profil. La première gagne, l'autre est refusée — et c'est ce qui
	// empêche une personne d'être chauffeur et livreuse à la fois.
	"/internal/accounts/agent-app",
	"/internal/accounts/by-phone",
	"/internal/accounts/contact",
	// CE QU'UNE VERTICALE A LE DROIT DE MONTRER d'une personne à l'autre : le
	// nom au niveau choisi, et seulement les champs que le pays a ouverts pour
	// ce métier et ce public.
	//
	// ⚠️ ELLE N'ÉLARGIT PAS LA SURFACE, ELLE LA RESSERRE. `/contact` rend le
	// nom et le téléphone sans filtre — c'est la vérité du back-office. Celle-
	// ci rend MOINS, et c'est elle qu'une verticale doit appeler pour montrer
	// quelqu'un à quelqu'un d'autre : un champ qu'on n'envoie pas ne fuit pas.
	"/internal/accounts/disclose",
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
	// ⚠️ LE CROCHET DE LA SUPERVISION, et non d'une verticale. Alertmanager
	// l'appelle pour que les alertes machine arrivent là où le staff regarde
	// déjà. Elle NE LIT RIEN et n'écrit rien en base : elle relaie un texte,
	// borné, vers des notifications.
	"/internal/alerts",
	"/internal/audit",
	"/internal/backoffice/payments",
	"/internal/backoffice/refund-order-payment",
	"/internal/backoffice/token-transactions",
	"/internal/backoffice/wallets",
	// Les PAYS OUVERTS, pour une verticale qui veut les afficher ou les
	// vérifier sans tenir sa propre liste. Le catalogue et les frontières,
	// eux, sont dans `pkg/country`, partagés par le code.
	// LES OBJECTIFS À ATTEINDRE (`internal/challenge`).
	//
	// ⚠️ DEUX ROUTES, ET ELLES VONT DANS LE MÊME SENS — la verticale POUSSE ce
	// qu'elle a compté, puis TIRE ce qu'on lui doit. Le socle ne peut pas
	// appeler une verticale ; un chauffeur VTC n'ayant pas de portefeuille ici,
	// son bonus reste DÛ jusqu'à ce que les courses viennent le chercher. C'est
	// le chemin des cautions de matériel, et pour la même raison.
	"/internal/challenges/owed",
	"/internal/challenges/report",
	"/internal/countries",
	// Le MATÉRIEL loué ou vendu aux agents : ce qu'un gain doit rendre au
	// contrat, et si la personne est bloquée par un retard.
	"/internal/equipment/collect",
	"/internal/equipment/standing",
	// OUBLIER DES FICHIERS — la part de l'effacement d'un compte que seule une
	// verticale sait nommer et que seul le socle sait exécuter : le bucket
	// n'est ouvert qu'ici. Une photo de permis ou de carte d'identité vit dans
	// une collection de conformité que le socle ne connaît pas.
	//
	// ⚠️ CE N'EST PAS UN POUVOIR DE SUPPRESSION GÉNÉRALE : une URL étrangère
	// au bucket de la plateforme est ignorée en silence.
	"/internal/files/forget",
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
	// LA FLOTTE D'UN PARTENAIRE — ce qu'une verticale demande pour borner les
	// listes de sa console partenaire.
	//
	// ⚠️ DEMANDÉE À CHAQUE REQUÊTE plutôt que mise dans le jeton : une
	// revendication figée aurait laissé un partenaire détaché continuer à gérer
	// sa flotte jusqu'à l'expiration de son jeton — et sa suspension aurait été
	// sans effet jusque-là.
	"/internal/fleets/of-owner",
	"/internal/notifications/send",
	"/internal/notifications/staff",
	"/internal/payments/initiate",
	// LES CODES PROMO de la plateforme — campagnes, influenceurs, parrainage.
	//
	// ⚠️ QUATRE ROUTES ET PAS UNE, parce qu'une remise a quatre moments : la
	// CHIFFRER (sans rien consommer), la RÉSERVER (l'opération est commandée),
	// la RÉGLER (l'argent est sorti) et la RENDRE (annulation). Une seule
	// route qui « applique » aurait consommé l'enveloppe au devis — et le code
	// aurait été épuisé par des gens qui regardaient le prix.
	//
	// ⚠️ ET C'EST LE SOCLE QUI LES TIENT, pas chaque verticale : un code doit
	// être unique partout et porter UNE seule enveloppe, puisque celui d'un
	// influenceur vaut sur une course ET sur une commande.
	"/internal/promo-codes/quote",
	"/internal/promo-codes/redeem",
	"/internal/promo-codes/release",
	"/internal/promo-codes/settle",
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
