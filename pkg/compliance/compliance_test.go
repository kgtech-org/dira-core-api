package compliance

import (
	"context"
	"errors"
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

// Tout en règle : les pièces personnelles, et celles de chaque véhicule.
//
// ⚠️ LE TEST DÉPOSE CE QUE LES LISTES DÉCLARENT, et non une liste recopiée. Il
// tenait cinq pièces en dur ; ajouter un type attendu le faisait échouer pour la
// mauvaise raison — pas « la conformité est cassée », mais « le test n'a pas été
// mis à jour ». Écrit ainsi, il vérifie ce qu'il doit : que déposer et valider
// TOUT ce qui est réclamé rend conforme, quoi qu'on réclame.
func TestFullyCompliantDriver(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()
	admin := newAdmin()

	var reqs []SubmitDocumentRequest
	for _, kind := range PersonKindsFor([]VehicleRef{{ID: vehicle, Motorised: true}}) {
		reqs = append(reqs, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/" + kind + ".jpg", ExpiresAt: inDays(400),
		})
	}
	for _, kind := range VehicleKinds {
		reqs = append(reqs, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/" + kind + ".jpg", VehicleID: vehicle,
		})
	}
	for _, req := range reqs {
		d := submit(t, fx, driver, req)
		_, err := fx.svc.ReviewDocument(ctx, admin, d.ID, ReviewDocumentRequest{Status: DocValid})
		require.NoError(t, err)
	}

	st, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)
	assert.True(t, st.Compliant)
	assert.Empty(t, st.Missing)
	assert.Len(t, st.Documents, len(reqs))
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
	// ⚠️ Et le COMPTE, quand la verticale sait le donner : sans lui, la
	// console n'affiche qu'un identifiant de chauffeur et retrouver la
	// personne demande une recherche à la main. C'est l'écran qui existe pour
	// que personne ne passe à côté d'un défaut ; le rendre pénible à utiliser
	// revient à le supprimer.
	assert.Equal(t, driver, queue[0].UserID)
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

// ⚠️ Une erreur sur le COMPTE ne doit pas faire échouer la file.
//
// C'est un arbitrage, pas un oubli : cet écran est le seul endroit où l'on
// apprend qu'un livreur n'est pas en règle — rien n'étant bloqué côté serveur.
// Le faire disparaître parce qu'un nom manque reviendrait à supprimer la seule
// contrepartie de cette décision produit.
func TestQueueSurvivesAnAccountLookupFailure(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	ctx := context.Background()
	submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)})

	fx.fleet.accountErr = errors.New("annuaire injoignable")

	queue, err := fx.svc.ComplianceQueue(ctx, 50)
	require.NoError(t, err, "la file s'affiche même sans les noms")
	require.Len(t, queue, 1)
	assert.NotEmpty(t, queue[0].DriverID, "l'identifiant reste : la file demeure actionnable")
	assert.Empty(t, queue[0].UserID, "et le compte manquant est ABSENT, pas inventé")
}

// ⚠️ UN VÉHICULE NON MOTORISÉ N'ATTEND AUCUN PAPIER.
//
// La plateforme réclamait une carte grise à un livreur À PIED, et une
// assurance à un vélo. Ces gens restaient « non conformes » pour toujours, sur
// un écran qui ne leur proposait aucun moyen de régulariser — et l'exploitation
// voyait une file de conformité pleine de défauts impossibles à corriger, ce
// qui est la meilleure façon de cesser de la regarder.
//
// ⚠️ IL ATTEND EN REVANCHE SA PHOTO (une seule, de côté), parce qu'un vélo n'a
// pas de plaque : c'est la seule façon de le reconnaître dans la rue. Ce test
// la dépose donc, et c'est tout ce qui s'ajoute — voir
// `TestAnUnmotorisedVehicleExpectsItsPhotoAndNoPaper`.
func TestUnmotorisedVehicleNeedsNoPaper(t *testing.T) {
	fx := newFixture()
	userID := primitive.NewObjectID().Hex()
	driverID := primitive.NewObjectID().Hex()
	fx.fleet.byUser[userID] = driverID
	fx.fleet.accounts[driverID] = userID
	bike := fx.fleet.addVehicleOf(driverID, false)

	ctx := context.Background()
	admin := newAdmin()
	// ⚠️ LES PIÈCES DE LA PERSONNE, ET PAS LE PERMIS. Un cycliste n'en a pas, et
	// le lui réclamer le laissait non conforme pour toujours. Si ce test
	// déposait un permis « pour faire bonne mesure », il ne prouverait plus rien
	// sur la règle. Les pièces viennent de la liste déclarée : le casier et le
	// selfie, eux, sont bien dus par un cycliste — ils ne dépendent pas du
	// véhicule.
	for _, kind := range PersonKinds {
		d := submit(t, fx, userID, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/" + kind + ".jpg", ExpiresAt: inDays(900),
		})
		_, err := fx.svc.ReviewDocument(ctx, admin, d.ID, ReviewDocumentRequest{Status: DocValid})
		require.NoError(t, err)
	}

	// La photo du vélo — la SEULE chose que son véhicule demande.
	photo := submit(t, fx, userID, SubmitDocumentRequest{
		Kind: DocVehicleSide, VehicleID: bike, FileURL: "https://f/bike.jpg",
	})
	_, err := fx.svc.ReviewDocument(ctx, admin, photo.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)

	st, err := fx.svc.Compliance(ctx, userID)
	require.NoError(t, err)
	assert.True(t, st.Compliant,
		"un livreur à vélo ne doit que les pièces de sa personne, et la photo de son vélo")
	assert.Empty(t, st.Missing)
}

