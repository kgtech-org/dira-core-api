package marker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Les quatre genres, dans l'ordre où la console et les applications les
// montrent — et rien d'autre : un genre inconnu n'est pas réglable.
func TestKinds(t *testing.T) {
	assert.Equal(t, []string{"courier", "client", "merchant", "stop"}, Kinds)
	for _, k := range Kinds {
		assert.True(t, validKind(k), k)
	}
	assert.False(t, validKind("driver"), "le chauffeur VTC est dessiné par son mode de véhicule")
	assert.False(t, validKind(""))
}
