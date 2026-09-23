// Paquet d'exemple pour l'analyseur : un dépôt qui lit une collection bornée
// par pays de toutes les façons qu'on rencontre en vrai.
package shop

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

type Repository struct {
	orders  *mongo.Collection
	markers *mongo.Collection
}

// Bornée : le filtre passe par country.Restrict.
func (r *Repository) ListOrders(ctx context.Context, status string) {
	filter := country.Restrict(ctx, bson.M{})
	filter["status"] = status
	_, _ = r.orders.Find(ctx, filter)
}

// Bornée : un identifiant désigne un objet, qui porte son pays.
func (r *Repository) OrderByID(ctx context.Context, id string) {
	_ = r.orders.FindOne(ctx, bson.M{"_id": id})
}

// Bornée : la borne est posée par une fonction voisine.
func (r *Repository) window(ctx context.Context) bson.M {
	return country.Restrict(ctx, bson.M{"status": "done"})
}

func (r *Repository) ListDone(ctx context.Context) {
	_, _ = r.orders.Find(ctx, r.window(ctx))
}

// Bornée : le filtre est celui de l'appelant.
func (r *Repository) page(ctx context.Context, filter bson.M) {
	_, _ = r.orders.Find(ctx, filter)
}

// NON bornée : une agrégation sur toute la collection. Le `_id` du `$group`
// nomme le regroupement, il ne borne rien.
func (r *Repository) ByDay(ctx context.Context) {
	_, _ = r.orders.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"status": "done"}},
		{"$group": bson.M{"_id": "$day", "n": bson.M{"$sum": 1}}},
	})
}

// NON bornée : une liste nue.
func (r *Repository) All(ctx context.Context) {
	_, _ = r.orders.Find(ctx, bson.M{})
}

// Hors du périmètre : `markers` n'est pas une collection bornée par pays.
func (r *Repository) Markers(ctx context.Context) {
	_, _ = r.markers.Find(ctx, bson.M{})
}

// NON bornée : le `_id` est un CURSEUR de pagination, pas une borne. Il dit
// « les suivants », pas « ceux-ci ».
func (r *Repository) Page(ctx context.Context, cursor string) {
	filter := bson.M{}
	filter["_id"] = bson.M{"$lt": cursor}
	_, _ = r.orders.Find(ctx, filter)
}
