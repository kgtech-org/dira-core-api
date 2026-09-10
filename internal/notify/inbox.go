package notify

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// CollectionInbox is the MongoDB collection backing the notification centre.
const CollectionInbox = "notifications"

// Categories, telles que l'écran de préférences les présente. Ce sont elles
// qu'un utilisateur coupe — jamais une clé de message : couper
// `order_delivered` sans couper `order_ready` n'aurait aucun sens pour lui.
const (
	CategoryOrderUpdates = "order_updates"
	CategoryChatMessages = "chat_messages"
	CategoryPromotions   = "promotions"
	CategoryTombola      = "tombola"
	CategoryDriverCall   = "driver_call"
)

// categoryOf range une clé de message dans sa catégorie.
//
// ⚠️ `driver_call` a sa propre catégorie ET n'est pas coupable : c'est le
// gagne-pain du livreur, et un réglage mal compris le rendrait invisible du
// dispatch sans qu'il sache pourquoi. La catégorie existe pour l'affichage,
// pas pour l'interrupteur.
func categoryOf(key string) string {
	switch key {
	case KeyChatMessage:
		return CategoryChatMessages
	case KeyDriverCall:
		return CategoryDriverCall
	case KeyMerchantNewOrder:
		// Rangée avec les mises à jour de commande : c'est ce que c'est pour
		// le marchand, et lui donner une catégorie à part lui offrirait un
		// interrupteur pour couper son gagne-pain.
		return CategoryOrderUpdates
	default:
		return CategoryOrderUpdates
	}
}

// Muteable dit si une catégorie peut être coupée par son destinataire.
func Muteable(category string) bool { return category != CategoryDriverCall }

// Inbox is one notification, kept so the user can read it again.
//
// La notification poussée ne laisse AUCUNE trace : un téléphone éteint, une
// bannière balayée, et le message n'existe plus nulle part. Le centre de
// notifications est ce qui le rend relisable — et ce qui donne un sens au
// badge de non-lus de l'écran Compte.
type Inbox struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	UserID   primitive.ObjectID `bson:"user_id"`
	Key      string             `bson:"key"`
	Category string             `bson:"category"`
	Title    string             `bson:"title"`
	Body     string             `bson:"body"`
	// Data est ce qui permet d'ouvrir la BONNE commande depuis la liste,
	// exactement comme depuis la bannière.
	Data      map[string]string `bson:"data,omitempty"`
	ReadAt    *time.Time        `bson:"read_at,omitempty"`
	CreatedAt time.Time         `bson:"created_at"`
}

// --- dépôt ---

// InsertInbox enregistre une notification.
func (r *Repository) InsertInbox(ctx context.Context, n *Inbox) error {
	if _, err := r.inbox.InsertOne(ctx, n); err != nil {
		return fmt.Errorf("notify: insert inbox: %w", err)
	}
	return nil
}

// ListInbox rend les notifications d'un compte, de la plus récente.
func (r *Repository) ListInbox(ctx context.Context, userID primitive.ObjectID, cursor primitive.ObjectID, limit int) ([]Inbox, error) {
	filter := bson.M{"user_id": userID}
	if !cursor.IsZero() {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.inbox.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("notify: list inbox: %w", err)
	}
	var out []Inbox
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode inbox: %w", err)
	}
	return out, nil
}

// CountUnread compte ce que le compte n'a pas lu — le badge de l'écran Compte.
func (r *Repository) CountUnread(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	n, err := r.inbox.CountDocuments(ctx, bson.M{
		"user_id": userID, "read_at": bson.M{"$exists": false},
	})
	if err != nil {
		return 0, fmt.Errorf("notify: count unread: %w", err)
	}
	return n, nil
}

// MarkInboxRead marque une notification lue, ou TOUTES quand id est nul.
func (r *Repository) MarkInboxRead(ctx context.Context, userID, id primitive.ObjectID, at time.Time) (int64, error) {
	filter := bson.M{"user_id": userID, "read_at": bson.M{"$exists": false}}
	if !id.IsZero() {
		filter["_id"] = id
	}
	res, err := r.inbox.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"read_at": at}})
	if err != nil {
		return 0, fmt.Errorf("notify: mark inbox read: %w", err)
	}
	return res.ModifiedCount, nil
}

// --- service ---

// ListInbox rend le centre de notifications d'un compte, avec le non-lu.
func (s *Service) ListInbox(ctx context.Context, userID, cursor string, limit int) ([]InboxResponse, int, string, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, 0, "", apperr.Validation("invalid user id").WithCause(err)
	}
	after := primitive.NilObjectID
	if cursor != "" {
		if after, err = primitive.ObjectIDFromHex(cursor); err != nil {
			return nil, 0, "", apperr.Validation("invalid cursor").WithCause(err)
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	items, err := s.repo.ListInbox(ctx, uid, after, limit)
	if err != nil {
		return nil, 0, "", err
	}
	unread, err := s.repo.CountUnread(ctx, uid)
	if err != nil {
		// Un compteur perdu ne doit pas emporter la liste elle-même.
		unread = 0
	}
	out := make([]InboxResponse, 0, len(items))
	for i := range items {
		out = append(out, toInboxResponse(items[i]))
	}
	next := ""
	if len(out) == limit && len(items) > 0 {
		next = items[len(items)-1].ID.Hex()
	}
	return out, int(unread), next, nil
}

// MarkRead marque une notification lue, ou toutes quand notificationID est vide.
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string) (int, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return 0, apperr.Validation("invalid user id").WithCause(err)
	}
	id := primitive.NilObjectID
	if notificationID != "" {
		if id, err = primitive.ObjectIDFromHex(notificationID); err != nil {
			return 0, apperr.NotFound("notification_not_found", "notification not found").WithCause(err)
		}
	}
	n, err := s.repo.MarkInboxRead(ctx, uid, id, time.Now().UTC())
	return int(n), err
}
