package chat

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

type Repository struct {
	col *mongo.Collection
}

// NewRepository builds the store on a collection the VERTICALE names.
//
// ⚠️ Le nom de la collection est un ARGUMENT, pas une constante de ce paquet :
// chaque verticale garde ses conversations chez elle. Les mêler dans une seule
// collection n'apporterait rien — une conversation de commande et une
// conversation de course ne se lisent jamais ensemble — et obligerait les deux
// services à écrire dans la même table.
func NewRepository(m *db.Mongo, collection string) *Repository {
	return &Repository{col: m.Collection(collection)}
}

func (r *Repository) Insert(ctx context.Context, m *Message) error {
	res, err := r.col.InsertOne(ctx, m)
	if err != nil {
		return apperr.Internal(fmt.Errorf("chat: insert: %w", err))
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		m.ID = id
	}
	return nil
}

// ListByOrder returns a page of the conversation, OLDEST FIRST.
//
// Une conversation se lit dans l'ordre où elle s'est tenue. La pagination
// remonte donc le fil par le haut : `after` est le dernier message déjà connu,
// ce qui rend aussi le rattrapage après une coupure — l'appelant redemande la
// suite de ce qu'il a.
func (r *Repository) ListByRef(ctx context.Context, refID primitive.ObjectID, after *primitive.ObjectID, limit int) ([]Message, error) {
	filter := bson.M{"ref_id": refID}
	if after != nil {
		filter["_id"] = bson.M{"$gt": *after}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit))
	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("chat: list: %w", err))
	}
	var out []Message
	if err := cur.All(ctx, &out); err != nil {
		return nil, apperr.Internal(fmt.Errorf("chat: decode: %w", err))
	}
	return out, nil
}

// MarkRead marks every message of the order NOT sent by `role` as read.
//
// Par RÔLE et non par identifiant d'expéditeur : c'est « tout ce que l'autre
// m'a envoyé » qu'on marque, et un livreur remplacé en cours de course
// laisserait sinon des messages que personne ne pourrait plus marquer lus.
func (r *Repository) MarkRead(ctx context.Context, refID primitive.ObjectID, role string, at time.Time) (int64, error) {
	res, err := r.col.UpdateMany(ctx,
		bson.M{"ref_id": refID, "sender_role": bson.M{"$ne": role}, "read_at": nil},
		bson.M{"$set": bson.M{"read_at": at}})
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("chat: mark read: %w", err))
	}
	return res.ModifiedCount, nil
}

// CountUnread counts what the other side sent and `role` has not read.
func (r *Repository) CountUnread(ctx context.Context, refID primitive.ObjectID, role string) (int, error) {
	n, err := r.col.CountDocuments(ctx, bson.M{
		"ref_id": refID, "sender_role": bson.M{"$ne": role}, "read_at": nil,
	})
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("chat: count unread: %w", err))
	}
	return int(n), nil
}
