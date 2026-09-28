package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/session"
)

// ⚠️ CE QUI PASSE SUR LE CANAL EST UN CONTRAT ENTRE DEUX DÉPÔTS. `dira-tracking`
// lit ce message-ci pour fermer le socket d'un téléphone chassé, et il ne
// partage AUCUN code avec le socle — c'est le seul service qui puisse tomber
// sans emporter les autres, et cette indépendance a un prix : renommer un champ
// ici ne casse rien à la compilation, ni aucun test de ce dépôt. Le suivi
// cesserait simplement de fermer les sockets, en silence, et on ne le
// découvrirait qu'en voyant une voiture à deux endroits.
//
// Ce test fige donc les noms de champs, et son jumeau dans `dira-tracking` fige
// la lecture du même message.
func TestTheSupersededMessageKeepsItsWireNames(t *testing.T) {
	raw, err := json.Marshal(session.Superseded{
		UserID: "u1", DeviceID: "install-B", DeviceName: "Itel A70", At: 1727520000000,
	})
	require.NoError(t, err)

	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))
	assert.Equal(t, "u1", wire["user_id"])
	assert.Equal(t, "install-B", wire["device_id"])
	assert.Equal(t, "Itel A70", wire["device_name"])
	assert.EqualValues(t, 1727520000000, wire["at"])
}

// Les noms des clés et du canal sont eux aussi un contrat entre dépôts : le
// suivi lit « dira:session:device:<compte> » et écoute « dira:session:superseded ».
func TestTheRegistryNamesAreTheOnesTrackingReads(t *testing.T) {
	assert.Equal(t, "dira:session:device:", session.KeyPrefix)
	assert.Equal(t, "dira:session:superseded", session.Channel)
}

// ⚠️ SANS REGISTRE, RIEN NE DOIT CASSER. `New` rend nil quand aucun client Redis
// n'est branché, et un registre nul n'accepte ni ne refuse : il ignore. C'est
// l'état d'un déploiement où Redis n'est pas partagé, et le service doit y
// tourner comme avant ce mécanisme — sans quoi la mise en service de la règle
// aurait enfermé dehors tous les chauffeurs de la plateforme.
func TestANilRegistryRefusesNobody(t *testing.T) {
	var r *session.Registry = session.New(nil, 0)
	require.Nil(t, r)

	ok, holder := r.Accepts(context.Background(), "u1", "install-A")
	assert.True(t, ok)
	assert.Empty(t, holder.ID)

	previous, chased := r.Bind(context.Background(), "u1", session.Device{ID: "install-A"})
	assert.Empty(t, previous.ID)
	assert.False(t, chased)

	current, known := r.Current(context.Background(), "u1")
	assert.False(t, known)
	assert.Empty(t, current.ID)

	// Et ces deux-là ne doivent pas paniquer.
	r.Assert(context.Background(), "u1", session.Device{ID: "install-A"})
	r.Release(context.Background(), "u1", "install-A")
}
