package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var errPaymentNotFound = apperr.NotFound("payment_not_found", "payment not found")

// Repository provides MongoDB access to the payments collection.
type Repository struct {
	col *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// Create inserts a new payment. The unique index on provider_ref enforces
// idempotency at initiation time.
func (r *Repository) Create(ctx context.Context, p *Payment) error {
	if p.ID.IsZero() {
		p.ID = primitive.NewObjectID()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if _, err := r.col.InsertOne(ctx, p); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.Conflict("duplicate_provider_ref", "a payment with this provider reference already exists").WithCause(err)
		}
		return fmt.Errorf("payment: create: %w", err)
	}
	return nil
}

// FindByID returns the payment with the given id.
func (r *Repository) FindByID(ctx context.Context, id primitive.ObjectID) (*Payment, error) {
	var p Payment
	if err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errPaymentNotFound
		}
		return nil, fmt.Errorf("payment: find by id: %w", err)
	}
	return &p, nil
}

// FindByProviderRef returns the payment carrying the given provider reference.
func (r *Repository) FindByProviderRef(ctx context.Context, ref string) (*Payment, error) {
	var p Payment
	if err := r.col.FindOne(ctx, bson.M{"provider_ref": ref}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errPaymentNotFound
		}
		return nil, fmt.Errorf("payment: find by provider ref: %w", err)
	}
	return &p, nil
}

// SetStatusIfPending atomically transitions a payment from "pending" to
// status. It returns false when the payment was not pending anymore, which is
// the idempotency gate for duplicated webhooks.
func (r *Repository) SetStatusIfPending(ctx context.Context, id primitive.ObjectID, status string) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": StatusPending},
		bson.M{"$set": bson.M{"status": status, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return false, fmt.Errorf("payment: set status: %w", err)
	}
	return res.MatchedCount > 0, nil
}
