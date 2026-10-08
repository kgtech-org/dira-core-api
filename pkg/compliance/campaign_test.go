package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LA COLLECTE DES PIÈCES MANQUANTES — relancer, sans harceler.

// fakeReminder compte les envois et simule la fenêtre anti-doublon, exactement
// comme la boîte de notifications du socle.
type fakeReminder struct {
	sentAt map[string]time.Time
	now    time.Time
	vars   map[string]map[string]string
}

func newReminder() *fakeReminder {
	return &fakeReminder{
		sentAt: map[string]time.Time{},
		now:    time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC),
		vars:   map[string]map[string]string{},
	}
}

func (f *fakeReminder) NotifyOnce(_ context.Context, userID, key string, within time.Duration, vars, _ map[string]string) bool {
	k := userID + ":" + key
	if last, ok := f.sentAt[k]; ok && within > 0 && f.now.Sub(last) < within {
		return false
	}
	f.sentAt[k] = f.now
	f.vars[userID] = vars
	return true
}

// enrol inscrit quelqu'un SANS aucune pièce — l'état de tous les chauffeurs
// déjà en service le jour où cinq types ont été ajoutés.
func enrol(fx *fixture, motorised bool) (userID, driverID string) {
	userID = primitive.NewObjectID().Hex()
	driverID = primitive.NewObjectID().Hex()
	fx.fleet.byUser[userID] = driverID
	fx.fleet.accounts[driverID] = userID
	fx.fleet.addVehicleOf(driverID, motorised)
	return userID, driverID
}

// ⚠️ LE BALAYAGE PARCOURT LES GENS, ET NON LES DOCUMENTS. C'est la décision qui
// porte toute la fonctionnalité : ceux qui n'ont RIEN envoyé n'ont AUCUN
// document, et une liste bâtie sur les documents les aurait tous ratés —
// c'est-à-dire exactement ceux qu'il faut relancer.
func TestTheSweepFindsPeopleWhoHaveSentNothingAtAll(t *testing.T) {
	fx := newFixture()
	_, driverID := enrol(fx, true)

	people, _, err := fx.svc.MissingSweep(context.Background(), "", 100)
	require.NoError(t, err)
	require.Len(t, people, 1, "quelqu'un sans aucune pièce doit apparaître")
	assert.Equal(t, driverID, people[0].DriverID)
	assert.NotEmpty(t, people[0].Missing)
}

// ⚠️ LES LIBELLÉS SONT SERVIS, SANS IDENTIFIANT DE VÉHICULE. « Assurance
// (6ac81db2fdb04195d7c49c29) » n'aide personne : ni l'opérateur, qui ne retient
// pas un hexadécimal, ni le livreur, qui n'a qu'un véhicule.
func TestTheMissingPiecesAreNamedWithoutVehicleIds(t *testing.T) {
	fx := newFixture()
	_, _ = enrol(fx, true)

	people, _, err := fx.svc.MissingSweep(context.Background(), "", 100)
	require.NoError(t, err)
	require.Len(t, people, 1)
	for _, l := range people[0].Labels {
		assert.NotContains(t, l, ":", "pas de clé technique dans un libellé")
		assert.NotEmpty(t, l)
	}
	// Et les doublons sont fondus : trois motos sans assurance font UNE ligne.
	assert.Equal(t, len(people[0].Labels), len(uniq(people[0].Labels)))
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Quelqu'un en règle n'est pas relancé : un message qui dit « il vous manque
// 0 pièce » détruit la crédibilité de tous les suivants.
func TestSomeoneInOrderIsNeverReminded(t *testing.T) {
	fx := newFixture()
	userID, driverID := enrol(fx, false)
	ctx := context.Background()
	admin := newAdmin()

	// Tout ce qu'un cycliste doit : ses pièces de personne, et la photo de son
	// vélo.
	for _, kind := range PersonKinds {
		d := submit(t, fx, userID, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/x.jpg", ExpiresAt: inDays(900),
		})
		_, err := fx.svc.ReviewDocument(ctx, admin, d.ID, ReviewDocumentRequest{Status: DocValid})
		require.NoError(t, err)
	}
	vehicles, err := fx.fleet.VehiclesOf(ctx, driverID)
	require.NoError(t, err)
	photo := submit(t, fx, userID, SubmitDocumentRequest{
		Kind: DocVehicleSide, VehicleID: vehicles[0].ID, FileURL: "https://f/bike.jpg",
	})
	_, err = fx.svc.ReviewDocument(ctx, admin, photo.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)

	people, _, err := fx.svc.MissingSweep(ctx, "", 100)
	require.NoError(t, err)
	assert.Empty(t, people)
}

