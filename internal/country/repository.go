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

// One rend l'installation d'un pays, ou `false` s'il n'y en a pas.
func (r *Repository) One(ctx context.Context, code string) (Installation, bool, error) {
	var out Installation
	err := r.countries.FindOne(ctx, bson.M{"_id": code}).Decode(&out)
	switch {
	case err == mongo.ErrNoDocuments:
		return Installation{}, false, nil
	case err != nil:
		return Installation{}, false, fmt.Errorf("country: one: %w", err)
	}
	return out, true, nil
}

// SetBasemap règle le fond par défaut du pays.
func (r *Repository) SetBasemap(ctx context.Context, code, basemap string) error {
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$set": bson.M{"maps.basemap": basemap, "updated_at": time.Now().UTC()}},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set basemap: %w", err)
	}
	return nil
}

// SetGoogleKey pose la clé d'une plateforme. Une clé vide la RETIRE.
//
// ⚠️ ON N'ÉCRIT QUE LE CHAMP VISÉ. Réécrire tout le sous-document `maps`
// effacerait la clé d'une autre plateforme dès que deux personnes règlent le
// même pays en même temps — et personne ne s'en apercevrait avant qu'une
// application entière n'ait plus de fond de carte.
func (r *Repository) SetGoogleKey(ctx context.Context, code, platform, key, mapType string) error {
	path := "maps.google." + platform
	update := bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}}
	if key == "" {
		update["$unset"] = bson.M{path: ""}
	} else {
		set := update["$set"].(bson.M)
		set[path+".key"] = key
		set[path+".updated_at"] = time.Now().UTC()
		if mapType != "" {
			set[path+".map_type"] = mapType
		}
	}
	if _, err := r.countries.UpdateOne(ctx, bson.M{"_id": code}, update,
		options.Update().SetUpsert(true)); err != nil {
		return fmt.Errorf("country: set google key: %w", err)
	}
	return nil
}

// SetGoogleMapType change la vue d'une plateforme SANS toucher à sa clé — pour
// qu'on puisse passer un pays en satellite sans avoir à connaître la clé en
// place, ce qui obligerait à la ressortir quelque part.
func (r *Repository) SetGoogleMapType(ctx context.Context, code, platform, mapType string) error {
	_, err := r.countries.UpdateOne(ctx,
		bson.M{"_id": code, "maps.google." + platform + ".key": bson.M{"$exists": true}},
		bson.M{"$set": bson.M{
			"maps.google." + platform + ".map_type": mapType,
			"updated_at":                            time.Now().UTC(),
		}})
	if err != nil {
		return fmt.Errorf("country: set google map type: %w", err)
	}
	return nil
}
