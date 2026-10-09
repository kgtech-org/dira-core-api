package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// newFleetDriver crée un chauffeur AU VOLANT D'UNE VOITURE DE SOCIÉTÉ, et rend
// le compte, le chauffeur, la société et le véhicule.
func (f *fixture) newFleetDriver(t *testing.T) (userID, driverID, fleetID, vehicleID string) {
	t.Helper()
	userID = primitive.NewObjectID().Hex()
	driverID = primitive.NewObjectID().Hex()
	fleetID = primitive.NewObjectID().Hex()
	vehicleID = primitive.NewObjectID().Hex()
	f.fleet.byUser[userID] = driverID
	f.fleet.accounts[driverID] = userID
	ref := VehicleRef{ID: vehicleID, Motorised: true, Plate: "AB-1234", FleetID: fleetID}
	f.fleet.vehicles[driverID] = append(f.fleet.vehicles[driverID], ref)
	f.fleet.parc[fleetID] = append(f.fleet.parc[fleetID], ref)
	// Le véhicule a un CONDUCTEUR et un PROPRIÉTAIRE : les deux réponses
	// existent, et elles ne désignent pas la même personne.
	f.fleet.owner[vehicleID] = driverID
	f.fleet.fleetOf[vehicleID] = fleetID
	return userID, driverID, fleetID, vehicleID
}

// validPersonDoc dépose UNE pièce du chauffeur et la fait valider.
func validPersonDoc(t *testing.T, f *fixture, user, kind, vehicleID string, expires *time.Time) {
	t.Helper()
	d := submit(t, f, user, SubmitDocumentRequest{
		Kind: kind, FileURL: "https://files.dira.llc/" + kind + ".jpg",
		VehicleID: vehicleID, ExpiresAt: expires,
	})
	_, err := f.svc.ReviewDocument(context.Background(), newAdmin(), d.ID,
		ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
}

func validFleetDoc(t *testing.T, f *fixture, fleetID, vehicleID, kind string) {
	t.Helper()
	doc, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: kind, FileURL: "https://files.dira.llc/" + kind + ".jpg", VehicleID: vehicleID,
	})
	require.NoError(t, err)
	admin := primitive.NewObjectID().Hex()
	_, err = f.svc.ReviewDocument(context.Background(), admin, doc.ID,
		ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
}

// ⚠️⚠️ LE TEST QUI JUSTIFIE TOUT CE FICHIER. On réclamait à un chauffeur la
// carte grise d'une voiture qui n'est pas la sienne — et la relance
// automatique le lui redisait tous les trois jours, pour une pièce qu'il ne
// peut pas fournir.
func TestAFleetDriverIsNotAskedForHisCompanysPapers(t *testing.T) {
	f := newFixture()
	user, _, _, _ := f.newFleetDriver(t)

	st, err := f.svc.Compliance(context.Background(), user)
	require.NoError(t, err)

	// Ses pièces À LUI sont demandées : identité, selfie, casier, permis.
	assert.Contains(t, st.Missing, DocIDCard)
	assert.Contains(t, st.Missing, DocLicence)
	// Celles de la VOITURE ne le sont pas — elles sont dans l'autre liste.
	for _, kind := range []string{DocRegistration, DocInsurance, DocInspection} {
		for _, m := range st.Missing {
			assert.NotContains(t, m, kind,
				"« %s » est le papier de la société, pas du chauffeur", kind)
		}
	}
	assert.NotEmpty(t, st.MissingFleet, "ce qui manque à la société doit RESTER visible")
	// ⚠️ ET LE DOSSIER N'EST PAS « EN RÈGLE » POUR AUTANT : la voiture roule
	// sans papiers. C'est le DESTINATAIRE du rappel qui change, pas la réalité.
	assert.False(t, st.Compliant)
}

