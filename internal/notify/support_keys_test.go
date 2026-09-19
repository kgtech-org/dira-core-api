package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
