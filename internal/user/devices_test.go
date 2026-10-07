package user

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// COMBIEN D'APPAREILS UN COMPTE PEUT TENIR — et lequel part.

// signIn ouvre une session de plus et rend son jeton de rafraîchissement.
func signIn(t *testing.T, svc *Service, phone, deviceID string) string {
	t.Helper()
	resp, err := svc.Login(context.Background(), LoginRequest{
		Phone: phone, Password: "s3cret-password", App: "client", DeviceID: deviceID,
	})
	require.NoError(t, err)
	// Les sessions sont triées sur leur date : sans ce pas, deux connexions
	// de la même milliseconde seraient départagées au hasard.
	time.Sleep(2 * time.Millisecond)
	return resp.RefreshToken
}

func newBoundedService(t *testing.T, max int) (*Service, *fakeUserRepo, string) {
	t.Helper()
	svc, repo, _ := newUserTestService()
	svc.SetCountryPolicies(fakePolicies{maxDevices: max})
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	return svc, repo, "+22890000000"
}

// Au-delà de la borne, c'est la session la PLUS SILENCIEUSE qui part — et la
// nouvelle qui reste.
func TestBeyondTheLimitTheQuietestSessionIsSignedOut(t *testing.T) {
	svc, _, phone := newBoundedService(t, 3)

	first := signIn(t, svc, phone, "")
	second := signIn(t, svc, phone, "")
	third := signIn(t, svc, phone, "")
	fourth := signIn(t, svc, phone, "")

	// La première est partie, les trois dernières tiennent.
	_, err := svc.Refresh(context.Background(), first, "")
	require.Error(t, err, "la session la plus ancienne doit être déconnectée")
	for i, token := range []string{second, third, fourth} {
		_, err := svc.Refresh(context.Background(), token, "")
		assert.NoError(t, err, "session %d encore valide", i+2)
	}
}

// ⚠️ LA SESSION QU'ON VIENT D'OUVRIR NE PART JAMAIS. L'évincer déconnecterait
// quelqu'un à l'instant où il donne son mot de passe, et il réessaierait en
// boucle sans comprendre.
func TestTheSessionJustOpenedIsNeverTheOneEvicted(t *testing.T) {
	svc, _, phone := newBoundedService(t, 1)

	signIn(t, svc, phone, "")
	last := signIn(t, svc, phone, "")

	_, err := svc.Refresh(context.Background(), last, "")
	assert.NoError(t, err)
}

// ⚠️ LE MÊME TÉLÉPHONE QUI REVIENT NE CONSOMME PAS UNE PLACE DE PLUS. Une
// réinstallation, ou une reconnexion sans déconnexion, est le MÊME appareil :
// le compter deux fois pousserait dehors le téléphone principal de quelqu'un.
func TestTheSameDeviceComingBackDoesNotCostASlot(t *testing.T) {
	svc, _, phone := newBoundedService(t, 2)

	tablet := signIn(t, svc, phone, "tablette")
	phone1 := signIn(t, svc, phone, "telephone")
	phone2 := signIn(t, svc, phone, "telephone") // le même, réinstallé

	// L'ancienne session de ce téléphone est partie…
	_, err := svc.Refresh(context.Background(), phone1, "")
	require.Error(t, err)
	// …mais pas la tablette, qui n'a rien à voir.
	_, err = svc.Refresh(context.Background(), tablet, "")
	assert.NoError(t, err, "la tablette ne doit pas payer la réinstallation du téléphone")
	_, err = svc.Refresh(context.Background(), phone2, "")
	assert.NoError(t, err)
}

// ⚠️ LES CHAUFFEURS NE SONT PAS CONCERNÉS : leur règle, plus stricte, vit dans
// `device.go` et ne se règle pas depuis un écran. Un `max_devices` à trois ne
// doit pas leur rendre un second appareil.
func TestAgentsKeepTheirSingleDeviceRule(t *testing.T) {
	svc, repo, _ := newUserTestService()
	svc.SetCountryPolicies(fakePolicies{maxDevices: 3})
	_, err := svc.Register(context.Background(), RegisterRequest{
		Phone: "+22891000009", Name: "Chauffeur", Password: "s3cret-password",
		Role: auth.RoleDriver, App: "driver", DeviceID: "tel-1",
	})
	require.NoError(t, err)

	first, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22891000009", Password: "s3cret-password", App: "driver", DeviceID: "tel-1",
	})
	require.NoError(t, err)
	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22891000009", Password: "s3cret-password", App: "driver", DeviceID: "tel-2",
	})
	require.NoError(t, err)

	// Le premier téléphone est chassé — la règle du terrain l'emporte.
	_, err = svc.Refresh(context.Background(), first.RefreshToken, "")
	assertUserCode(t, err, "session_superseded")
	_ = repo
}

// Sans pays branché, la borne retombe sur son défaut — trois — plutôt que de
// laisser un compte accumuler des sessions sans fin.
func TestWithoutACountryTheDefaultBoundStillApplies(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	phone := "+22890000000"

	first := signIn(t, svc, phone, "")
	for range 3 {
		signIn(t, svc, phone, "")
	}

	_, err = svc.Refresh(context.Background(), first, "")
	assert.Error(t, err, "défaut de trois appareils, même sans module de pays")
}