// ⚠️ LE PERMIS SUIT LES VÉHICULES, et la règle est appelée directement.
//
// Un cycliste qui déclare une moto demain devra son permis dès ce jour-là,
// sans qu'on touche à sa fiche.
func TestLicenceFollowsMotorisedVehicles(t *testing.T) {
	bike := VehicleRef{ID: "b", Motorised: false}
	moto := VehicleRef{ID: "m", Motorised: true}

	// ⚠️ COMPARÉ À `PersonKinds`, et non à une liste recopiée : ce test porte sur
	// LE PERMIS, et il doit continuer à ne porter que sur lui le jour où une
	// pièce personnelle s'ajoute.
	withLicence := append(append([]string(nil), PersonKinds...), DocLicence)
	assert.ElementsMatch(t, PersonKinds, PersonKindsFor(nil), "à pied : pas de permis")
	assert.ElementsMatch(t, PersonKinds, PersonKindsFor([]VehicleRef{bike}), "à vélo : pas de permis")
	assert.ElementsMatch(t, withLicence, PersonKindsFor([]VehicleRef{moto}))
	assert.ElementsMatch(t, withLicence, PersonKindsFor([]VehicleRef{bike, moto}),
		"UNE moto suffit à exiger le permis, quel que soit le reste du parc")
	assert.NotContains(t, PersonKinds, DocLicence,
		"le permis ne doit JAMAIS entrer dans la liste de base — c'est toute la règle")
}

// Et la règle elle-même, appelée directement.
func TestKindsForDependsOnMotorisation(t *testing.T) {
	assert.Equal(t, VehicleKinds, KindsFor(VehicleRef{ID: "x", Motorised: true}))
	// ⚠️ Un non-motorisé n'attend QUE sa photo — voir
	// `TestAnUnmotorisedVehicleExpectsItsPhotoAndNoPaper` pour le pourquoi.
	assert.Equal(t, []string{DocVehicleSide}, KindsFor(VehicleRef{ID: "x", Motorised: false}))
	// Les trois pièces d'un véhicule motorisé, contrôle technique compris.
	assert.Contains(t, VehicleKinds, DocInspection)
}

// --- LES PIÈCES AJOUTÉES (casier, selfie, photos du véhicule) -----------

// ⚠️ LE CASIER JUDICIAIRE EXIGE UNE DATE D'EXPIRATION. C'est un INSTANTANÉ : il
// dit ce qu'on savait le jour de sa délivrance, et rien du lendemain. Admis sans
// date, il vaudrait pour toujours — et un extrait de 2019 marqué « valide »
// rendrait décoratif le contrôle le plus sensible de la plateforme.
func TestACriminalRecordWithoutAnExpiryIsRefused(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)

	_, err := fx.svc.SubmitDocument(context.Background(), driver, SubmitDocumentRequest{
		Kind: DocCriminalRecord, FileURL: "https://f/casier.jpg",
	})
	require.Error(t, err)
	// ⚠️ Le refus NOMME le champ : la personne doit savoir quoi chercher sur son
	// extrait — un refus muet la fait redéposer la même image.
	assert.Contains(t, err.Error(), "expires_at")

	// Avec une date, il passe.
	d := submit(t, fx, driver, SubmitDocumentRequest{
		Kind: DocCriminalRecord, FileURL: "https://f/casier.jpg", ExpiresAt: inDays(90),
	})
	assert.Equal(t, DocCriminalRecord, d.Kind)
	assert.Equal(t, DocPending, d.State)
}

