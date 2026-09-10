package staff

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository is the MongoDB access for staff records.
type Repository struct {
	members *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{members: m.Collection(Collection)}
}

// Create inserts a staff record.
//
// ⚠️ Le doublon est refusé par l'index unique sur `user_id`, pas par une
// lecture préalable : deux fiches pour la même personne décideraient de ses
// droits selon celle qu'on lit en premier, et deux créations concurrentes
// passeraient toutes deux un contrôle applicatif.
func (r *Repository) Create(ctx context.Context, m *Member) error {
	now := time.Now().UTC()
	m.CreatedAt, m.UpdatedAt = now, now
	res, err := r.members.InsertOne(ctx, m)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return errAlreadyStaff
		}
		return fmt.Errorf("staff: create: %w", err)
	}
	m.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

// ByUserID returns the staff record of an account, or nil.
//
// C'est la lecture du CHEMIN DE CONNEXION : elle décide des portées inscrites
// dans le jeton, donc elle est faite à chaque ouverture de session.
func (r *Repository) ByUserID(ctx context.Context, userID primitive.ObjectID) (*Member, error) {
	var m Member
	err := r.members.FindOne(ctx, bson.M{"user_id": userID}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("staff: by user: %w", err)
	}
	return &m, nil
}

// ByID returns one staff record, or nil.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Member, error) {
	var m Member
	err := r.members.FindOne(ctx, bson.M{"_id": id}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("staff: by id: %w", err)
	}
	return &m, nil
}

// List returns staff records, newest first, optionally filtered.
func (r *Repository) List(ctx context.Context, function, scope, status string, cursor primitive.ObjectID, limit int) ([]Member, error) {
	filter := bson.M{}
	if function != "" {
		filter["function"] = function
	}
	if status != "" {
		filter["status"] = status
	}
	if scope != "" {
		// ⚠️ Un membre SANS portée déclarée couvre TOUTES les verticales — il
		// doit donc apparaître quand on filtre sur l'une d'elles. L'oublier
		// ferait disparaître les administrateurs les plus puissants de la
		// liste « qui s'occupe des courses ? », qui est précisément celle où
		// l'on veut les voir.
		filter["$or"] = []bson.M{
			{"scopes": scope},
			{"scopes": bson.M{"$exists": false}},
			{"scopes": bson.M{"$size": 0}},
		}
	}
	if !cursor.IsZero() {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.members.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("staff: list: %w", err)
	}
	var members []Member
	if err := cur.All(ctx, &members); err != nil {
		return nil, fmt.Errorf("staff: decode list: %w", err)
	}
	return members, nil
}

// Update applies the given fields and returns the record as stored.
func (r *Repository) Update(ctx context.Context, id primitive.ObjectID, set bson.M) (*Member, error) {
	set["updated_at"] = time.Now().UTC()
	var m Member
	err := r.members.FindOneAndUpdate(ctx,
		bson.M{"_id": id}, bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("staff: update: %w", err)
	}
	return &m, nil
}

// Delete removes a staff record.
//
// ⚠️ Retire la FICHE, pas le COMPTE. Une personne qui quitte l'équipe garde
// son compte — ses actions passées restent attribuées dans le journal d'audit,
// et supprimer le compte ferait apparaître « utilisateur inconnu » à côté de
// chaque geste qu'elle a posé.
func (r *Repository) Delete(ctx context.Context, id primitive.ObjectID) (bool, error) {
	res, err := r.members.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return false, fmt.Errorf("staff: delete: %w", err)
	}
	return res.DeletedCount > 0, nil
}