// ⚠️ LA RELANCE AUTOMATIQUE SE TAIT quand il ne manque QUE des papiers de
// société : c'est elle qui harcelait. Mais la personne reste dans la file de
// l'exploitation, parce que quelqu'un doit agir — son propriétaire.
func TestTheRemindSweepSkipsAFleetOnlyGap(t *testing.T) {
	f := newFixture()
	user, _, fleetID, vehicleID := f.newFleetDriver(t)
	// Tout ce qui lui incombe est déposé et validé.
	for _, kind := range []string{DocIDCard, DocSelfie, DocLicence} {
		validPersonDoc(t, f, user, kind, "", inDays(400))
	}
	validPersonDoc(t, f, user, DocCriminalRecord, "", inDays(100))
	for _, kind := range VehiclePhotoKinds {
		validPersonDoc(t, f, user, kind, vehicleID, nil)
	}

	people, _, err := f.svc.MissingSweep(context.Background(), "", 50)
	require.NoError(t, err)
	assert.Empty(t, people, "on ne relance pas quelqu'un pour le papier d'un autre")

	// Et dès que la société dépose, le chauffeur devient conforme SANS avoir
	// rien envoyé de plus.
	for _, kind := range []string{DocRegistration, DocInsurance, DocInspection} {
		validFleetDoc(t, f, fleetID, vehicleID, kind)
	}
	st, err := f.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	assert.True(t, st.Compliant, "manquait : %v / société : %v", st.Missing, st.MissingFleet)
}

// ⚠️ LA PIÈCE DE LA SOCIÉTÉ APPARAÎT SUR LA FICHE DU CHAUFFEUR, MARQUÉE. Cachée,
// il la redéposerait — et un opérateur regarderait deux fois la même image.
func TestTheCompanyPaperShowsOnTheDriversFileMarked(t *testing.T) {
	f := newFixture()
	user, _, fleetID, vehicleID := f.newFleetDriver(t)
	validFleetDoc(t, f, fleetID, vehicleID, DocInsurance)

	st, err := f.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	var found *DocumentResponse
	for i := range st.Documents {
		if st.Documents[i].Kind == DocInsurance {
			found = &st.Documents[i]
		}
	}
	require.NotNil(t, found, "l'assurance de sa voiture doit être visible")
	assert.True(t, found.ByFleet, "et dite comme déposée par le propriétaire")
}

// ⚠️ LE MÊME CONTRÔLE QUE POUR UNE PERSONNE, DE L'AUTRE CÔTÉ : déposer
// l'assurance de la voiture d'un autre la rendrait conforme sans que son
// propriétaire le sache.
func TestAFleetCannotPaperAVehicleThatIsNotItsOwn(t *testing.T) {
	f := newFixture()
	_, _, fleetID, _ := f.newFleetDriver(t)
	_, _, _, someoneElses := f.newFleetDriver(t)

	_, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocInsurance, FileURL: "https://files.dira.llc/x.jpg", VehicleID: someoneElses,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vehicle_not_in_your_fleet")

	// Et un véhicule qui n'est dans AUCUNE flotte non plus — celui d'un
	// chauffeur propriétaire de sa voiture.
	ownDriver, own := f.newDriver(t, 1)
	_ = ownDriver
	_, err = f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocInsurance, FileURL: "https://files.dira.llc/x.jpg", VehicleID: own,
	})
	require.Error(t, err)
}

// ⚠️⚠️ UNE SOCIÉTÉ NE DÉPOSE PAS LA PIÈCE D'IDENTITÉ DE QUELQU'UN. Le geste
// serait compréhensible — le patron a les photocopies — et profondément faux :
// cette pièce appartient à la personne, et c'est elle qui décide de la confier.
func TestAFleetCannotSubmitAPersonalDocument(t *testing.T) {
	f := newFixture()
	_, _, fleetID, vehicleID := f.newFleetDriver(t)

	for _, kind := range PersonKinds {
		_, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://files.dira.llc/x.jpg", VehicleID: vehicleID,
		})
		require.Error(t, err, "kind %s", kind)
	}
	// Le permis aussi — et SANS véhicule, ce qui est le chemin par lequel il
	// serait passé si l'on s'était contenté de `CheckOwner`.
	_, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocLicence, FileURL: "https://files.dira.llc/x.jpg",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to the person")
}

// ⚠️ LA FILE D'ARBITRAGE NOMME LA SOCIÉTÉ, ET LAISSE LE CHAUFFEUR VIDE.
// `owner_id` vaut la flotte sur ces pièces : le rendre comme un `driver_id`
// aurait fait cliquer l'opérateur sur la fiche d'un chauffeur qui n'existe pas,
// et lui aurait fait redemander la carte grise à quelqu'un qui ne l'a pas.
func TestTheQueueNamesTheFleetAndNotADriver(t *testing.T) {
	f := newFixture()
	_, _, fleetID, vehicleID := f.newFleetDriver(t)
	_, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocRegistration, FileURL: "https://files.dira.llc/cg.jpg", VehicleID: vehicleID,
	})
	require.NoError(t, err)

	items, err := f.svc.ComplianceQueue(context.Background(), 50)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, fleetID, items[0].FleetID)
	assert.Empty(t, items[0].DriverID, "aucun chauffeur ne répond de cette pièce")
	assert.Empty(t, items[0].UserID)
}

