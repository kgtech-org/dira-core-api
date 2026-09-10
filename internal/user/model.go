// Package user manages accounts, authentication (phone + password, JWT
// access/refresh) and profiles for the four platform roles.
package user

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection names.
const (
	usersCollection         = "users"
	refreshTokensCollection = "refresh_tokens"

	// Exportées pour le PROVISIONNEMENT (`cmd/seed`), qui doit pouvoir les
	// vider — et pour lui seul. Les nommer ailleurs ferait un second écrivain
	// sur les comptes.
	CollectionUsers         = usersCollection
	CollectionRefreshTokens = refreshTokensCollection
)

// Account statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// User is a platform account. The phone number (E.164) is the primary
// identifier. PasswordHash is an encoded argon2id string and must never leave
// the service layer.
type User struct {
	ID    primitive.ObjectID `bson:"_id,omitempty"`
	Role  string             `bson:"role"`  // "client" | "driver" | "merchant" | "admin"
	Phone string             `bson:"phone"` // E.164, unique
	// Name est le nom d'AFFICHAGE. Il reste la source unique de vérité pour
	// tout ce qui affiche un nom : les prénom/nom séparés servent les
	// formulaires d'état civil, pas les écrans.
	Name string `bson:"name"`
	// FirstName et LastName sont facultatifs, et le resteront : une bonne
	// partie des utilisateurs s'inscrit avec un seul mot. Les DÉDUIRE en
	// coupant `name` à l'espace produirait des « Nom : Diallo Ndiaye » chez
	// tous ceux qui ont deux prénoms.
	FirstName string     `bson:"first_name,omitempty"`
	LastName  string     `bson:"last_name,omitempty"`
	BirthDate *time.Time `bson:"birth_date,omitempty"`
	Gender    string     `bson:"gender,omitempty"`
	Email     string     `bson:"email,omitempty"`
	AvatarURL string     `bson:"avatar_url,omitempty"`
	// Preferences : notifications par catégorie, thème, langue, sons.
	Preferences  *Preferences `bson:"preferences,omitempty"`
	PasswordHash string       `bson:"password_hash"`
	Status       string       `bson:"status"` // "active" | "suspended"
	CreatedAt    time.Time    `bson:"created_at"`
	UpdatedAt    time.Time    `bson:"updated_at"`
}

// RefreshToken stores the sha256 hash of an issued refresh token. Rotation
// deletes the old hash and inserts the new one; a TTL index on expires_at
// purges expired entries.
type RefreshToken struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	TokenHash string             `bson:"token_hash"` // sha256 hex, unique
	UserID    primitive.ObjectID `bson:"user_id"`
	ExpiresAt time.Time          `bson:"expires_at"`
	CreatedAt time.Time          `bson:"created_at"`
}
