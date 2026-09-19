// Package support is the SUPPORT DESK of one vertical: tickets a person opens
// from the app, the thread with the operations team, and the one case that
// involves a third party — a LOST ITEM, which the driver of that trip must
// hear about at once.
//
// Extrait de `internal/admin` de la livraison, comme `pkg/chat` : les courses
// n'avaient AUCUNE porte de réclamation, et une plainte se dépose au même
// guichet quel que soit le métier. Chaque verticale monte le paquet sous sa
// base (`/food/tickets`, `/vtc/tickets`), sur sa propre collection, et lui
// prête ce qu'elle seule sait : à qui appartient une commande ou une course,
// et qui la portait.
package support

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Reference kinds — ce dont un ticket parle. Fixé à la construction du
// service, comme pour les conversations : une instance ne sert qu'une
// verticale.
const (
	RefOrder = "order"
	RefRide  = "ride"
)

// Categories.
//
// `order` et `ride` sont le MÊME motif — « quelque chose s'est mal passé
// pendant » — nommé dans le vocabulaire de chaque métier ; un service n'en
// accepte qu'un des deux. `lost_item` est le cas à part : il exige la
// référence, prévient le chauffeur, et attend sa réponse.
const (
	CategoryOrder     = "order"
	CategoryRide      = "ride"
	CategoryPayment   = "payment"
	CategoryTokens    = "tokens"
	CategoryAccount   = "account"
	CategoryLostItem  = "lost_item"
	CategoryBehaviour = "behaviour"
	CategoryOther     = "other"
)

// Priorities.
const (
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

// Statuses.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

// Ticket is one request to the support desk, with its thread.
type Ticket struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Reference string             `bson:"reference"` // human readable, unique, e.g. TCK-000123
	UserID    primitive.ObjectID `bson:"user_id"`
	// Role de la personne qui ouvre — `client`, `driver`, `merchant`. La
	// console trie dessus : une plainte de passager et une plainte de
	// chauffeur ne se traitent pas au même guichet.
	Role string `bson:"role,omitempty"`
	// Country : celui de la personne qui ouvre le ticket — la borne du
	// support de la console. Voir `pkg/country`.
	Country    string              `bson:"country,omitempty"`
	Category   string              `bson:"category"`
	Priority   string              `bson:"priority"`
	Status     string              `bson:"status"`
	AssignedTo *primitive.ObjectID `bson:"assigned_to,omitempty"` // support agent (admin)

	// La RÉFÉRENCE — la commande ou la course dont on parle. Facultative
	// pour une question de compte ou de paiement, OBLIGATOIRE pour un objet
	// perdu : on ne cherche pas un sac sans savoir dans quelle voiture.
	RefKind string              `bson:"ref_kind,omitempty"`
	RefID   *primitive.ObjectID `bson:"ref_id,omitempty"`
	// LegacyOrderID : la livraison écrivait `order_id` avant l'extraction.
	// LU pour les tickets existants, jamais écrit.
	LegacyOrderID *primitive.ObjectID `bson:"order_id,omitempty"`
	// RefLabel est la référence DITE — « Lomé Centre → Aéroport, 19 sept.
	// 14:02 » — figée à l'ouverture pour que le fil reste lisible même si
	// la course est purgée.
	RefLabel string `bson:"ref_label,omitempty"`
	// CounterpartID est le compte de celui qui PORTAIT la référence — le
	// chauffeur, le livreur. Posé pour un objet perdu : il devient partie au
	// ticket, le lit, y répond.
	CounterpartID *primitive.ObjectID `bson:"counterpart_id,omitempty"`

	LostItem *LostItem `bson:"lost_item,omitempty"`

	Messages  []Message `bson:"messages"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// LostItem is what was forgotten, and what the driver said about it.
type LostItem struct {
	Item    string `bson:"item"`
	Details string `bson:"details,omitempty"`
	// Found est la réponse du chauffeur — nil tant qu'il n'a pas répondu.
	// Trois états, pas deux : « pas encore regardé » n'est pas « pas
	// trouvé », et le passager doit voir la différence.
	Found      *bool      `bson:"found,omitempty"`
	AnsweredAt *time.Time `bson:"answered_at,omitempty"`
	Note       string     `bson:"note,omitempty"`
}

// Message is one message of a ticket thread.
type Message struct {
	AuthorID primitive.ObjectID `bson:"author_id"`
	// AuthorRole dit de quel côté vient le message — `client`, `driver`,
	// `merchant`, `admin`. Vide sur les fils écrits avant l'extraction.
	AuthorRole string    `bson:"author_role,omitempty"`
	Body       string    `bson:"body"`
	At         time.Time `bson:"at"`
}

// Update carries the admin-editable fields (nil = unchanged).
type Update struct {
	Status     *string
	Priority   *string
	AssignedTo *primitive.ObjectID
}

// Filter narrows a listing.
type Filter struct {
	// ParticipantID : les tickets ouverts PAR cette personne ou qui la
	// concernent (objet perdu dans son véhicule). Nil = tous (admin).
	ParticipantID *primitive.ObjectID
	Status        string
	Category      string
	AssignedTo    *primitive.ObjectID
}

// refID rend l'identifiant de la référence, nouveau champ ou ancien.
func (t *Ticket) refID() *primitive.ObjectID {
	if t.RefID != nil {
		return t.RefID
	}
	return t.LegacyOrderID
}
