// Package boundary ne contient qu'une vérification : la FRONTIÈRE PAYS du
// socle tient dans les dépôts, et pas seulement dans les intentions.
//
// ⚠️ ELLE EXISTE PARCE QU'UN OUBLI NE SE VOIT PAS. Une lecture sans
// `country.Restrict` répond normalement — avec les données de tous les pays.
// La console de Dakar affichait ainsi l'activité de Lomé, et rien dans la
// réponse ne le disait.
package boundary

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/country/guard"
)

// Les collections du socle dont les documents portent un `country`.
var collections = []string{
	"users",       // internal/user
	"wallets",     // internal/token
	"campaigns",   // internal/notify
	"fleets",      // internal/fleet
	"items",       // internal/equipment — le matériel
	"contracts",   // internal/equipment — ventes, locations, prêts
	"entries",     // internal/finance — le journal comptable
	"tickets",     // pkg/support
	"uses",        // pkg/promo — le grand livre des promotions
	"payment.col", // internal/payment
	"audit.col",   // pkg/audit — le journal des actions
}

// Les lectures légitimement SANS pays, et pourquoi. Chacune se relit à
// l'ajout : c'est la seule porte de sortie, et elle est nominative.
var global = map[string]string{
	// L'identité : on cherche quelqu'un AVANT de savoir d'où il est. La
	// connexion d'un Sénégalais depuis un déploiement togolais doit le
	// trouver, sans quoi il ne pourrait plus se connecter du tout.
	"user.FindByPhone": "la connexion cherche un compte avant de connaître son pays",
	"user.FindByEmail": "la connexion cherche un compte avant de connaître son pays",
	// Le rappel d'un prestataire de paiement arrive SANS requête de console :
	// il porte une référence, pas un pays.
	"payment.FindByProviderRef": "un rappel de prestataire n'a que sa référence",
	// Les balayages de fond n'ont pas de requête, donc pas de pays :
	// `country.Restrict` y serait sans effet, et l'écrire ferait croire à une
	// borne qui n'existe pas.
	"notify.DueCampaigns":       "l'ordonnanceur balaye les campagnes de tous les pays",
	"finance.Wallets":           "le balayage d'intégrité compare TOUTE la base à son journal",
	"finance.FirstEntryAt":      "le balayage d'intégrité cherche le début du journal",
	"finance.UnbalancedEntries": "le balayage d'intégrité relit toutes les écritures",
	"equipment.ActiveContracts": "le prélèvement automatique passe sur tous les contrats dus",
}

// Toute lecture d'une collection bornée par pays porte son pays, ou un
// identifiant — sinon elle est ici, avec sa raison.
func TestEveryCountryBoundedReadCarriesItsCountry(t *testing.T) {
	found, err := guard.Scan("../..", guard.Options{Collections: collections, Global: global})
	require.NoError(t, err)
	if len(found) == 0 {
		return
	}
	lines := make([]string, 0, len(found))
	for _, f := range found {
		lines = append(lines, "  "+f.String()+"  (clé : "+f.Key()+")")
	}
	assert.Fail(t, "des lectures ne portent pas leur pays",
		"Ajoutez `country.Restrict(ctx, …)` au filtre, ou inscrivez la lecture\n"+
			"dans `global` avec la raison pour laquelle elle est mondiale :\n\n"+
			strings.Join(lines, "\n"))
}

// Une exemption périmée est signalée : sans cela elle survivrait à la borne
// qu'on vient de poser, et couvrirait la prochaine lecture du même nom.
func TestNoStaleExemption(t *testing.T) {
	stale, err := guard.Stale("../..", guard.Options{Collections: collections, Global: global})
	require.NoError(t, err)
	assert.Empty(t, stale, "ces lectures sont bornées (ou ont disparu) : retirez-les de `global`")
}
