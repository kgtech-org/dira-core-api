package user

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Genres acceptés. Une liste OUVERTE serait ingérable pour des statistiques,
// une liste fermée à deux valeurs exclut ; `other` et l'absence de réponse
// couvrent le reste, et personne n'est forcé de répondre.
const (
	GenderFemale = "female"
	GenderMale   = "male"
	GenderOther  = "other"
)

// Thèmes de l'application.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Preferences sont les réglages d'un compte.
//
// Un sous-document sur l'utilisateur plutôt qu'une collection : ils se lisent
// TOUJOURS avec le compte, jamais seuls, et une collection séparée
// coûterait une lecture de plus à chaque ouverture de l'application pour
// quatre champs.
type Preferences struct {
	// Notifications par CATÉGORIE. Un client qui coupe les nouveautés doit
	// continuer de recevoir « votre livreur est en bas » : tout couper d'un
	// bloc ferait perdre les messages qui comptent avec ceux qui gênent.
	//
	// Pointeurs : nil = jamais réglé, donc ACTIF. Un booléen nu rendrait
	// « désactivé » indiscernable de « pas encore choisi », et une mise à
	// jour partielle couperait tout ce qu'elle ne mentionne pas.
	OrderUpdates  *bool `bson:"notif_order_updates,omitempty" json:"order_updates,omitempty"`
	ChatMessages  *bool `bson:"notif_chat_messages,omitempty" json:"chat_messages,omitempty"`
	Promotions    *bool `bson:"notif_promotions,omitempty" json:"promotions,omitempty"`
	TombolaAlerts *bool `bson:"notif_tombola,omitempty" json:"tombola,omitempty"`

	Theme string `bson:"theme,omitempty" json:"theme,omitempty"`
	// Locale est la langue CHOISIE dans l'application. Elle prime sur celle
	// que déclare le téléphone : un utilisateur qui bascule en anglais dans
	// nos réglages l'a fait exprès.
	Locale string `bson:"locale,omitempty" json:"locale,omitempty"`
	Sounds *bool  `bson:"sounds,omitempty" json:"sounds,omitempty"`
	// PaymentProvider est l'opérateur mobile money que le client utilise
	// d'habitude — Orange Money, Wave, MTN, Moov. Mémorisé pour être
	// PRÉSÉLECTIONNÉ, jamais pour payer tout seul : aucun jeton de paiement
	// n'est conservé, et chaque paiement passe par l'opérateur comme la
	// première fois.
	//
	// C'est la seule forme de « moyen de paiement enregistré » qui ne demande
	// pas de garder des données de paiement chez nous.
	PaymentProvider string `bson:"payment_provider,omitempty" json:"payment_provider,omitempty"`
}

// Allows dit si une catégorie de notification est autorisée.
//
// L'ABSENCE vaut OUI : un compte créé avant ces réglages, ou qui n'y a jamais
// touché, doit continuer de recevoir ce qu'il recevait.
func (p *Preferences) Allows(category string) bool {
	if p == nil {
		return true
	}
	var v *bool
	switch category {
	case CategoryOrderUpdates:
		v = p.OrderUpdates
	case CategoryChatMessages:
		v = p.ChatMessages
	case CategoryPromotions:
		v = p.Promotions
	case CategoryTombola:
		v = p.TombolaAlerts
	default:
		return true
	}
	return v == nil || *v
}

// Catégories de notification, telles que l'écran de préférences les présente.
const (
	CategoryOrderUpdates = "order_updates"
	CategoryChatMessages = "chat_messages"
	CategoryPromotions   = "promotions"
	CategoryTombola      = "tombola"
)

// Address is a saved delivery address.
//
// Une COLLECTION et non un tableau sur l'utilisateur : une adresse se
// référence depuis une commande, se renomme, se supprime, et un tableau
// imbriqué rendrait chacun de ces gestes une réécriture du document entier.
type Address struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	UserID primitive.ObjectID `bson:"user_id"`
	// Label est le nom que l'utilisateur lui donne — « Maison », « Bureau ».
	Label   string     `bson:"label"`
	Address string     `bson:"address"`
	Geo     [2]float64 `bson:"geo"` // [lng, lat]
	// Details est ce qui ne tient pas dans une adresse et qui fait pourtant
	// trouver la porte : « portail bleu », « 2e étage, gauche ». C'est
	// exactement ce que le client répète au livreur à chaque commande.
	Details string `bson:"details,omitempty"`
	// IsDefault : UNE seule adresse par compte. Poser celle-ci retire le
	// drapeau de l'ancienne, dans la même opération.
	IsDefault bool      `bson:"is_default,omitempty"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// CollectionAddresses is the MongoDB collection backing saved addresses.
const CollectionAddresses = "user_addresses"

// MaxAddresses borne le carnet. Au-delà, ce n'est plus un carnet d'adresses,
// et la liste devient plus longue à parcourir que l'adresse à retaper.
const MaxAddresses = 20
