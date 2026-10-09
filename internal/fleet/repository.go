package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository is the MongoDB access for private fleets.
type Repository struct {
	fleets *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{fleets: m.Collection(Collection)}
}

// Create inserts a fleet.
//
// ⚠️ Le doublon de NOM est détecté par l'index unique, pas par une lecture
// préalable. Deux enregistrements concurrents de la même société — ce qui
// arrive quand deux exploitants traitent le même contrat — passeraient tous
// les deux un contrôle « ce nom existe-t-il ? », et la base porterait deux
// flottes que rien ne distingue.
func (r *Repository) Create(ctx context.Context, f *Fleet) error {
	now := time.Now().UTC()
	f.CreatedAt, f.UpdatedAt = now, now
	res, err := r.fleets.InsertOne(ctx, f)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return errNameTaken
		}
		return fmt.Errorf("fleet: create: %w", err)
	}
	f.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

// ByID returns a fleet, or nil when the id matches nothing.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Fleet, error) {
	var f Fleet
	err := r.fleets.FindOne(ctx, bson.M{"_id": id}).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fleet: by id: %w", err)
	}
	return &f, nil
}

// ByIDs resolves several fleets in ONE query, for the lists a vertical
// decorates with an owner name.
// ByOwner retrouve la flotte d'un compte gérant.
//
// ⚠️ UNE SEULE FLOTTE PAR COMPTE, par construction de la console partenaire :
// le menu, les listes et les écritures partent tous de « sa » flotte. Si un
// jour quelqu'un gère deux sociétés, c'est ICI que ça se verra — et il faudra
// alors un sélecteur, pas un second compte.
//
// ⚠️ ET LA LECTURE N'EST PAS BORNÉE PAR PAYS, délibérément : c'est le COMPTE
// CONNECTÉ qui la désigne, pas une liste d'exploitation. Un partenaire togolais
// dont le contrat est enregistré depuis Dakar doit pouvoir se connecter — le
// pays de la requête n'a rien à voir avec la propriété de sa flotte.
func (r *Repository) ByOwner(ctx context.Context, ownerUserID primitive.ObjectID) (*Fleet, error) {
	var f Fleet
	err := r.fleets.FindOne(ctx, bson.M{"owner_user_id": ownerUserID}).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fleet: by owner: %w", err)
	}
	return &f, nil
}

// ByMember retrouve la flotte d'un compte — PROPRIÉTAIRE OU MEMBRE DU
// PERSONNEL.
//
// ⚠️ UNE SEULE LECTURE POUR LES DEUX CAS (`$or`) : chercher d'abord par
// propriétaire puis par membre aurait fait deux allers-retours sur la route la
// plus appelée de la console partenaire — elle est demandée à CHAQUE requête.
//
// ⚠️ ET PAS DE BORNE PAYS, pour la même raison que `ByOwner` : c'est le COMPTE
// CONNECTÉ qui désigne la flotte, pas une liste d'exploitation.
func (r *Repository) ByMember(ctx context.Context, userID primitive.ObjectID) (*Fleet, error) {
	var f Fleet
	err := r.fleets.FindOne(ctx, memberFilter(userID)).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fleet: by member: %w", err)
	}
	return &f, nil
}

// AddMember ajoute un compte au personnel.
//
// ⚠️ `$addToSet` NE SUFFIRAIT PAS : il compare le document ENTIER, donc le même
// compte avec un autre rôle passerait — et la flotte porterait deux lignes pour
// une personne, dont une seule serait lue. Le doublon est refusé par le SERVICE,
// qui a déjà la flotte en main ; ici on ajoute.
func (r *Repository) AddMember(ctx context.Context, id primitive.ObjectID, m Member) error {
	_, err := r.fleets.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$push": bson.M{"members": m}, "$set": bson.M{"updated_at": time.Now().UTC()}})
	if err != nil {
		return fmt.Errorf("fleet: add member: %w", err)
	}
	return nil
}

// RemoveMember détache un compte du personnel. Rend `false` quand il n'y était
// pas — ce que le service traduit en « inconnu » plutôt qu'en succès muet.
func (r *Repository) RemoveMember(ctx context.Context, id, userID primitive.ObjectID) (bool, error) {
	res, err := r.fleets.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$pull": bson.M{"members": bson.M{"user_id": userID}},
			"$set": bson.M{"updated_at": time.Now().UTC()}})
	if err != nil {
		return false, fmt.Errorf("fleet: remove member: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

func (r *Repository) ByIDs(ctx context.Context, ids []primitive.ObjectID) (map[primitive.ObjectID]Fleet, error) {
	out := make(map[primitive.ObjectID]Fleet, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cur, err := r.fleets.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("fleet: by ids: %w", err)
	}
	var fleets []Fleet
	if err := cur.All(ctx, &fleets); err != nil {
		return nil, fmt.Errorf("fleet: decode by ids: %w", err)
	}
	for _, f := range fleets {
		out[f.ID] = f
	}
	return out, nil
}

// List returns fleets, newest first, optionally filtered by status or a
// case-insensitive name search.
func (r *Repository) List(ctx context.Context, status, q string, cursor primitive.ObjectID, limit int) ([]Fleet, error) {
	filter := country.Restrict(ctx, bson.M{})
	if status != "" {
		filter["status"] = status
	}
	if q != "" {
		filter["name"] = bson.M{"$regex": primitive.Regex{Pattern: regexEscape(q), Options: "i"}}
	}
	if !cursor.IsZero() {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.fleets.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("fleet: list: %w", err)
	}
	var fleets []Fleet
	if err := cur.All(ctx, &fleets); err != nil {
		return nil, fmt.Errorf("fleet: decode list: %w", err)
	}
	return fleets, nil
}

// Update applies the given fields and returns the fleet as stored.
func (r *Repository) Update(ctx context.Context, id primitive.ObjectID, set bson.M) (*Fleet, error) {
	set["updated_at"] = time.Now().UTC()
	var f Fleet
	err := r.fleets.FindOneAndUpdate(ctx,
		bson.M{"_id": id},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, errNameTaken
		}
		return nil, fmt.Errorf("fleet: update: %w", err)
	}
	return &f, nil
}

// regexEscape neutralise ce qu'un utilisateur tape dans une recherche.
//
// ⚠️ Sans cela, une recherche contenant `(` ou `*` fait échouer la requête —
// et une recherche `.*` parcourt la collection entière. Un champ de recherche
// est une entrée non fiable comme une autre.
func regexEscape(s string) string {
	const special = `\.+*?()|[]{}^$`
	out := make([]rune, 0, len(s)*2)
	for _, r := range s {
		for _, sp := range special {
			if r == sp {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, r)
	}
	return string(out)
}
