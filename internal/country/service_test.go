package country

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAccounts struct{ country map[string]string }

func (f *fakeAccounts) CountryOf(_ context.Context, id string) (string, error) {
	return f.country[id], nil
}
func (f *fakeAccounts) SetCountry(_ context.Context, id, code string) error {
	f.country[id] = code
	return nil
}

type fakeIP map[string]string

func (f fakeIP) Country(_ context.Context, ip string) (string, error) { return f[ip], nil }

func f64(v float64) *float64 { return &v }

// Sans dépôt, le service tient TOUT le catalogue pour ouvert ; on ferme le
// Ghana à la main pour tester la zone.
func newTestService(accounts *fakeAccounts, ips fakeIP) *Service {
	s := NewService(nil, "TG")
	s.enabled = map[string]bool{"TG": true, "BJ": true}
	s.loadedAt = time.Now()
	s.SetAccounts(accounts)
	s.SetIPLookup(ips)
	return s
}

func TestResolveByPosition(t *testing.T) {
	acc := &fakeAccounts{country: map[string]string{"u": "TG"}}
	s := newTestService(acc, nil)

	// À Cotonou : le Bénin est ouvert, le compte suit.
	out, err := s.Resolve(context.Background(), "u", "", ResolveRequest{Lng: f64(2.4183), Lat: f64(6.3654)})
	require.NoError(t, err)
	assert.Equal(t, "BJ", out.Country)
	assert.Equal(t, ResolvedByGeo, out.Source)
	assert.True(t, out.Supported)
	assert.True(t, out.Updated)
	assert.Equal(t, "BJ", acc.country["u"])

	// Même endroit, même pays : rien ne bouge.
	out, err = s.Resolve(context.Background(), "u", "", ResolveRequest{Lng: f64(2.4183), Lat: f64(6.3654)})
	require.NoError(t, err)
	assert.False(t, out.Updated)
}

func TestResolveOutOfZoneKeepsTheAccount(t *testing.T) {
	acc := &fakeAccounts{country: map[string]string{"u": "TG"}}
	s := newTestService(acc, fakeIP{"1.2.3.4": "GH"})

	// À Accra, depuis une IP ghanéenne : le Ghana n'est pas ouvert.
	out, err := s.Resolve(context.Background(), "u", "1.2.3.4", ResolveRequest{Lng: f64(-0.1870), Lat: f64(5.6037)})
	require.NoError(t, err)
	assert.Equal(t, "TG", out.Country)
	assert.Equal(t, ResolvedByAccount, out.Source)
	assert.Equal(t, "GH", out.Detected)
	assert.False(t, out.Supported)
	assert.False(t, out.Updated)
}

func TestResolveFallsBackToIPThenDefault(t *testing.T) {
	acc := &fakeAccounts{country: map[string]string{}}
	s := newTestService(acc, fakeIP{"5.6.7.8": "BJ", "9.9.9.9": ""})

	// Sans position, l'IP décide — et le compte, qui n'avait pas de pays,
	// en reçoit un.
	out, err := s.Resolve(context.Background(), "u", "5.6.7.8", ResolveRequest{})
	require.NoError(t, err)
	assert.Equal(t, "BJ", out.Country)
	assert.Equal(t, ResolvedByIP, out.Source)
	assert.True(t, out.Updated)

	// Ni position ni IP connue, compte sans pays : le défaut.
	acc.country = map[string]string{}
	out, err = s.Resolve(context.Background(), "u", "9.9.9.9", ResolveRequest{})
	require.NoError(t, err)
	assert.Equal(t, "TG", out.Country)
	assert.Equal(t, ResolvedByDefault, out.Source)
	assert.True(t, out.Updated)
}

func TestResolveRefusesHalfCoordinates(t *testing.T) {
	s := newTestService(&fakeAccounts{country: map[string]string{}}, nil)
	_, err := s.Resolve(context.Background(), "u", "", ResolveRequest{Lng: f64(1)})
	assert.ErrorIs(t, err, errNoCoordinates)
}
