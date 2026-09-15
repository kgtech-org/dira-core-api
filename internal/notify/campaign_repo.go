package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InsertCampaign enregistre une campagne.
func (r *Repository) InsertCampaign(ctx context.Context, c *Campaign) error {
	res, err := r.campaigns.InsertOne(ctx, c)
	if err != nil {
		return fmt.Errorf("notify: insert campaign: %w", err)
	}
	c.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

// SetCampaignData écrit les données de notification — qui portent
// l'identifiant de la campagne, connu seulement après l'insertion.
func (r *Repository) SetCampaignData(ctx context.Context, id primitive.ObjectID, data map[string]string) error {
	_, err := r.campaigns.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"data": data}})
	if err != nil {
		return fmt.Errorf("notify: set campaign data: %w", err)
	}
	return nil
}

// FindCampaign rend une campagne, ou nil.
func (r *Repository) FindCampaign(ctx context.Context, id primitive.ObjectID) (*Campaign, error) {
	var c Campaign
	err := r.campaigns.FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("notify: find campaign: %w", err)
	}
	return &c, nil
}

// ListCampaigns rend les dernières campagnes.
func (r *Repository) ListCampaigns(ctx context.Context, limit int) ([]Campaign, error) {
	cur, err := r.campaigns.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("notify: list campaigns: %w", err)
	}
	out := []Campaign{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode campaigns: %w", err)
	}
	return out, nil
}

// DueCampaigns rend les campagnes programmées dont l'heure est passée.
func (r *Repository) DueCampaigns(ctx context.Context, now time.Time) ([]Campaign, error) {
	cur, err := r.campaigns.Find(ctx, bson.M{"status": CampaignScheduled, "send_at": bson.M{"$lte": now}},
		options.Find().SetSort(bson.D{{Key: "send_at", Value: 1}}).SetLimit(20))
	if err != nil {
		return nil, fmt.Errorf("notify: due campaigns: %w", err)
	}
	var out []Campaign
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode due campaigns: %w", err)
	}
	return out, nil
}

// CampaignTransition fait passer une campagne d'un état à l'autre, SI elle
// y est encore.
//
// Compare-and-set : c'est ce qui garantit qu'une campagne ne part qu'une
// fois quand plusieurs instances du service la voient arriver à échéance au
// même instant — et qu'on n'annule pas ce qui est déjà parti.
func (r *Repository) CampaignTransition(ctx context.Context, id primitive.ObjectID, from, to string) (bool, error) {
	now := time.Now().UTC()
	set := bson.M{"status": to, "updated_at": now}
	if to == CampaignSending {
		set["started_at"] = now
	}
	res, err := r.campaigns.UpdateOne(ctx, bson.M{"_id": id, "status": from}, bson.M{"$set": set})
	if err != nil {
		return false, fmt.Errorf("notify: campaign transition: %w", err)
	}
	return res.ModifiedCount == 1, nil
}

// SaveCampaignProgress écrit les compteurs et l'issue d'une campagne.
func (r *Repository) SaveCampaignProgress(ctx context.Context, c *Campaign) error {
	_, err := r.campaigns.UpdateOne(ctx, bson.M{"_id": c.ID}, bson.M{"$set": bson.M{
		"status": c.Status, "recipients": c.Recipients, "muted": c.Muted, "archived": c.Archived,
		"devices": c.Devices, "pushed": c.Pushed, "failed": c.Failed,
		"finished_at": c.FinishedAt, "error": c.Error, "updated_at": time.Now().UTC(),
	}})
	if err != nil {
		return fmt.Errorf("notify: save campaign progress: %w", err)
	}
	return nil
}
