package faults

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// Repository lit et écrit les pannes.
type Repository struct{ col *mongo.Collection }

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// Record range une occurrence : elle CRÉE la panne ou l'INCRÉMENTE.
//
// ⚠️ UN SEUL ALLER-RETOUR, par `upsert`. Lire puis écrire aurait ouvert une
// course entre deux occurrences simultanées — et c'est exactement quand une
// panne se répète que deux requêtes tombent en même temps.
func (r *Repository) Record(ctx context.Context, f obs.Fault, version string) error {
	now := f.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	set := bson.M{
		"service": f.Service, "kind": f.Kind, "message": f.Message,
		"last_seen": now, "version": version,
	}
	// Ce qui décrit la DERNIÈRE occurrence : on l'écrase, c'est la plus
	// utile — celle qu'on peut encore rejouer.
	for k, v := range map[string]any{
		"stack": f.Stack, "route": f.Route, "method": f.Method,
		"request_id": f.RequestID, "country": f.Country, "user_id": f.UserID,
	} {
		if s, ok := v.(string); ok && s != "" {
			set[k] = s
		}
	}
	if f.Status > 0 {
		set["status"] = f.Status
	}
	_, err := r.col.UpdateOne(ctx,
		bson.M{"fingerprint": f.Fingerprint},
		bson.M{
			"$set":         set,
			"$inc":         bson.M{"count": 1},
			"$setOnInsert": bson.M{"fingerprint": f.Fingerprint, "first_seen": now},
			// ⚠️ UNE PANNE QUI REVIENT SE ROUVRE. Une résolution qui tient
			// malgré les faits est un mensonge qu'on relit chaque semaine.
			"$unset": bson.M{"resolved_at": "", "resolved_by": ""},
		},
		options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("faults: record: %w", err)
	}
	return nil
}

// Filter borne une liste de pannes.
type Filter struct {
	Service  string
	Kind     string
	Resolved string // "" (tout) | "open" | "resolved"
	Since    time.Time
}

// List rend les pannes, LA PLUS RÉCENTE D'ABORD.
//
// ⚠️ PAR DERNIÈRE OCCURRENCE, pas par nombre. Une panne qui arrive trois mille
// fois par jour depuis six mois est connue ; celle qui vient d'apparaître est
// celle qu'on cherche.
func (r *Repository) List(ctx context.Context, f Filter, limit int) ([]Fault, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	filter := bson.M{}
	if f.Service != "" {
		filter["service"] = f.Service
	}
	if f.Kind != "" {
		filter["kind"] = f.Kind
	}
	switch f.Resolved {
	case "open":
		filter["resolved_at"] = nil
	case "resolved":
		filter["resolved_at"] = bson.M{"$ne": nil}
	}
	if !f.Since.IsZero() {
		filter["last_seen"] = bson.M{"$gte": f.Since}
	}
	cur, err := r.col.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "last_seen", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("faults: list: %w", err)
	}
	var out []Fault
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("faults: list: %w", err)
	}
	return out, nil
}

// ByFingerprint rend UNE panne.
func (r *Repository) ByFingerprint(ctx context.Context, fp string) (*Fault, error) {
	var out Fault
	err := r.col.FindOne(ctx, bson.M{"fingerprint": fp}).Decode(&out)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("faults: by fingerprint: %w", err)
	}
	return &out, nil
}

// Resolve marque une panne comme corrigée.
func (r *Repository) Resolve(ctx context.Context, fp, by string) (bool, error) {
	now := time.Now().UTC()
	res, err := r.col.UpdateOne(ctx, bson.M{"fingerprint": fp},
		bson.M{"$set": bson.M{"resolved_at": now, "resolved_by": by}})
	if err != nil {
		return false, fmt.Errorf("faults: resolve: %w", err)
	}
	return res.MatchedCount == 1, nil
}

// Counts rend le nombre de pannes OUVERTES par service — ce qu'une mesure
// expose, et ce qu'une alerte surveille.
func (r *Repository) Counts(ctx context.Context) (map[string]int, error) {
	cur, err := r.col.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"resolved_at": nil}}},
		{{Key: "$group", Value: bson.M{"_id": "$service", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("faults: counts: %w", err)
	}
	var rows []struct {
		ID string `bson:"_id"`
		N  int    `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("faults: counts: %w", err)
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.ID] = row.N
	}
	return out, nil
}
