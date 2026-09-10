// Package audit records append-only audit log entries for sensitive actions.
// Every module calls Record on token operations, payment confirmations,
// status validations, etc. The admin module exposes the search endpoint over
// the same collection.
package audit

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

const Collection = "audit_logs"

type Entry struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	ActorID   string             `bson:"actor_id"`
	ActorRole string             `bson:"actor_role"`
	Action    string             `bson:"action"` // e.g. "token.consume", "merchant.validate"
	Resource  Resource           `bson:"resource"`
	Before    any                `bson:"before,omitempty"`
	After     any                `bson:"after,omitempty"`
	IP        string             `bson:"ip,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
}

type Resource struct {
	Type string `bson:"type"`
	ID   string `bson:"id"`
}

// Recorder writes audit entries. Failures are logged, never returned: an
// audit write must not break the business operation it documents.
type Recorder struct {
	col *mongo.Collection
}

func NewRecorder(database *mongo.Database) *Recorder {
	return &Recorder{col: database.Collection(Collection)}
}

// Record inserts an audit entry, resolving the actor from the context.
func (r *Recorder) Record(ctx context.Context, action, resourceType, resourceID string, before, after any) {
	actorID, _ := auth.UserFromContext(ctx)
	actorRole, _ := auth.RoleFromContext(ctx)
	entry := Entry{
		ActorID:   actorID,
		ActorRole: actorRole,
		Action:    action,
		Resource:  Resource{Type: resourceType, ID: resourceID},
		Before:    before,
		After:     after,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := r.col.InsertOne(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit: record failed", "action", action, "error", err)
	}
}
