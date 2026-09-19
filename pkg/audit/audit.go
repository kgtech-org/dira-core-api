// Package audit records append-only audit log entries for sensitive actions.
// Every module calls Record on token operations, payment confirmations,
// status validations, etc.
//
// ⚠️ UN SEUL JOURNAL POUR TOUTE LA PLATEFORME, tenu par le socle. Chaque
// service écrivait dans sa propre collection `audit_logs`, et la console ne
// lisait que celle de la livraison : une suspension de compte (socle), une
// immobilisation de véhicule (courses) n'apparaissaient nulle part. Les
// verticales enregistrent désormais par le socle (`NewRemoteRecorder` →
// `POST /internal/audit`), chaque entrée porte le `service` qui l'a écrite,
// et `GET /admin/audit` du socle rend tout, d'un seul tenant.
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
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	// Service est le SERVICE qui a écrit l'entrée — `core`, `vtc`, `food`.
	// Vide sur les entrées d'avant le journal unique (livraison).
	Service   string    `bson:"service,omitempty" json:"service,omitempty"`
	ActorID   string    `bson:"actor_id" json:"actor_id"`
	ActorRole string    `bson:"actor_role" json:"actor_role"`
	Action    string    `bson:"action" json:"action"` // e.g. "token.consume", "merchant.validate"
	Resource  Resource  `bson:"resource" json:"resource"`
	Before    any       `bson:"before,omitempty" json:"before,omitempty"`
	After     any       `bson:"after,omitempty" json:"after,omitempty"`
	IP        string    `bson:"ip,omitempty" json:"ip,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type Resource struct {
	Type string `bson:"type" json:"type"`
	ID   string `bson:"id" json:"id"`
}

// Services.
const (
	ServiceCore = "core"
	ServiceVTC  = "vtc"
	ServiceFood = "food"
)

// Sink ships an entry to the core — implemented by a vertical's core bridge.
type Sink interface {
	ShipAudit(ctx context.Context, e Entry) error
}

// Recorder writes audit entries. Failures are logged, never returned: an
// audit write must not break the business operation it documents.
//
// Deux formes, un seul type : le socle écrit dans SA collection, une
// verticale expédie au socle. Le même `*Recorder` circule dans les deux
// câblages, et aucun module n'a à savoir où le journal habite.
type Recorder struct {
	service string
	col     *mongo.Collection
	sink    Sink
}

// NewRecorder writes locally — le socle, propriétaire du journal.
func NewRecorder(database *mongo.Database) *Recorder {
	return &Recorder{service: ServiceCore, col: database.Collection(Collection)}
}

// NewRemoteRecorder ships every entry to the core, stamped with `service`.
func NewRemoteRecorder(service string, sink Sink) *Recorder {
	return &Recorder{service: service, sink: sink}
}

// Record inserts an audit entry, resolving the actor from the context.
func (r *Recorder) Record(ctx context.Context, action, resourceType, resourceID string, before, after any) {
	actorID, _ := auth.UserFromContext(ctx)
	actorRole, _ := auth.RoleFromContext(ctx)
	entry := Entry{
		Service:   r.service,
		ActorID:   actorID,
		ActorRole: actorRole,
		Action:    action,
		Resource:  Resource{Type: resourceType, ID: resourceID},
		Before:    before,
		After:     after,
		CreatedAt: time.Now().UTC(),
	}
	if r.sink != nil {
		if err := r.sink.ShipAudit(ctx, entry); err != nil {
			slog.ErrorContext(ctx, "audit: ship failed", "service", r.service, "action", action, "error", err)
		}
		return
	}
	r.Store(ctx, entry)
}

// Store inserts an entry AS IS — ce que le socle fait d'une entrée expédiée
// par une verticale : l'acteur et l'instant sont ceux de là-bas.
func (r *Recorder) Store(ctx context.Context, entry Entry) {
	if r.col == nil {
		slog.ErrorContext(ctx, "audit: no collection to store into", "action", entry.Action)
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if _, err := r.col.InsertOne(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit: record failed", "action", entry.Action, "error", err)
	}
}