// ⚠️ L'EFFACEMENT D'UN COMPTE NE PREND PAS LES PAPIERS DE LA SOCIÉTÉ. La carte
// grise d'une voiture d'entreprise n'est pas une donnée personnelle du
// chauffeur qui s'en va — et la voiture continue de rouler avec quelqu'un
// d'autre. Les effacer aurait mis une société en défaut parce qu'un de ses
// conducteurs a fermé son compte.
func TestErasingADriverLeavesTheCompanyPapers(t *testing.T) {
	f := newFixture()
	user, driverID, fleetID, vehicleID := f.newFleetDriver(t)
	validPersonDoc(t, f, user, DocIDCard, "", inDays(400))
	validFleetDoc(t, f, fleetID, vehicleID, DocInsurance)

	driverOID, err := primitive.ObjectIDFromHex(driverID)
	require.NoError(t, err)
	n, err := f.svc.PurgeOf(context.Background(), driverOID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "seule SA pièce part")

	fleetOID, err := primitive.ObjectIDFromHex(fleetID)
	require.NoError(t, err)
	left, err := f.repo.DocumentsByOwner(context.Background(), fleetOID)
	require.NoError(t, err)
	assert.Len(t, left, 1, "l'assurance de la voiture reste")
}

// L'ÉTAT DU PARC, VU PAR SON PROPRIÉTAIRE : par véhicule, avec la plaque.
//
// ⚠️ PAS DE DRAPEAU GLOBAL SEUL : « votre flotte n'est pas en règle » sur douze
// voitures n'appelle aucun geste. C'est la plaque qui dit quoi faire.
func TestTheFleetSeesWhatIsMissingVehicleByVehicle(t *testing.T) {
	f := newFixture()
	_, _, fleetID, vehicleID := f.newFleetDriver(t)

	st, err := f.svc.FleetCompliance(context.Background(), fleetID)
	require.NoError(t, err)
	require.Len(t, st.Vehicles, 1)
	assert.Equal(t, vehicleID, st.Vehicles[0].VehicleID)
	assert.Equal(t, "AB-1234", st.Vehicles[0].Plate, "un propriétaire ne reconnaît pas un hexadécimal")
	assert.False(t, st.Compliant)
	// Les types sont NUS ici : le véhicule est déjà nommé par la ligne.
	assert.Contains(t, st.Vehicles[0].Missing, DocRegistration)
	assert.NotContains(t, st.Vehicles[0].Missing, DocRegistration+":"+vehicleID)
	// ⚠️ ET PAS LES PIÈCES DE LA PERSONNE : le permis de son chauffeur ne
	// figure pas dans ce que la société doit déposer.
	assert.NotContains(t, st.Vehicles[0].Missing, DocLicence)

	for _, kind := range VehicleKinds {
		validFleetDoc(t, f, fleetID, vehicleID, kind)
	}
	st, err = f.svc.FleetCompliance(context.Background(), fleetID)
	require.NoError(t, err)
	assert.True(t, st.Compliant)
}

// ⚠️ LE DÉPÔT DU CONDUCTEUR RESTE POSSIBLE SUR UNE VOITURE DE SOCIÉTÉ, et c'est
// voulu : les papiers sont souvent dans la boîte à gants, et un chauffeur qui
// peut régulariser sa voiture ne doit pas en être empêché parce que son patron
// ne répond pas.
func TestTheDriverMayStillPaperTheCompanyCarHimself(t *testing.T) {
	f := newFixture()
	user, _, _, vehicleID := f.newFleetDriver(t)

	validPersonDoc(t, f, user, DocInsurance, vehicleID, inDays(200))
	st, err := f.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	for _, m := range st.MissingFleet {
		assert.NotContains(t, m, DocInsurance,
			"l'assurance est couverte, qui que l'ait déposée")
	}
}
