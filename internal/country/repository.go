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

// SetSecurity enregistre la politique de sécurité d'un pays — le verrou des
// applications. Le bloc est écrit ENTIER : il est petit, et un `$set` par
// champ aurait laissé se construire, à force d'écritures partielles, un
// document dont personne ne sait plus quels champs ont été voulus.
func (r *Repository) SetSecurity(ctx context.Context, code string, sec Security) error {
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$set": bson.M{"security": sec, "updated_at": time.Now().UTC()}},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set security: %w", err)
	}
	return nil
}

// SetPrivacy enregistre la politique de confidentialité d'un pays — qui voit
// quoi de qui, métier par métier. Voir `privacy.go`.
//
// ⚠️ `upsert` : un pays jamais enregistré se règle quand même. Sans lui, un
// réglage posé avant la première ouverture du pays disparaissait sans erreur.
func (r *Repository) SetPrivacy(ctx context.Context, code string, p Privacy) error {
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$set": bson.M{"privacy": p, "updated_at": time.Now().UTC()}},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set privacy: %w", err)
	}
	return nil
}

// SetTesting réserve un pays aux essais, ou l'en sort.
func (r *Repository) SetTesting(ctx context.Context, code string, testing bool) error {
	update := bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}}
	if testing {
		update["$set"].(bson.M)["testing"] = true
	} else {
		// Sorti des essais, le champ DISPARAÎT plutôt que de valoir `false` :
		// un pays ordinaire n'a pas à porter la trace d'avoir servi aux essais.
		update["$unset"] = bson.M{"testing": ""}
	}
	if _, err := r.countries.UpdateOne(ctx, bson.M{"_id": code}, update,
		options.Update().SetUpsert(true)); err != nil {
		return fmt.Errorf("country: set testing: %w", err)
	}
	return nil
}

// SetReferral enregistre la politique de parrainage d'un pays — voir
// `referral.go`.
//
// ⚠️ Le bloc est écrit ENTIER, comme `security` et `privacy` : quatre nombres
// qui se lisent ensemble (« 1 000 au filleul, 500 au parrain, 10 filleuls
// maximum »), et un `$set` par champ aurait laissé se construire, à force
// d'écritures partielles, une politique dont personne ne sait plus quelle
// moitié a été voulue.
//
// ⚠️ `upsert` : un pays jamais enregistré se règle quand même — sinon un
// réglage posé avant la première ouverture du pays disparaissait sans erreur.
func (r *Repository) SetReferral(ctx context.Context, code string, ref Referral) error {
	_, err := r.countries.UpdateOne(ctx, bson.M{"_id": code},
		bson.M{"$set": bson.M{"referral": ref, "updated_at": time.Now().UTC()}},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("country: set referral: %w", err)
	}
	return nil
}
