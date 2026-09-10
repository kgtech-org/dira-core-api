// Package jobs defines the Asynq task types and payloads shared between the
// API (enqueuers) and the worker (handlers).
package jobs

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

const (
	// TypeGenerateDishVideo produces an AI video for a store dish and updates
	// the feed_videos status (ready/failed).
	TypeGenerateDishVideo = "ai:generate_dish_video"
	// TypeMealReminder sends a meal reminder notification with an AI
	// suggestion to a client.
	TypeMealReminder = "nutrition:meal_reminder"
	// TypeTranscodeVideo re-encodes an uploaded short into a streamable MP4
	// and, when missing, extracts its thumbnail.
	TypeTranscodeVideo = "feed:transcode_video"
	// TypeRefPaid annonce à une VERTICALE qu'un paiement a abouti.
	//
	// ⚠️ La seule tâche du SOCLE, et elle existe pour découpler : le webhook
	// du prestataire dépendait de la disponibilité de la verticale. Pendant un
	// redéploiement de la livraison, le paiement d'un client échouait et
	// c'était au prestataire de réessayer — on faisait porter à l'acheteur la
	// latence de nos mises en production.
	TypeRefPaid = "core:ref_paid"
)

// RefPaidPayload porte de quoi rappeler la bonne verticale.
//
// `Purpose` route : le socle ne sait ni ce qu'est une commande, ni ce qu'est
// une course. `PaymentID` accompagne parce que la verticale en a besoin pour
// sa trace d'audit — c'est le seul moyen de remonter au prestataire depuis
// une commande contestée.
type RefPaidPayload struct {
	Purpose   string `json:"purpose"`
	RefID     string `json:"ref_id"`
	PaymentID string `json:"payment_id"`
}

type GenerateDishVideoPayload struct {
	VideoID     string   `json:"video_id"`
	StoreID     string   `json:"store_id"`
	DishID      string   `json:"dish_id,omitempty"`
	Images      []string `json:"images,omitempty"`
	Description string   `json:"description,omitempty"`
}

// TranscodeVideoPayload carries the raw upload to optimize. NeedThumbnail is
// false when the merchant supplied their own.
type TranscodeVideoPayload struct {
	VideoID       string `json:"video_id"`
	StoreID       string `json:"store_id"`
	SourceURL     string `json:"source_url"`
	NeedThumbnail bool   `json:"need_thumbnail"`
}

type MealReminderPayload struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
	Time   string `json:"time"` // "HH:MM"
}

// NewTask marshals payload into an Asynq task of the given type.
func NewTask(typ string, payload any) (*asynq.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal %s payload: %w", typ, err)
	}
	return asynq.NewTask(typ, data), nil
}
