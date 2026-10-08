package sos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository lit et écrit les alertes.
type Repository struct{ col *mongo.Collection }

func NewRepository(db *db.Mongo) *Repository {
	return &Repository{col: db.Collection(Collection)}
}

// EnsureIndexes : la file ouverte d'abord, l'historique ensuite.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// La FILE : les alertes vivantes d'un pays, les plus récentes d'abord.
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "country", Value: 1}, {Key: "created_at", Value: -1}}},
		// ⚠️ L'ALERTE OUVERTE D'UNE PERSONNE, et c'est l'index qui tient la
		// règle « une seule à la fois ». Sans lui, cinq appuis en panique
		// balayent la collection cinq fois pendant que l'exploitation attend.
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("sos: indexes: %w", err)
	}
	return nil
}

// Insert écrit une alerte.
func (r *Repository) Insert(ctx context.Context, a *Alert) error {
	res, err := r.col.InsertOne(ctx, a)
	if err != nil {
		return fmt.Errorf("sos: insert: %w", err)
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		a.ID = oid
	}
	return nil
}

// OpenOf rend l'alerte vivante d'une personne, s'il y en a une.
//
// ⚠️ PAS DE `country.Restrict` ICI, ET LA RAISON TIENT EN UNE PHRASE : c'est SA
// propre alerte, trouvée par SON identifiant de compte. La borner au pays de la
// requête ferait rater l'alerte ouverte d'un chauffeur togolais qui roule à
// Dakar — et ce serait un doublon créé au pire moment.
func (r *Repository) OpenOf(ctx context.Context, userID primitive.ObjectID) (*Alert, error) {
	var a Alert
	err := r.col.FindOne(ctx, bson.M{
		"user_id": userID,
		"status":  bson.M{"$in": []string{StatusOpen, StatusAcknowledged}},
	}, options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sos: open of: %w", err)
	}
	return &a, nil
}

// ByID rend une alerte.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Alert, error) {
	var a Alert
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sos: by id: %w", err)
	}
	return &a, nil
}

// AppendPosition pousse un point et borne le parcours.
//
// ⚠️ `$slice: -TrailMax` GARDE LES DERNIERS. Une alerte qui dure vingt minutes
// à une position par seconde ferait un document de plusieurs mégaoctets, lu à
// chaque rafraîchissement de la console — celle-là même qu'on veut rapide.
func (r *Repository) AppendPosition(ctx context.Context, id primitive.ObjectID, p Position, now time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$push": bson.M{"trail": bson.M{"$each": []Position{p}, "$slice": -TrailMax}},
		"$set":  bson.M{"updated_at": now},
	})
	if err != nil {
		return fmt.Errorf("sos: append position: %w", err)
	}
	return nil
}

// Acknowledge marque l'alerte prise, SI elle est encore ouverte.
//
// ⚠️ LE FILTRE PORTE SUR `status: open`, et c'est ce qui rend la prise
// EXCLUSIVE. Deux opérateurs qui cliquent en même temps : le second ne modifie
// rien et l'apprend (`false`), au lieu d'écraser le nom du premier et de les
// envoyer tous les deux appeler la même personne.
func (r *Repository) Acknowledge(ctx context.Context, id, by primitive.ObjectID, now time.Time) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": StatusOpen},
		bson.M{"$set": bson.M{
			"status": StatusAcknowledged, "acknowledged_at": now,
			"acknowledged_by": by, "updated_at": now,
		}})
	if err != nil {
		return false, fmt.Errorf("sos: acknowledge: %w", err)
	}
	return res.ModifiedCount == 1, nil
}

// Close referme une alerte encore vivante.
func (r *Repository) Close(ctx context.Context, id primitive.ObjectID, by string, byID *primitive.ObjectID, outcome, resolution string, now time.Time) (*Alert, error) {
	set := bson.M{
		"status": StatusClosed, "closed_at": now, "closed_by": by,
		"outcome": outcome, "updated_at": now,
	}
	if resolution != "" {
		set["resolution"] = resolution
	}
	if byID != nil {
		set["closed_by_id"] = *byID
	}
	var a Alert
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "status": bson.M{"$in": []string{StatusOpen, StatusAcknowledged}}},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sos: close: %w", err)
	}
	return &a, nil
}

// Filter borne une liste d'alertes.
type Filter struct {
	// Live : seulement ce qui demande une action — les ouvertes, les prises, et
	// les annulées RÉCENTES par la personne elle-même.
	Live   bool
	Status string
	UserID *primitive.ObjectID
	Since  *time.Time
}

// List rend les alertes, les plus récentes d'abord.
//
// ⚠️ BORNÉE AU PAYS (`country.Restrict`) : la console de Dakar n'a pas à voir —
// ni à traiter — une alerte de Lomé. C'est aussi ce qui évite qu'un opérateur
// prenne une alerte qu'il ne peut joindre ni appeler.
func (r *Repository) List(ctx context.Context, f Filter, limit int) ([]Alert, error) {
	q := country.Restrict(ctx, listQuery(f, time.Now().UTC()))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	cur, err := r.col.Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("sos: list: %w", err)
	}
	defer cur.Close(ctx)
	out := make([]Alert, 0, limit)
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("sos: list decode: %w", err)
	}
	return out, nil
}

// listQuery construit le filtre d'une liste d'alertes.
//
// ⚠️ EXTRAITE POUR ÊTRE ÉPROUVÉE, et pas par goût de la pureté. Un filtre
// MongoDB est exactement le genre de code qui a l'air juste, qui répond sans
// erreur, et qui ne rend pas les bonnes lignes — on l'a déjà payé sur le
// verrou des rapports, où une doublure de test disait vrai pendant que la base
// disait le contraire. Ici, la ligne qui compte est celle qui garde les
// alertes ANNULÉES PAR LA PERSONNE dans la file : si elle tombe, le cas le plus
// grave (une annulation contrainte) devient invisible, et rien ne le signale.
func listQuery(f Filter, now time.Time) bson.M {
	q := bson.M{}
	switch {
	case f.Live:
		// ⚠️ LES ANNULÉES RÉCENTES RESTENT DANS LA FILE. Une annulation peut
		// être contrainte : les faire disparaître aurait rendu invisible
		// exactement le cas que ce bouton existe pour couvrir.
		q["$or"] = []bson.M{
			{"status": bson.M{"$in": []string{StatusOpen, StatusAcknowledged}}},
			{"closed_by": ClosedByRaiser, "closed_at": bson.M{"$gte": now.Add(-RecentlyCancelledFor)}},
		}
	case f.Status != "":
		q["status"] = f.Status
	}
	if f.UserID != nil {
		q["user_id"] = *f.UserID
	}
	if f.Since != nil {
		q["created_at"] = bson.M{"$gte": *f.Since}
	}
	return q
}

// CountLive compte ce qui demande une action — la pastille de la console.
func (r *Repository) CountLive(ctx context.Context) (int, error) {
	q := country.Restrict(ctx, bson.M{
		"status": bson.M{"$in": []string{StatusOpen, StatusAcknowledged}},
	})
	n, err := r.col.CountDocuments(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("sos: count live: %w", err)
	}
	return int(n), nil
}