// ⚠️ ET LA RÈGLE NE S'APPLIQUE QU'AU CASIER. On ne resserre pas une règle
// existante dans le même geste qu'on en ajoute une : une carte grise sans date
// est acceptée depuis des mois, et la refuser aujourd'hui casserait des dépôts
// qui marchaient.
func TestOnlyTheCriminalRecordDemandsAnExpiry(t *testing.T) {
	assert.True(t, NeedsExpiry(DocCriminalRecord))
	for _, kind := range []string{
		DocIDCard, DocSelfie, DocLicence, DocRegistration,
		DocInsurance, DocInspection, DocVehicleFront,
	} {
		assert.False(t, NeedsExpiry(kind), "%s ne doit pas devenir obligatoire ici", kind)
	}
}

// ⚠️ LE CASIER ET LE SELFIE SONT DES PIÈCES DE LA PERSONNE, pas du véhicule.
// Les attacher à une moto les aurait rendus à refaire à chaque changement de
// véhicule — et aurait permis à quelqu'un d'être « vérifié » sur une moto et
// pas sur l'autre.
func TestTheCriminalRecordAndTheSelfieBelongToThePerson(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()

	for _, kind := range []string{DocCriminalRecord, DocSelfie} {
		_, err := fx.svc.SubmitDocument(ctx, driver, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/x.jpg", VehicleID: vehicle, ExpiresAt: inDays(90),
		})
		require.Error(t, err, "%s n'appartient pas à un véhicule", kind)
	}
}

// ⚠️ TROIS PHOTOS, TROIS PIÈCES — et c'est ce qui les empêche de s'écraser. Une
// pièce porte UNE image et le dépôt remplace celle du même type : un type unique
// « photos » aurait fait que l'arrière écrase l'avant, sans message ni trace.
func TestTheThreeVehiclePhotosDoNotOverwriteEachOther(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()

	for _, kind := range VehiclePhotoKinds {
		submit(t, fx, driver, SubmitDocumentRequest{
			Kind: kind, FileURL: "https://f/" + kind + ".jpg", VehicleID: vehicle,
		})
	}
	st, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)

	seen := map[string]string{}
	for _, d := range st.Documents {
		seen[d.Kind] = d.FileURL
	}
	for _, kind := range VehiclePhotoKinds {
		assert.Equal(t, "https://f/"+kind+".jpg", seen[kind],
			"%s doit avoir gardé SON image", kind)
	}
	assert.Len(t, VehiclePhotoKinds, 3)
}

// ⚠️ ET CE QUI MANQUE EST DIT ANGLE PAR ANGLE. « Il manque une photo » n'indique
// pas laquelle reprendre ; « il manque la photo de la plaque » se règle en trente
// secondes. C'est la raison d'être des trois types.
func TestAMissingVehiclePhotoSaysWhichAngle(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()
	admin := newAdmin()

	// L'avant seulement, validé.
	d := submit(t, fx, driver, SubmitDocumentRequest{
		Kind: DocVehicleFront, FileURL: "https://f/av.jpg", VehicleID: vehicle,
	})
	_, err := fx.svc.ReviewDocument(ctx, admin, d.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)

	st, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)
	assert.NotContains(t, st.Missing, DocVehicleFront+":"+vehicle)
	assert.Contains(t, st.Missing, DocVehicleRear+":"+vehicle)
	assert.Contains(t, st.Missing, DocVehicleSide+":"+vehicle)
}

