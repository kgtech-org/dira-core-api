package country

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository is the MongoDB access for installed countries.
type Repository struct {
	countries *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{countries: m.Collection(Collection)}
}

// All rend toutes les installations, ouvertes ou fermées.
func (r *Repository) All(ctx context.Context) ([]Installation, error) {
	cur, err := r.countries.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("country: list: %w", err)
	}
	var out []Installation
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("country: list: %w", err)
	}
	return out, nil
}

// SetEnabled ouvre ou ferme un pays, en créant la ligne au besoin.
func (r *Repository) SetEnabled(ctx context.Context, code string, enabled bool) error {
	now := time.Now().UTC()
	set := bson.M{"enabled": enabled, "updated_at": now}
	if enabled {
		set["enabled_at"] = now
	}
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$set": set}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set enabled: %w", err)
	}
	return nil
}

// SetCurrency règle la monnaie d'un pays (vide = celle du catalogue).
func (r *Repository) SetCurrency(ctx context.Context, code, currency string) error {
	update := bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}}
	if currency == "" {
		update["$unset"] = bson.M{"currency": ""}
	} else {
		update["$set"].(bson.M)["currency"] = currency
	}
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code}, update, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set currency: %w", err)
	}
	return nil
}

// EnsureEnabled ouvre un pays s'il n'a JAMAIS été enregistré — sans rouvrir
// un pays qu'on a fermé exprès. C'est le geste du démarrage pour le pays par
// défaut.
func (r *Repository) EnsureEnabled(ctx context.Context, code string) error {
	now := time.Now().UTC()
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$setOnInsert": bson.M{"enabled": true, "enabled_at": now, "updated_at": now}},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: ensure enabled: %w", err)
	}
	return nil
}
