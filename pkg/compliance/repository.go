package compliance

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository stores the compliance papers of ONE vertical.
//
// ⚠️ Le nom de la collection est un ARGUMENT : livreurs et chauffeurs VTC sont
// deux populations disjointes, et leurs pièces ne se lisent jamais ensemble.
// Les mêler obligerait deux services à écrire dans la même table.
type Repository struct{ documents *mongo.Collection }

// NewRepository builds the store on a collection the vertical names.
func NewRepository(m *db.Mongo, collection string) *Repository {
	return &Repository{documents: m.Collection(collection)}
}

// UpsertDocument replaces the paper of this KIND for this owner.
//
// Un dépôt REMPLACE le précédent : une assurance renouvelée n'ajoute pas une
// seconde ligne, elle prend la place de l'ancienne. Garder les deux ferait
// hésiter chaque lecture sur laquelle fait foi.
//
// ⚠️ Le remplacement repasse la pièce en `pending` : une image nouvelle est
// une image que personne n'a regardée, même si l'ancienne était validée.
func (r *Repository) UpsertDocument(ctx context.Context, d *Document) (*Document, error) {
	now := time.Now().UTC()
	filter := bson.M{"owner_id": d.OwnerID, "kind": d.Kind}
	if d.VehicleID != nil {
		filter["vehicle_id"] = *d.VehicleID
	} else {
		filter["vehicle_id"] = bson.M{"$exists": false}
	}
	set := bson.M{
		"file_url":   d.FileURL,
		"status":     DocPending,
		"updated_at": now,
	}
	if d.ExpiresAt != nil {
		set["expires_at"] = *d.ExpiresAt
	}
	unset := bson.M{"reviewed_by": "", "reviewed_at": "", "rejected_reason": ""}
	if d.ExpiresAt == nil {
		unset["expires_at"] = ""
	}
	update := bson.M{
		"$set":         set,
		"$unset":       unset,
		"$setOnInsert": bson.M{"owner_id": d.OwnerID, "kind": d.Kind, "created_at": now},
	}
	if d.VehicleID != nil {
		update["$setOnInsert"].(bson.M)["vehicle_id"] = *d.VehicleID
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var out Document
	if err := r.documents.FindOneAndUpdate(ctx, filter, update, opts).Decode(&out); err != nil {
		return nil, fmt.Errorf("delivery: upsert document: %w", err)
	}
	return &out, nil
}

// DocumentsByAgent returns every paper of a driver — his own and his vehicles'.
func (r *Repository) DocumentsByOwner(ctx context.Context, ownerID primitive.ObjectID) ([]Document, error) {
	cur, err := r.documents.Find(ctx, bson.M{"owner_id": ownerID},
		options.Find().SetSort(bson.D{{Key: "kind", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("delivery: documents by agent: %w", err)
	}
	var out []Document
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("delivery: decode documents: %w", err)
	}
	return out, nil
}

// DocumentByID returns one paper, or nil when it does not exist.
func (r *Repository) DocumentByID(ctx context.Context, id primitive.ObjectID) (*Document, error) {
	var d Document
	err := r.documents.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: document by id: %w", err)
	}
	return &d, nil
}

// ReviewDocument records an administrator's decision.
func (r *Repository) ReviewDocument(ctx context.Context, id, reviewer primitive.ObjectID, status, reason string) (*Document, error) {
	now := time.Now().UTC()
	set := bson.M{"status": status, "reviewed_by": reviewer, "reviewed_at": now, "updated_at": now}
	unset := bson.M{}
	if status == DocRejected {
		set["rejected_reason"] = reason
	} else {
		// Une pièce validée ne garde pas le motif de son refus précédent : il
		// se lirait comme un reproche sur une pièce désormais conforme.
		unset["rejected_reason"] = ""
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var out Document
	err := r.documents.FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&out)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: review document: %w", err)
	}
	return &out, nil
}

// PendingOrExpiredDocuments lists what an operator must look at, oldest first.
//
// ⚠️ C'est la CONTREPARTIE de la décision de ne rien bloquer automatiquement.
// Sans cette liste, « l'exploitation suspend à la main » veut dire « personne
// ne suspend », parce que personne ne sait qui est en défaut.
//
// Le filtre est posé par la BASE : `pending`, `rejected`, et les pièces
// validées dont la date est dépassée ou proche. Trier une liste complète côté
// application coûterait une lecture de toute la collection à chaque ouverture
// du tableau de bord.
func (r *Repository) PendingOrExpiredDocuments(ctx context.Context, now time.Time, limit int) ([]Document, error) {
	filter := bson.M{"$or": []bson.M{
		{"status": bson.M{"$in": []string{DocPending, DocRejected}}},
		{"status": DocValid, "expires_at": bson.M{"$lte": now.AddDate(0, 0, ExpiryWarningDays)}},
	}}
	cur, err := r.documents.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "expires_at", Value: 1}, {Key: "created_at", Value: 1}}).
			SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("delivery: compliance list: %w", err)
	}
	var out []Document
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("delivery: decode compliance list: %w", err)
	}
	return out, nil
}
