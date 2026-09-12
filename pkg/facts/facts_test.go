package facts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Un fait fait l'aller-retour par le flux sans rien perdre : c'est la seule
// garantie que l'analytique lit ce que la verticale a écrit.
func TestFactSurvivesTheStream(t *testing.T) {
	at := time.Date(2026, 9, 12, 18, 30, 0, 0, time.UTC)
	in := Fact{Kind: DemandUnmet, Vertical: VerticalVTC, At: at, Geo: [2]float64{1.2385, 6.1481},
		Ref: "ride1", Actor: "rider1", Reason: ReasonExhausted, Attrs: map[string]string{"attempts": "3"}}
	values, err := Encode(in)
	require.NoError(t, err)
	assert.Equal(t, "demand.unmet", values["kind"])
	assert.Equal(t, "1789237800000", values["at"])

	out, err := Decode(values)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestGarbageIsAnError(t *testing.T) {
	_, err := Decode(map[string]any{"json": "{"})
	assert.Error(t, err)
}
