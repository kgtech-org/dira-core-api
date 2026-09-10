package payment

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// PaymentRow is one payment, as the back-office reads it.
//
// ⚠️ `ref_id` sort BRUT : le socle ne sait pas si l'identifiant désigne une
// commande de repas ou une course. Seule la verticale qui a déclenché le
// paiement peut le rattacher à quelque chose de lisible.
type PaymentRow struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Purpose   string    `json:"purpose"`
	RefID     string    `json:"ref_id"`
	Provider  string    `json:"provider"`
	Amount    int       `json:"amount"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// List pages payments, newest first, with optional status and purpose filters.
func (r *Repository) List(ctx context.Context, status, purpose, cursor string, limit int) ([]PaymentRow, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	if purpose != "" {
		filter["purpose"] = purpose
	}
	// Un curseur illisible rend la première page plutôt qu'une erreur : c'est
	// le comportement attendu d'une pagination qu'on relance.
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": cid}
		}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit + 1))
	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var docs []struct {
		ID        primitive.ObjectID `bson:"_id"`
		UserID    primitive.ObjectID `bson:"user_id"`
		Purpose   string             `bson:"purpose"`
		RefID     primitive.ObjectID `bson:"ref_id"`
		Provider  string             `bson:"provider"`
		Amount    int                `bson:"amount"`
		Currency  string             `bson:"currency"`
		Status    string             `bson:"status"`
		CreatedAt time.Time          `bson:"created_at"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(docs) > limit {
		docs = docs[:limit]
		next = docs[limit-1].ID.Hex()
	}
	out := make([]PaymentRow, 0, len(docs))
	for _, d := range docs {
		out = append(out, PaymentRow{
			ID: d.ID.Hex(), UserID: d.UserID.Hex(), Purpose: d.Purpose,
			RefID: d.RefID.Hex(), Provider: d.Provider, Amount: d.Amount,
			Currency: d.Currency, Status: d.Status, CreatedAt: d.CreatedAt,
		})
	}
	return out, next, nil
}
