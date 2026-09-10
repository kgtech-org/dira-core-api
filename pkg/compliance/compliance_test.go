package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

func newAdmin() string { return primitive.NewObjectID().Hex() }

func inDays(n int) *time.Time {
	t := time.Now().UTC().AddDate(0, 0, n)
	return &t
}

func submit(t *testing.T, fx *fixture, driver string, req SubmitDocumentRequest) *DocumentResponse {
	t.Helper()
	d, err := fx.svc.SubmitDocument(context.Background(), driver, req)
	require.NoError(t, err)
	return d
}

// ⚠️ Le TYPE de la pièce décide de son propriétaire. Une carte grise sans
// véhicule, ou un permis attaché à une moto, sont refusés par une TABLE — pas
// par la discipline de l'appelant.
func TestDocumentOwnerIsDecidedByKind(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)

	_, err := fx.svc.SubmitDocument(context.Background(), driver, SubmitDocumentRequest{
		Kind: DocInsurance, FileURL: "https://f/a.jpg", ExpiresAt: inDays(200),
	})
	require.Error(t, err, "une assurance sans véhicule n'a pas de sens")

	_, err = fx.svc.SubmitDocument(context.Background(), driver, SubmitDocumentRequest{
		Kind: DocLicence, FileURL: "https://f/b.jpg", VehicleID: vehicle, ExpiresAt: inDays(200),
	})
	require.Error(t, err, "un permis appartient à la personne, pas à la moto")
}

// Le véhicule doit être LE SIEN : assurer la moto d'un autre la rendrait
// conforme sans que son propriétaire le sache.
func TestCannotDocumentSomeoneElsesVehicle(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	_, otherVehicle := fx.newDriver(t, 5)

	_, err := fx.svc.SubmitDocument(context.Background(), driver, SubmitDocumentRequest{
		Kind: DocInsurance, FileURL: "https://f/a.jpg", VehicleID: otherVehicle, ExpiresAt: inDays(200),
	})
	require.Error(t, err)
	assert.Equal(t, "vehicle_not_found", apperr.From(err).Code)
}

// ⚠️ Un dépôt repasse la pièce en `pending`, même si l'ancienne était validée :
// une image nouvelle est une image que personne n'a regardée.
func TestResubmitReturnsToPending(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	ctx := context.Background()
	doc := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)})

	reviewed, err := fx.svc.ReviewDocument(ctx, newAdmin(), doc.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
	require.Equal(t, DocValid, reviewed.State)

	again := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/2.jpg", ExpiresAt: inDays(400)})
	assert.Equal(t, DocPending, again.State)
	assert.Equal(t, doc.ID, again.ID, "le dépôt REMPLACE, il n'ajoute pas une seconde ligne")
}

// Un refus MUET laisserait le livreur redéposer la même image.
func TestRejectionNeedsAReason(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	doc := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocIDCard, FileURL: "https://f/1.jpg"})

	_, err := fx.svc.ReviewDocument(context.Background(), newAdmin(), doc.ID, ReviewDocumentRequest{Status: DocRejected})
	require.Error(t, err)

	ok, err := fx.svc.ReviewDocument(context.Background(), newAdmin(), doc.ID,
		ReviewDocumentRequest{Status: DocRejected, Reason: "photo floue"})
	require.NoError(t, err)
	assert.Equal(t, DocRejected, ok.State)
	assert.Equal(t, "photo floue", ok.RejectedReason)
}

// L'état se CALCULE à la lecture : le stocker demanderait une tâche de fond, et
// une pièce resterait « valide » jusqu'à son prochain passage.
func TestStateIsComputedFromExpiry(t *testing.T) {
	now := time.Now().UTC()
	valid := &Document{Status: DocValid, ExpiresAt: inDays(90)}
	assert.Equal(t, DocValid, valid.State(now))

	soon := &Document{Status: DocValid, ExpiresAt: inDays(ExpiryWarningDays - 1)}
	assert.Equal(t, DocExpiring, soon.State(now))
	assert.True(t, soon.Compliant(now), "un préavis n'est pas un défaut")

	gone := &Document{Status: DocValid, ExpiresAt: inDays(-1)}
	assert.Equal(t, DocExpired, gone.State(now))
	assert.False(t, gone.Compliant(now))

	// Sans date, la pièce ne périme pas — une carte grise n'en a pas.
	forever := &Document{Status: DocValid}
	assert.Equal(t, DocValid, forever.State(now))
}