// ⚠️ UN VÉHICULE NON MOTORISÉ N'ATTEND AUCUN PAPIER — MAIS IL ATTEND UNE PHOTO.
// C'est le seul endroit du paquet où le non-motorisé demande PLUS, pas moins, et
// la raison est qu'IL N'A PAS DE PLAQUE : pour une moto, « AB-1234-CD »
// identifie l'engin ; un vélo n'a rien de tel, et la photo est la seule façon
// de dire à un client ce qu'il doit chercher dans la rue.
//
// ⚠️ CE TEST AFFIRMAIT L'INVERSE, avec ce motif : « y faire une exception aurait
// mis en défaut tous les cyclistes déjà inscrits ». Le motif tenait debout, et
// ce qui l'a renversé est une VÉRIFICATION : rien, dans aucune des deux
// verticales, ne conditionne le dispatch à `compliant` — on l'a cherché. « En
// défaut » est donc une liste de choses à envoyer, pas une sanction : personne
// ne cesse de travailler parce qu'il manque la photo de son vélo. Si un jour le
// dispatch lit ce drapeau, c'est CETTE décision qu'il faudra reprendre.
//
// ⚠️ UNE SEULE PHOTO, ET C'EST LE CÔTÉ. Trois vues d'un vélo seraient trois fois
// le même objet, et `vehicle_front` est documenté comme « plaque lisible » —
// une attente qu'un vélo ne peut pas satisfaire.
func TestAnUnmotorisedVehicleExpectsItsPhotoAndNoPaper(t *testing.T) {
	bike := VehicleRef{ID: "b", Motorised: false}
	assert.Equal(t, []string{DocVehicleSide}, KindsFor(bike))
	// Aucun PAPIER : ni carte grise, ni assurance, ni contrôle technique.
	for _, kind := range []string{DocRegistration, DocInsurance, DocInspection} {
		assert.NotContains(t, KindsFor(bike), kind, "un vélo n'a pas de %s", kind)
	}
	moto := VehicleRef{ID: "m", Motorised: true}
	for _, kind := range VehiclePhotoKinds {
		assert.Contains(t, KindsFor(moto), kind)
	}
}

// ⚠️ ET LES PHOTOS DE CONFORMITÉ NE SONT PAS LA GALERIE DU VÉHICULE. Celle-ci
// (`images[]`) sert à ce qu'un passager reconnaisse la voiture qui arrive ;
// celles-ci sont des pièces qu'un humain REGARDE et valide. Les confondre aurait
// fait d'une photo choisie par le chauffeur une preuve de conformité.
func TestVehiclePhotosAreReviewedPapersNotAGallery(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	ctx := context.Background()

	d := submit(t, fx, driver, SubmitDocumentRequest{
		Kind: DocVehicleFront, FileURL: "https://f/av.jpg", VehicleID: vehicle,
	})
	// Déposée = `pending`, comme toute pièce : personne ne l'a encore regardée.
	assert.Equal(t, DocPending, d.State)

	admin := newAdmin()
	out, err := fx.svc.ReviewDocument(ctx, admin, d.ID,
		ReviewDocumentRequest{Status: DocRejected, Reason: "plaque illisible"})
	require.NoError(t, err)
	assert.Equal(t, DocRejected, out.State)
	assert.Equal(t, "plaque illisible", out.RejectedReason)
}

// --- LES LIBELLÉS -------------------------------------------------------

// ⚠️ CHAQUE TYPE ATTENDU DOIT AVOIR UN NOM EN CLAIR, DANS LES DEUX LANGUES. La
// table vivait en double dans les deux verticales : ajouter un type obligeait à
// le déclarer à trois endroits, et le troisième affichait `criminal_record` à un
// exploitant — un identifiant technique dans une notification, sur l'écran de
// quelqu'un qui doit décider vite. Ce test est ce qui remplace la discipline.
func TestEveryExpectedKindHasAPlainName(t *testing.T) {
	kinds := append([]string{DocLicence}, PersonKinds...)
	kinds = append(kinds, VehicleKinds...)
	// ⚠️ ON VÉRIFIE LA PRÉSENCE DANS LA TABLE, et non que le nom DIFFÈRE de la
	// clé. Ma première version comparait les deux et accusait `insurance`, dont
	// le libellé anglais est précisément « insurance » : un test qui crie à tort
	// finit par être ignoré, et c'est alors le vrai manque qui passe.
	for _, kind := range kinds {
		for _, lang := range []string{"fr", "en"} {
			name, ok := labels[lang][kind]
			assert.True(t, ok,
				"%s n'a pas de nom en %s — il s'afficherait tel quel dans une alerte", kind, lang)
			assert.NotEmpty(t, name, "%s en %s", kind, lang)
			assert.Equal(t, name, Label(kind, lang))
		}
	}
}

// ⚠️ ET LE REPLI EST LA CLÉ, PAS UN BLANC. Un libellé vide ferait une alerte qui
// dit « pièce déposée : » et personne ne saurait laquelle.
func TestAnUnknownKindFallsBackToItsKey(t *testing.T) {
	assert.Equal(t, "ce_qui_vient", Label("ce_qui_vient", "fr"))
	// Langue inconnue ou étiquette complète : le français, puis la langue.
	assert.Equal(t, labels["fr"][DocSelfie], Label(DocSelfie, "pt"))
	assert.Equal(t, labels["fr"][DocSelfie], Label(DocSelfie, ""))
	assert.Equal(t, labels["en"][DocSelfie], Label(DocSelfie, "en-GB"))
}
