// Package payment collects payments (orders and token purchases) through
// mobile-money providers hidden behind the PaymentProvider abstraction.
// Confirmation is asynchronous via signed provider webhooks and strictly
// idempotent.
package payment

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection is the MongoDB collection storing payments.
const Collection = "payments"

// Payment purposes.
const (
	PurposeOrder = "order"
	// PurposeRide paie une COURSE. Distinct de `order` : ce n'est pas la même
	// verticale qu'il faut prévenir quand le prestataire confirme, et confondre
	// les deux enverrait la confirmation d'une course à la livraison — qui ne
	// connaît aucune course et la refuserait.
	PurposeRide          = "ride"
	PurposeTokenPurchase = "token_purchase"
	// PurposeWalletTopup recharge le portefeuille d'ARGENT d'un client. Le
	// crédit n'a lieu qu'à la CONFIRMATION du prestataire : créditer sur la
	// réponse d'une initiation reviendrait à offrir l'argent.
	PurposeWalletTopup = "wallet_topup"
)

// Payment statuses.
const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	// StatusRefunded est posé par la RÉSOLUTION D'UN LITIGE, dans la
	// verticale : c'est elle qui juge qu'une commande doit être remboursée.
	//
	// ⚠️ Ce statut décrit le registre de la plateforme, PAS le prestataire.
	// Rendre l'argent chez l'opérateur mobile reste un geste manuel ; marquer
	// ici sans le faire là-bas laisserait un client remboursé sur l'écran et
	// pas sur son téléphone.
	StatusRefunded = "refunded"
)

// DefaultCurrency is the platform currency (smallest unit, integer amounts).
const DefaultCurrency = "XOF"

// Meta keys used for token purchases.
const (
	MetaWalletOwnerID = "wallet_owner_id"
	MetaTokens        = "tokens"
)

// Payment is one payment attempt tracked from initiation to provider
// confirmation.
type Payment struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	UserID   primitive.ObjectID `bson:"user_id"`
	Purpose  string             `bson:"purpose"` // "order" | "token_purchase"
	RefID    primitive.ObjectID `bson:"ref_id"`  // order id or wallet owner id
	Provider string             `bson:"provider"`
	Amount   int                `bson:"amount"`   // smallest currency unit
	Currency string             `bson:"currency"` // "XOF"
	Status   string             `bson:"status"`   // "pending" | "succeeded" | "failed"
	// ProviderRef is the provider-side identifier; unique index guarantees
	// webhook idempotency.
	ProviderRef string `bson:"provider_ref"`
	// Meta carries purpose-specific context, e.g. wallet_owner_id and tokens
	// count for token purchases.
	Meta      map[string]any `bson:"meta,omitempty"`
	CreatedAt time.Time      `bson:"created_at"`
	UpdatedAt time.Time      `bson:"updated_at"`
}
