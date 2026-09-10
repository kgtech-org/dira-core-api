package payment

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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

// RefundOrderPayment marks the succeeded payment of an order as refunded and
// returns its id.
//
// ⚠️ Ne rend l'argent NULLE PART : cette écriture dit que la plateforme
// considère la commande remboursée. L'exécution chez l'opérateur mobile reste
// à faire, et le nier ici ferait croire au client que c'est parti.
//
// Le filtre exige `status: "succeeded"` : rembourser un paiement en attente ou
// échoué n'a pas de sens, et l'index sur (purpose, ref_id) rend l'écriture
// atomique — deux résolutions de litige simultanées ne rembourseront pas deux
// fois.
func (r *Repository) RefundOrderPayment(ctx context.Context, orderID string) (string, error) {
	oid, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return "", apperr.Validation("invalid order id").WithCause(err)
	}
	var doc struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	err = r.col.FindOneAndUpdate(ctx,
		bson.M{"purpose": PurposeOrder, "ref_id": oid, "status": StatusSucceeded},
		bson.M{"$set": bson.M{"status": StatusRefunded, "updated_at": time.Now().UTC()}},
	).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", errPaymentNotFound
		}
		return "", apperr.Internal(err)
	}
	return doc.ID.Hex(), nil
}
