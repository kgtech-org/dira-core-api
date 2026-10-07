// Package jobs defines the Asynq task types and payloads shared between the
// API (enqueuers) and the worker (handlers).
package jobs

import (
	"encoding/json"
	"fmt"
	"time"

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
	// TypeAccountErased annonce aux VERTICALES qu'un compte vient d'être
	// effacé, pour qu'elles purgent ce qu'elles seules détiennent de cette
	// personne.
	//
	// ⚠️ EN FILE, et pas en appel direct, pour la raison inverse de
	// `TypeRefPaid` : ici personne n'attend : l'effacement du socle est DÉJÀ
	// fait et il ne sera pas défait. Si une verticale est en redéploiement au
	// moment du balayage, un appel direct perdrait la purge pour toujours — et
	// rien, nulle part, ne le dirait. La file garde le fait et le réessaie.
	TypeAccountErased = "core:account_erased"
)

// AccountErasedPayload porte l'identifiant et le TÉLÉPHONE.
//
// ⚠️ LE TÉLÉPHONE EST LÀ À CONTRECŒUR, et l'arbitrage mérite d'être lu.
//
// Cette charge vit dans Redis, et une tâche aboutie y est retenue : recopier
// un numéro à l'instant où on l'efface est exactement ce qu'un effacement ne
// devrait pas faire. Mais une verticale garde des traces CLASSÉES PAR NUMÉRO
// et non par compte — la conversation du robot WhatsApp, qui porte le numéro
// de la personne et tout ce qu'elle a écrit pour commander. Sans ce champ,
// elles resteraient là POUR TOUJOURS, et rien, nulle part, ne dirait comment
// les retrouver : l'identifiant de compte n'y apparaît pas.
//
// Trois jours dans une file contre toujours dans un journal de conversations :
// le choix se fait tout seul. C'est pour cela que cette tâche est retenue
// `AccountErasedRetention` — UNE HEURE — et non les 72 h des autres annonces.
//
// ⚠️ NI NOM NI ADRESSE, en revanche : rien d'autre que ce qui sert de CLÉ
// quelque part. Un nom ne déverrouille aucune purge.
type AccountErasedPayload struct {
	UserID string `json:"user_id"`
	// Phone est le numéro E.164 TEL QU'IL ÉTAIT, lu avant l'anonymisation du
	// compte. Peut être vide : une purge par numéro doit alors ne rien faire
	// plutôt que de deviner.
	Phone string `json:"phone,omitempty"`
}

// AccountErasedRetention est combien de temps la file garde une annonce
// d'effacement ABOUTIE.
//
// ⚠️ COURTE EXPRÈS, et c'est le prix du champ `Phone` : une heure suffit à
// répondre « la purge est-elle passée ? » pendant qu'on regarde, et ne laisse
// pas un numéro de téléphone dormir trois jours dans Redis après qu'on a
// promis de l'effacer. Les reprises, elles, ne sont pas bornées par ce
// réglage : une tâche qui échoue reste jusqu'à épuisement de ses essais.
const AccountErasedRetention = time.Hour

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
