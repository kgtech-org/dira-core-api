package compliance

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- faux dépôt et fausse flotte ---
//
// ⚠️ Ils REPRODUISENT le vrai : le dépôt remplace la pièce du même type au lieu
// d'en ajouter une seconde, et repasse en `pending` à chaque dépôt. Un faux qui
// empilerait les pièces ferait passer au vert une règle — « une image nouvelle
// est une image que personne n'a regardée » — que la production applique.

type fakeStore struct{ documents []Document }

func (f *fakeStore) key(d *Document) string {
	return docKey(d.Kind, d.VehicleID) + ":" + d.OwnerID.Hex()
}

func (f *fakeStore) UpsertDocument(_ context.Context, d *Document) (*Document, error) {
	now := time.Now().UTC()
	for i := range f.documents {
		if f.key(&f.documents[i]) == f.key(d) {
			// Le remplacement : la pièce repasse en `pending`, et le motif de
			// refus précédent disparaît.
			f.documents[i].FileURL, f.documents[i].ExpiresAt = d.FileURL, d.ExpiresAt
			f.documents[i].Status, f.documents[i].RejectedReason = DocPending, ""
			f.documents[i].UpdatedAt = now
			out := f.documents[i]
			return &out, nil
		}
	}
	d.ID = primitive.NewObjectID()
	d.Status, d.CreatedAt, d.UpdatedAt = DocPending, now, now
	f.documents = append(f.documents, *d)
	out := *d
	return &out, nil
}

func (f *fakeStore) DocumentsByOwner(_ context.Context, ownerID primitive.ObjectID) ([]Document, error) {
	var out []Document
	for _, d := range f.documents {
		if d.OwnerID == ownerID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeStore) DocumentByID(_ context.Context, id primitive.ObjectID) (*Document, error) {
	for i := range f.documents {
		if f.documents[i].ID == id {
			out := f.documents[i]
			return &out, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ReviewDocument(_ context.Context, id, reviewer primitive.ObjectID, status, reason string) (*Document, error) {
	for i := range f.documents {
		if f.documents[i].ID != id {
			continue
		}
		now := time.Now().UTC()
		f.documents[i].Status, f.documents[i].RejectedReason = status, reason
		f.documents[i].ReviewedBy, f.documents[i].ReviewedAt = &reviewer, &now
		f.documents[i].UpdatedAt = now
		out := f.documents[i]
		return &out, nil
	}
	return nil, errDocumentNotFound
}

func (f *fakeStore) PendingOrExpiredDocuments(_ context.Context, now time.Time, limit int) ([]Document, error) {
	var out []Document
	for _, d := range f.documents {
		st := d.State(now)
		if st == DocPending || st == DocExpired || st == DocExpiring {
			out = append(out, d)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// fakeFleet tient lieu de VERTICALE : elle seule sait qui est chauffeur et à
// qui appartient un véhicule.
type fakeFleet struct {
	byUser   map[string]string   // compte -> chauffeur
	vehicles map[string][]VehicleRef // chauffeur -> véhicules
	owner    map[string]string   // véhicule -> chauffeur
	accounts map[string]string   // chauffeur -> compte
	// accountErr simule un annuaire injoignable, pour vérifier que la file
	// s'affiche quand même.
	accountErr error
}

func newFleet() *fakeFleet {
	return &fakeFleet{
		byUser:   map[string]string{},
		vehicles: map[string][]VehicleRef{},
		owner:    map[string]string{},
		accounts: map[string]string{},
	}
}

func (f *fakeFleet) DriverOf(_ context.Context, userID string) (string, error) {
	return f.byUser[userID], nil
}

func (f *fakeFleet) AccountOf(_ context.Context, driverID string) (string, error) {
	if f.accountErr != nil {
		return "", f.accountErr
	}
	return f.accounts[driverID], nil
}

func (f *fakeFleet) DriverExists(_ context.Context, driverID string) (bool, error) {
	for _, d := range f.byUser {
		if d == driverID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeFleet) VehicleOwner(_ context.Context, vehicleID string) (string, error) {
	return f.owner[vehicleID], nil
}

func (f *fakeFleet) VehiclesOf(_ context.Context, driverID string) ([]VehicleRef, error) {
	return f.vehicles[driverID], nil
}

// addVehicle déclare un véhicule MOTORISÉ à un chauffeur.
func (f *fakeFleet) addVehicle(driverID string) string {
	return f.addVehicleOf(driverID, true)
}

// addVehicleOf déclare un véhicule et dit s'il est motorisé — un vélo ou un
// livreur à pied n'attend aucune pièce.
func (f *fakeFleet) addVehicleOf(driverID string, motorised bool) string {
	vid := primitive.NewObjectID().Hex()
	f.vehicles[driverID] = append(f.vehicles[driverID], VehicleRef{ID: vid, Motorised: motorised})
	f.owner[vid] = driverID
	return vid
}

type fixture struct {
	svc   *Service
	repo  *fakeStore
	fleet *fakeFleet
}

func newFixture() *fixture {
	store := &fakeStore{}
	fleet := newFleet()
	return &fixture{svc: NewService(store, fleet), repo: store, fleet: fleet}
}

// newDriver crée un chauffeur AVEC un véhicule, et rend le COMPTE et le
// véhicule.
//
// Le compte et non l'identifiant de chauffeur : les routes du chauffeur
// partent de sa session, et c'est ce chemin-là que les tests doivent exercer.
func (f *fixture) newDriver(t *testing.T, _ int) (userID, vehicleID string) {
	t.Helper()
	userID = primitive.NewObjectID().Hex()
	driverID := primitive.NewObjectID().Hex()
	f.fleet.byUser[userID] = driverID
	f.fleet.accounts[driverID] = userID
	return userID, f.fleet.addVehicle(driverID)
}
