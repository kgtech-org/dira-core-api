package support

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository stores the tickets of ONE vertical.
type Repository struct {
	tickets  *mongo.Collection
	counters *mongo.Collection
}

// NewRepository builds the store on a collection the VERTICALE names.
//
// ⚠️ Le nom est un ARGUMENT, pas une constante : la livraison garde
// `tickets` — ses fils existants restent lisibles — et les courses ouvrent la
// leur. Les mêler obligerait deux services à écrire dans la même table pour
// une liste que la console compose de toute façon.
func NewRepository(m *db.Mongo, collection string) *Repository {
	return &Repository{tickets: m.Collection(collection), counters: m.Collection("counters")}
}

// Indexes rend les index de la collection nommée, pour le registre de la
// verticale (`internal/indexes`) — c'est lui qui les pose au démarrage.
func Indexes(collection string) []db.Index {
	return []db.Index{
		{Collection: collection, Keys: db.K("reference", 1), Unique: true},
		{Collection: collection, Keys: db.K("status", 1)},
		{Collection: collection, Keys: db.K("assigned_to", 1)},
		{Collection: collection, Keys: db.K("user_id", 1)},
		{Collection: collection, Keys: db.K("counterpart_id", 1)},
		// L'EFFACEMENT D'UN COMPTE retire ses messages des fils d'AUTRUI —
		// l'objet perdu dans sa voiture, où il a répondu. Sans cet index, la
		// purge balaie toute la collection des tickets pour trouver, le plus
		// souvent, un seul fil. Voir `PurgeOf`.
		{Collection: collection, Keys: db.K("messages.author_id", 1)},
	}
}

// NextReference atomically increments the ticket counter and returns a
// human-readable unique reference (TCK-000123).
func (r *Repository) NextReference(ctx context.Context) (string, error) {
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	err := r.counters.FindOneAndUpdate(ctx,
		bson.M{"_id": "ticket_reference"},
		bson.M{"$inc": bson.M{"seq": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return "", fmt.Errorf("support: next ticket reference: %w", err)
	}
	return fmt.Sprintf("TCK-%06d", doc.Seq), nil
}

func (r *Repository) Create(ctx context.Context, t *Ticket) error {
	if t.ID.IsZero() {
		t.ID = primitive.NewObjectID()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if _, err := r.tickets.InsertOne(ctx, t); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.Conflict("duplicate_ticket_reference", "ticket reference already exists").WithCause(err)
		}
		return fmt.Errorf("support: create ticket: %w", err)
	}
	return nil
}

func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Ticket, error) {
	var t Ticket
	if err := r.tickets.FindOne(ctx, bson.M{"_id": id}).Decode(&t); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("support: find ticket: %w", err)
	}
	return &t, nil
}

// List rend une page, du plus RÉCENT au plus ancien : un guichet lit ce qui
// vient d'arriver, et une personne relit sa dernière demande.
func (r *Repository) List(ctx context.Context, f Filter, limit int, cursor string) ([]Ticket, string, error) {
	filter := country.Restrict(ctx, bson.M{})
	if f.ParticipantID != nil {
		filter["$or"] = bson.A{
			bson.M{"user_id": *f.ParticipantID},
			bson.M{"counterpart_id": *f.ParticipantID},
		}
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Category != "" {
		filter["category"] = f.Category
	}
	if f.AssignedTo != nil {
		filter["assigned_to"] = *f.AssignedTo
	}
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": cid}
		}
	}
	cur, err := r.tickets.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("support: list tickets: %w", err)
	}
	var items []Ticket
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", fmt.Errorf("support: list tickets decode: %w", err)
	}
	if len(items) <= limit {
		return items, "", nil
	}
	items = items[:limit]
	return items, items[limit-1].ID.Hex(), nil
}

func (r *Repository) AppendMessage(ctx context.Context, ticketID primitive.ObjectID, msg Message) error {
	res, err := r.tickets.UpdateOne(ctx,
		bson.M{"_id": ticketID},
		bson.M{
			"$push": bson.M{"messages": msg},
			"$set":  bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return fmt.Errorf("support: append ticket message: %w", err)
	}
	if res.MatchedCount == 0 {
		return errNotFound
	}
	return nil
}

// Update applies the non-nil fields and returns the updated ticket.
func (r *Repository) Update(ctx context.Context, ticketID primitive.ObjectID, upd Update) (*Ticket, error) {
	set := bson.M{"updated_at": time.Now().UTC()}
	if upd.Status != nil {
		set["status"] = *upd.Status
	}
	if upd.Priority != nil {
		set["priority"] = *upd.Priority
	}
	if upd.AssignedTo != nil {
		set["assigned_to"] = *upd.AssignedTo
	}
	return r.findAndSet(ctx, ticketID, set)
}

// AnswerLostItem writes the driver's answer and moves the ticket on.
func (r *Repository) AnswerLostItem(ctx context.Context, ticketID primitive.ObjectID, found bool, note, status string, at time.Time) (*Ticket, error) {
	set := bson.M{
		"updated_at":            at,
		"lost_item.found":       found,
		"lost_item.answered_at": at,
		"status":                status,
	}
	if note != "" {
		set["lost_item.note"] = note
	}
	return r.findAndSet(ctx, ticketID, set)
}

func (r *Repository) findAndSet(ctx context.Context, ticketID primitive.ObjectID, set bson.M) (*Ticket, error) {
	var t Ticket
	err := r.tickets.FindOneAndUpdate(ctx,
		bson.M{"_id": ticketID},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&t)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("support: update ticket: %w", err)
	}
	return &t, nil
}

// PurgeOf jette les fils de support de cette personne — la part de
// l'effacement d'un compte qui vit ici.
//
// Deux gestes, parce qu'une personne est présente de deux façons :
//
//  1. LES FILS QU'ELLE A OUVERTS partent entièrement. Un ticket est fait de ce
//     qu'elle a écrit : « le livreur ne répond pas », « j'ai oublié mon sac ».
//     L'anonymiser laisserait sa plainte lisible sous un nom vide, ce qui
//     n'efface rien de ce qui compte.
//  2. LES FILS D'AUTRUI OÙ ELLE A RÉPONDU — l'objet perdu dans sa voiture, où
//     le chauffeur est partie au ticket — gardent le fil et perdent SES
//     messages. Le ticket appartient à quelqu'un d'autre, qui a le droit de le
//     relire.
//
// ⚠️ `counterpart_id` RESTE. Il désigne une ligne de compte qui existe toujours,
// anonymisée : la lecture y trouvera « Compte supprimé », ce qui est exactement
// ce qu'il faut lire. Le retirer ferait croire à un ticket sans contrepartie —
// une donnée manquante au lieu d'un compte supprimé.
//
// ⚠️ IDEMPOTENTE : l'annonce du socle arrive « au moins une fois ».
func (r *Repository) PurgeOf(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	del, err := r.tickets.DeleteMany(ctx, bson.M{"user_id": userID})
	if err != nil {
		return 0, fmt.Errorf("support: purge tickets of %s: %w", userID.Hex(), err)
	}
	upd, err := r.tickets.UpdateMany(ctx,
		bson.M{"messages.author_id": userID},
		bson.M{"$pull": bson.M{"messages": bson.M{"author_id": userID}}},
	)
	if err != nil {
		return del.DeletedCount, fmt.Errorf("support: purge messages of %s: %w", userID.Hex(), err)
	}
	return del.DeletedCount + upd.ModifiedCount, nil
}
