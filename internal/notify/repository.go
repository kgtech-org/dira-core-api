package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Repository struct {
	templates *mongo.Collection
	devices   *mongo.Collection
	inbox     *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		templates: m.Collection(CollectionTemplates),
		devices:   m.Collection(CollectionDevices),
		inbox:     m.Collection(CollectionInbox),
	}
}

// --- gabarits ---

// FindTemplate rend le gabarit d'une clé, ou nil.
func (r *Repository) FindTemplate(ctx context.Context, key string) (*Template, error) {
	var t Template
	err := r.templates.FindOne(ctx, bson.M{"key": key}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil // la valeur compilée prendra le relais
	}
	if err != nil {
		return nil, fmt.Errorf("notify: find template: %w", err)
	}
	return &t, nil
}

// ListTemplates rend tous les gabarits enregistrés, par clé.
func (r *Repository) ListTemplates(ctx context.Context) ([]Template, error) {
	opts := options.Find().SetSort(bson.D{{Key: "key", Value: 1}})
	cur, err := r.templates.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("notify: list templates: %w", err)
	}
	var out []Template
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode templates: %w", err)
	}
	return out, nil
}

// SaveTemplate écrit un gabarit (création ou remplacement).
func (r *Repository) SaveTemplate(ctx context.Context, t *Template) error {
	t.UpdatedAt = time.Now().UTC()
	_, err := r.templates.UpdateOne(ctx, bson.M{"key": t.Key}, bson.M{
		"$set": bson.M{
			"locales":     t.Locales,
			"description": t.Description,
			"enabled":     t.Enabled,
			"updated_at":  t.UpdatedAt,
		},
		"$setOnInsert": bson.M{"key": t.Key},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("notify: save template: %w", err)
	}
	return nil
}

// --- appareils ---

// UpsertDevice enregistre ou rafraîchit un appareil.
//
// La clé est le JETON et non l'utilisateur : c'est le téléphone qu'on joint.
// Un même compte peut en avoir plusieurs (téléphone et tablette), et un même
// téléphone peut changer de compte — auquel cas la ligne est RÉATTRIBUÉE.
// Sans cela, l'ancien utilisateur continuerait de recevoir les notifications
// du nouveau, sur un appareil qui n'est plus le sien.
func (r *Repository) UpsertDevice(ctx context.Context, d *Device) error {
	now := time.Now().UTC()
	_, err := r.devices.UpdateOne(ctx, bson.M{"token": d.Token}, bson.M{
		"$set": bson.M{
			"user_id":      d.UserID,
			"platform":     d.Platform,
			"locale":       d.Locale,
			"last_seen_at": now,
		},
		"$setOnInsert": bson.M{"token": d.Token, "created_at": now},
		// Un jeton qu'on réenregistre est vivant : on relève la marque de
		// mort laissée par un échec précédent, sinon un utilisateur qui
		// rouvre l'application resterait injoignable pour toujours.
		"$unset": bson.M{"disabled_at": "", "disabled_reason": ""},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("notify: upsert device: %w", err)
	}
	return nil
}

// DeleteDevice retire un appareil (déconnexion).
func (r *Repository) DeleteDevice(ctx context.Context, userID primitive.ObjectID, token string) error {
	_, err := r.devices.DeleteOne(ctx, bson.M{"token": token, "user_id": userID})
	if err != nil {
		return fmt.Errorf("notify: delete device: %w", err)
	}
	return nil
}

// ActiveDevices rend les appareils JOIGNABLES d'un utilisateur.
func (r *Repository) ActiveDevices(ctx context.Context, userID primitive.ObjectID) ([]Device, error) {
	cur, err := r.devices.Find(ctx, bson.M{
		"user_id":     userID,
		"disabled_at": bson.M{"$exists": false},
	})
	if err != nil {
		return nil, fmt.Errorf("notify: active devices: %w", err)
	}
	var out []Device
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode devices: %w", err)
	}
	return out, nil
}

// DisableDevice marque un jeton que FCM a déclaré mort.
//
// Marqué et NON supprimé : le support doit pouvoir répondre « votre téléphone
// ne reçoit plus depuis le 3 mars », et non « je ne vois aucun appareil ».
func (r *Repository) DisableDevice(ctx context.Context, token, reason string) error {
	_, err := r.devices.UpdateOne(ctx, bson.M{"token": token}, bson.M{"$set": bson.M{
		"disabled_at":     time.Now().UTC(),
		"disabled_reason": reason,
	}})
	if err != nil {
		return fmt.Errorf("notify: disable device: %w", err)
	}
	return nil
}
