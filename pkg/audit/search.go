package audit

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Filter narrows a search of the journal.
type Filter struct {
	Service    string
	ActorID    string
	Action     string
	ResourceID string
	From       *time.Time
	To         *time.Time
}

// Row is one entry as the console reads it — flat, JSON-plain.
type Row struct {
	ID           string    `json:"id"`
	Service      string    `json:"service,omitempty"`
	Country      string    `json:"country,omitempty"`
	ActorID      string    `json:"actor_id"`
	ActorRole    string    `json:"actor_role"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Before       any       `json:"before,omitempty"`
	After        any       `json:"after,omitempty"`
	IP           string    `json:"ip,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Search returns entries NEWEST FIRST with cursor pagination. Local
// recorders only — a remote one has nothing to read.
func (r *Recorder) Search(ctx context.Context, f Filter, limit int, cursor string) ([]Row, string, error) {
	if r.col == nil {
		return nil, "", fmt.Errorf("audit: search on a remote recorder")
	}
	// ⚠️ BORNÉ PAR LE PAYS de la requête : le journal est unique pour toute
	// la plateforme, mais on ne le lit jamais qu'au travers d'un pays.
	filter := country.Restrict(ctx, bson.M{})
	if f.Service != "" {
		filter["service"] = f.Service
	}
	if f.ActorID != "" {
		filter["actor_id"] = f.ActorID
	}
	if f.Action != "" {
		filter["action"] = f.Action
	}
	if f.ResourceID != "" {
		filter["resource.id"] = f.ResourceID
	}
	created := bson.M{}
	if f.From != nil {
		created["$gte"] = *f.From
	}
	if f.To != nil {
		created["$lte"] = *f.To
	}
	if len(created) > 0 {
		filter["created_at"] = created
	}
	if cursor != "" {
		if oid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": oid}
		}
	}
	cur, err := r.col.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("audit: search: %w", err)
	}
	var items []Entry
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", fmt.Errorf("audit: search decode: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID.Hex()
	}
	out := make([]Row, 0, len(items))
	for i := range items {
		out = append(out, toRow(&items[i]))
	}
	return out, next, nil
}

func toRow(e *Entry) Row {
	return Row{
		ID: e.ID.Hex(), Service: e.Service, Country: e.Country, ActorID: e.ActorID, ActorRole: e.ActorRole, Action: e.Action,
		ResourceType: e.Resource.Type, ResourceID: e.Resource.ID,
		Before: plainJSON(e.Before), After: plainJSON(e.After), IP: e.IP, CreatedAt: e.CreatedAt,
	}
}

// plainJSON rend lisible ce que Mongo a décodé en `any`.
//
// ⚠️ Un document écrit comme `map[string]any` revient de la base en `bson.D`
// — une LISTE ordonnée de paires — et le JSON le sert alors comme
// `[{"Key":"status","Value":"cancelled"}]` : la console recevait un tableau
// là où elle attendait un objet, et n'affichait rien.
func plainJSON(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bson.D:
		out := make(map[string]any, len(x))
		for _, e := range x {
			out[e.Key] = plainJSON(e.Value)
		}
		return out
	case bson.M:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plainJSON(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plainJSON(e)
		}
		return out
	case bson.A:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, plainJSON(e))
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, plainJSON(e))
		}
		return out
	case primitive.ObjectID:
		return x.Hex()
	case primitive.DateTime:
		return x.Time().UTC()
	default:
		return v
	}
}
