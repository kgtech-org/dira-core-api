package rating

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// errAlreadyRated is returned when a client rates the same target twice for
// the same order.
var errAlreadyRated = apperr.Conflict("already_rated", "this order has already been rated")

type Repository struct {
	col *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// Insert records one score.
//
// L'unicité (commande, cible) est portée par l'INDEX : deux envois simultanés
// passeraient tous les deux un contrôle applicatif, et la note compterait
// double dans la moyenne. C'est la base qui tranche, une fois.
func (r *Repository) Insert(ctx context.Context, rating *Rating) error {
	_, err := r.col.InsertOne(ctx, rating)
	if mongo.IsDuplicateKeyError(err) {
		return errAlreadyRated
	}
	if err != nil {
		return apperr.Internal(fmt.Errorf("rating: insert: %w", err))
	}
	return nil
}

// ListByTarget returns the most recent scores left on one target.
func (r *Repository) ListByTarget(ctx context.Context, targetType string, targetID any, limit int) ([]Rating, error) {
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.col.Find(ctx, bson.M{"target_type": targetType, "target_id": targetID}, opts)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("rating: list: %w", err))
	}
	var out []Rating
	if err := cur.All(ctx, &out); err != nil {
		return nil, apperr.Internal(fmt.Errorf("rating: decode: %w", err))
	}
	return out, nil
}