// ⚠️⚠️ LE TEST QUI COMPTE : ON NE REDIT PAS LA MÊME CHOSE À LA MÊME PERSONNE.
// Une campagne qui part deux fois de suite devient un fond sonore — et la
// relance n'est PAS coupable (catégorie `support`), donc la personne ne peut pas
// l'éteindre : elle apprendrait simplement à ne plus la lire, et c'est la
// relance suivante, celle qui compte, qui serait perdue.
func TestTheSameReminderIsNotSentTwiceInTheWindow(t *testing.T) {
	fx := newFixture()
	rem := newReminder()
	fx.svc.SetReminder(rem)
	_, _ = enrol(fx, true)
	ctx := context.Background()

	first, err := fx.svc.Remind(ctx, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Missing)
	assert.Equal(t, 1, first.Reminded)
	assert.Zero(t, first.Skipped)

	// Relancée tout de suite : rien ne repart.
	second, err := fx.svc.Remind(ctx, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Missing)
	assert.Zero(t, second.Reminded)
	// ⚠️ `Skipped` EST RENDU, et c'est ce qui évite de prendre le garde-fou
	// pour une panne : « 0 envoyé » tout court ressemble à une erreur.
	assert.Equal(t, 1, second.Skipped)

	// Passé la fenêtre, on peut redire.
	rem.now = rem.now.Add(RemindEvery + time.Hour)
	third, err := fx.svc.Remind(ctx, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, third.Reminded)
}

// ⚠️ LE MESSAGE NOMME LES PIÈCES. « Vous n'êtes pas en règle » n'appelle aucun
// geste ; « il vous manque votre casier judiciaire » en appelle un. C'est toute
// la différence entre une relance et un reproche.
func TestTheReminderNamesWhatIsMissing(t *testing.T) {
	fx := newFixture()
	rem := newReminder()
	fx.svc.SetReminder(rem)
	userID, _ := enrol(fx, true)

	_, err := fx.svc.Remind(context.Background(), "", 100)
	require.NoError(t, err)
	vars := rem.vars[userID]
	require.NotNil(t, vars)
	assert.NotEmpty(t, vars["documents"])
	assert.NotEqual(t, "0", vars["count"])
	// ⚠️ TROIS AU PLUS DANS LE TEXTE : une bannière de téléphone coupe au-delà,
	// et une liste tronquée au milieu d'un mot est pire qu'une liste courte
	// suivie de « et 2 autre(s) ».
	assert.LessOrEqual(t, countCommas(vars["documents"]), 2,
		"au plus trois pièces nommées, le reste compté")
}

func countCommas(s string) int {
	n := 0
	for _, r := range s {
		if r == ',' {
			n++
		}
	}
	return n
}

// ⚠️ SANS COMPTE LIÉ, ON NE PRÉTEND PAS AVOIR ENVOYÉ. Un profil de chauffeur
// sans compte est un défaut d'exploitation : il apparaît dans la liste, compté
// `unreachable`, parce qu'un chiffre qui le rangerait avec les envoyés ferait
// croire que la personne a été prévenue.
func TestAProfileWithNoAccountIsCountedUnreachableNotSent(t *testing.T) {
	fx := newFixture()
	rem := newReminder()
	fx.svc.SetReminder(rem)
	// Un chauffeur sans compte : on l'inscrit à la main, sans `accounts`.
	driverID := primitive.NewObjectID().Hex()
	fx.fleet.accounts[driverID] = ""
	fx.fleet.addVehicleOf(driverID, true)

	res, err := fx.svc.Remind(context.Background(), "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Missing)
	assert.Zero(t, res.Reminded)
	assert.Equal(t, 1, res.Unreachable)
}

// Sans relance branchée du tout, on ne prétend rien avoir envoyé — plutôt que
// de compter des envois imaginaires.
func TestWithNoReminderWiredNothingIsClaimedSent(t *testing.T) {
	fx := newFixture()
	_, _ = enrol(fx, true)
	res, err := fx.svc.Remind(context.Background(), "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Missing)
	assert.Zero(t, res.Reminded)
	assert.Equal(t, 1, res.Unreachable)
}

// La pagination est STABLE et avance : sans cela, la dernière page relancerait
// ce que la première a déjà relancé.
func TestTheSweepPagesForward(t *testing.T) {
	fx := newFixture()
	for range 5 {
		enrol(fx, true)
	}
	ctx := context.Background()

	first, next, err := fx.svc.MissingSweep(ctx, "", 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.NotEmpty(t, next)

	second, _, err := fx.svc.MissingSweep(ctx, next, 2)
	require.NoError(t, err)
	require.Len(t, second, 2)
	for _, a := range first {
		for _, b := range second {
			assert.NotEqual(t, a.DriverID, b.DriverID, "aucune personne relancée deux fois")
		}
	}
}