// ⚠️ Une pièce `pending` dont la date est passée n'est PAS « expirée » : elle
// n'a jamais compté. L'une attend un administrateur, l'autre le livreur.
func TestPendingStaysPendingWhateverTheDate(t *testing.T) {
	assert.Equal(t, DocPending, (&Document{Status: DocPending, ExpiresAt: inDays(-30)}).State(time.Now().UTC()))
}

// ⚠️ Une pièce déposée ne compte QU'APRÈS validation. Accepter d'abord
// reviendrait à faire rouler quelqu'un sur une photo que personne n'a regardée.
func TestPendingDocumentDoesNotMakeCompliant(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)})

	st, err := fx.svc.Compliance(context.Background(), driver)
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Missing, DocLicence, "déposée n'est pas validée")
}

// Les pièces MANQUANTES sont rendues à part : une liste vide ne se distingue
// pas d'une liste complète, et un livreur qui n'a rien déposé verrait un écran
// sans défaut.
func TestMissingDocumentsAreNamed(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)

	st, err := fx.svc.Compliance(context.Background(), driver)
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Contains(t, st.Missing, DocLicence)
	assert.Contains(t, st.Missing, DocIDCard)
	// Le véhicule est NOMMÉ : « assurance manquante » sur deux motos ne dit
	// pas laquelle rouler.
	assert.Contains(t, st.Missing, DocInsurance+":"+vehicle)
	assert.Contains(t, st.Missing, DocRegistration+":"+vehicle)
}

// Tout en règle : les deux pièces personnelles, et les deux de chaque véhicule.
func TestFullyCompliantDriver(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()
	admin := newAdmin()

	for _, req := range []SubmitDocumentRequest{
		{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)},
		{Kind: DocIDCard, FileURL: "https://f/2.jpg", ExpiresAt: inDays(900)},
		{Kind: DocRegistration, FileURL: "https://f/3.jpg", VehicleID: vehicle},
		{Kind: DocInsurance, FileURL: "https://f/4.jpg", VehicleID: vehicle, ExpiresAt: inDays(200)},
	} {
		d := submit(t, fx, driver, req)
		_, err := fx.svc.ReviewDocument(ctx, admin, d.ID, ReviewDocumentRequest{Status: DocValid})
		require.NoError(t, err)
	}

	st, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)
	assert.True(t, st.Compliant)
	assert.Empty(t, st.Missing)
	assert.Len(t, st.Documents, 4)
}

// ⚠️ Une pièce EXPIRÉE ne bloque RIEN côté serveur — décision produit. Elle
// rend simplement le livreur non conforme, et c'est l'exploitation qui agit.
// C'est pourquoi la file de conformité existe : sans elle, personne ne sait.
func TestExpiredDocumentDoesNotBlockButShows(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	ctx := context.Background()
	d := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(30)})
	_, err := fx.svc.ReviewDocument(ctx, newAdmin(), d.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
	// On fait VIEILLIR la pièce dans le dépôt.
	//
	// ⚠️ Par l'index, pas par la valeur : `for _, doc := range` rend une COPIE,
	// et la version précédente de ce test ne modifiait rien — elle vérifiait
	// donc qu'une pièce valide est valide.
	for i := range fx.repo.documents {
		fx.repo.documents[i].ExpiresAt = inDays(-1)
	}

	st, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)
	assert.False(t, st.Compliant)
	assert.Equal(t, DocExpired, st.Documents[0].State)
	// ⚠️ Ce paquet ne BLOQUE rien : il dit qui n'est pas en règle, et c'est
	// tout. Que la course reste acceptable se vérifie dans la verticale, qui
	// seule décide d'accepter — voir `internal/delivery`.
}

// La FILE est la contrepartie de ce choix : sans elle, « l'exploitation
// suspend à la main » veut dire « personne ne suspend ».
func TestComplianceQueueShowsWhatNeedsAction(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()

	pending := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)})
	good := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocRegistration, FileURL: "https://f/2.jpg", VehicleID: vehicle})
	_, err := fx.svc.ReviewDocument(ctx, newAdmin(), good.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)

	queue, err := fx.svc.ComplianceQueue(ctx, 50)
	require.NoError(t, err)
	require.Len(t, queue, 1, "seule la pièce à regarder remonte")
	assert.Equal(t, pending.ID, queue[0].ID)
	// De quoi retrouver la personne. Le NOM, lui, est attaché par la verticale
	// qui connaît les comptes — cette bibliothèque ne les connaît pas.
	assert.NotEmpty(t, queue[0].DriverID)
}

// Une pièce DÉJÀ périmée à la remise n'est pas une mise en conformité.
func TestPastExpiryIsRefused(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	_, err := fx.svc.SubmitDocument(context.Background(), driver, SubmitDocumentRequest{
		Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(-1),
	})
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
}
