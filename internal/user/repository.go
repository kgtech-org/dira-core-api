package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ErrDuplicatePhone is returned when a user already exists with the same
// phone number (unique index on phone). Fakes must reproduce this behaviour.
var ErrDuplicatePhone = errors.New("user: phone already registered")

// Repository implements Repo on MongoDB.
type Repository struct {
	users         *mongo.Collection
	refreshTokens *mongo.Collection
	addresses     *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		users:         m.Collection(usersCollection),
		refreshTokens: m.Collection(refreshTokensCollection),
		addresses:     m.Collection(CollectionAddresses),
	}
}

// CreateUser inserts a new user. Returns ErrDuplicatePhone when the phone
// number is already registered (unique index on phone).
func (r *Repository) CreateUser(ctx context.Context, u *User) error {
	res, err := r.users.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicatePhone
		}
		return fmt.Errorf("user: create: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		u.ID = id
	}
	return nil
}

// FindByPhone returns the user with the given phone, or (nil, nil) when none
// exists.
func (r *Repository) FindByPhone(ctx context.Context, phone string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"phone": phone}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by phone: %w", err)
	}
	return &u, nil
}

// FindByEmail returns the user with the given email (stored lowercase), or
// (nil, nil) when none exists.
func (r *Repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"email": strings.ToLower(email)}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by email: %w", err)
	}
	return &u, nil
}

// FindByID returns the user with the given id, or (nil, nil) when none exists.
func (r *Repository) FindByID(ctx context.Context, id primitive.ObjectID) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by id: %w", err)
	}
	return &u, nil
}

// UpdateUser replaces the mutable fields of a user document.
func (r *Repository) UpdateUser(ctx context.Context, u *User) error {
	// ⚠️ Cette liste doit couvrir TOUT ce que le service sait modifier.
	// `avatar_url` y manquait : le service posait la nouvelle photo en
	// mémoire, la réponse la montrait, et rien n'était écrit — l'utilisateur
	// la voyait changer puis revenir au rechargement suivant. Puis `phone` et
	// `password_hash`, le jour où la console a su corriger un compte : un
	// mot de passe « remplacé » en 200 avec lequel personne ne pouvait se
	// connecter. Les tests du service passent par un dépôt en mémoire et ne
	// voient pas cette liste — c'est un parcours réel qui l'a montré.
	res, err := r.users.UpdateOne(ctx, bson.M{"_id": u.ID}, bson.M{"$set": bson.M{
		"name":          u.Name,
		"first_name":    u.FirstName,
		"last_name":     u.LastName,
		"birth_date":    u.BirthDate,
		"gender":        u.Gender,
		"email":         u.Email,
		"phone":         u.Phone,
		"password_hash": u.PasswordHash,
		"avatar_url":    u.AvatarURL,
		"preferences":   u.Preferences,
		"status":        u.Status,
		"updated_at":    u.UpdatedAt,
	}})
	if err != nil {
		// Le téléphone est unique : le donner à un compte qui l'a déjà est
		// le même conflit qu'à l'inscription, et se lit pareil (409).
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicatePhone
		}
		return fmt.Errorf("user: update: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("user: update: user %s not found", u.ID.Hex())
	}
	return nil
}

// DeleteUser removes a user document (compensation when wallet creation
// fails during driver registration).
func (r *Repository) DeleteUser(ctx context.Context, id primitive.ObjectID) error {
	if _, err := r.users.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return fmt.Errorf("user: delete: %w", err)
	}
	return nil
}

// InsertRefreshToken stores the hash of an issued refresh token.
func (r *Repository) InsertRefreshToken(ctx context.Context, t *RefreshToken) error {
	res, err := r.refreshTokens.InsertOne(ctx, t)
	if err != nil {
		return fmt.Errorf("user: insert refresh token: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		t.ID = id
	}
	return nil
}

// DeleteRefreshTokenByHash removes a stored refresh token hash and reports
// whether it existed (false means already rotated, revoked or never issued).
func (r *Repository) DeleteRefreshTokenByHash(ctx context.Context, tokenHash string) (bool, error) {
	res, err := r.refreshTokens.DeleteOne(ctx, bson.M{"token_hash": tokenHash})
	if err != nil {
		return false, fmt.Errorf("user: delete refresh token: %w", err)
	}
	return res.DeletedCount > 0, nil
}

// FindNamesByIDs resolves several display names in ONE query. Un commentaire
// signé d'un identifiant hexadécimal ne se lit pas, et résoudre chaque auteur
// séparément ferait une requête par ligne.
func (r *Repository) FindNamesByIDs(ctx context.Context, ids []primitive.ObjectID) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cur, err := r.users.Find(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"name": 1}),
	)
	if err != nil {
		return nil, fmt.Errorf("user: names: %w", err)
	}
	var rows []struct {
		ID   primitive.ObjectID `bson:"_id"`
		Name string             `bson:"name"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("user: decode names: %w", err)
	}
	for _, row := range rows {
		out[row.ID.Hex()] = row.Name
	}
	return out, nil
}
