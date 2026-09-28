package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/user"
	"github.com/kgtech-org/dira-core-api/pkg/support"
)

// Les clés du guichet de support sont déclarées dans `pkg/support` — importé
// par les verticales — et recopiées ici avec leurs gabarits. Une clé sans
// gabarit est une notification qui ne part jamais, sans erreur.
func TestEverySupportKeyHasATemplateAndItsVariables(t *testing.T) {
	for key, vars := range map[string][]string{
		support.KeyLostItemReported:      {"item", "ref"},
		support.KeyLostItemFound:         {"item"},
		support.KeyLostItemNotFound:      {"item"},
		support.KeyTicketReply:           {"reference"},
		support.KeyTicketResolved:        {"reference"},
		support.KeyStaffTicketOpened:     {"kind", "ref", "who"},
		support.KeyStaffLostItemAnswered: {"ref", "who", "answer", "item"},
	} {
		tmpl, ok := defaults[key]
		assert.True(t, ok, "gabarit manquant pour %s", key)
		assert.True(t, tmpl.Enabled, key)
		assert.Subset(t, Provided(key), vars, key)
	}
	assert.Equal(t, CategoryStaff, categoryOf(support.KeyStaffTicketOpened))
	assert.Equal(t, CategorySupport, categoryOf(support.KeyLostItemReported))
	assert.False(t, Muteable(CategorySupport), "on a posé la question, on reçoit la réponse")
}

// UN SEUL APPAREIL PAR CHAUFFEUR : la clé est déclarée dans `internal/user`,
// qui l'émet, et recopiée ici avec son gabarit — comme celles du support.
//
// ⚠️ UNE CLÉ SANS GABARIT EST UNE NOTIFICATION QUI NE PART JAMAIS, SANS ERREUR.
// Et celle-ci est la seule alerte que reçoit un chauffeur dont quelqu'un
// d'autre utilise le compte : l'appareil chassé, lui, est peut-être éteint.
func TestTheSupersededSessionKeyHasItsTemplate(t *testing.T) {
	tmpl, ok := defaults[user.KeySessionSuperseded]
	require.True(t, ok, "gabarit manquant pour %s", user.KeySessionSuperseded)
	assert.True(t, tmpl.Enabled)
	assert.Subset(t, Provided(user.KeySessionSuperseded), []string{"device"})
	// ⚠️ PAS COUPABLE. Rangée dans les mises à jour de commande, elle aurait
	// disparu avec elles — et le chauffeur aurait découvert le partage de son
	// compte en constatant qu'il ne reçoit plus d'appels.
	assert.Equal(t, CategorySecurity, categoryOf(user.KeySessionSuperseded))
	assert.False(t, Muteable(CategorySecurity))
	// Le texte nomme l'appareil QUI PREND la session, jamais celui qui la
	// perd : le message part sur TOUS les téléphones du compte, y compris
	// celui qui vient de se connecter.
	assert.Contains(t, tmpl.Locales[LocaleFR].Body, "[device]")
	assert.Contains(t, tmpl.Locales[LocaleEN].Body, "[device]")
}
