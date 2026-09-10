package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

var (
	errAddressNotFound = apperr.NotFound("address_not_found", "address not found")
	errTooManyAddress  = apperr.Conflict("too_many_addresses",
		fmt.Sprintf("an account keeps at most %d addresses", MaxAddresses))
)

// --- dépôt ---

// ListAddresses rend le carnet d'un compte, l'adresse par défaut en tête.
//
// En tête et non par ordre de création : c'est celle que l'application
// présélectionne, et la faire chercher dans la liste serait absurde.
func (r *Repository) ListAddresses(ctx context.Context, userID primitive.ObjectID) ([]Address, error) {
	opts := options.Find().SetSort(bson.D{
		{Key: "is_default", Value: -1},
		{Key: "created_at", Value: 1},
	})
	cur, err := r.addresses.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("user: list addresses: %w", err)
	}
	var out []Address
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("user: decode addresses: %w", err)
	}
	return out, nil
}

// CountAddresses compte le carnet.
func (r *Repository) CountAddresses(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	n, err := r.addresses.CountDocuments(ctx, bson.M{"user_id": userID})
	if err != nil {
		return 0, fmt.Errorf("user: count addresses: %w", err)
	}
	return n, nil
}

// FindAddress rend une adresse DU COMPTE, ou nil.
//
// L'identifiant du compte est dans le FILTRE et non vérifié après lecture :
// une adresse d'autrui ne doit pas pouvoir être lue, même le temps d'un
// contrôle.
func (r *Repository) FindAddress(ctx context.Context, userID, id primitive.ObjectID) (*Address, error) {
	var a Address
	err := r.addresses.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user: find address: %w", err)
	}
	return &a, nil
}

// SaveAddress insère ou remplace une adresse.
func (r *Repository) SaveAddress(ctx context.Context, a *Address) error {
	if a.ID.IsZero() {
		a.ID = primitive.NewObjectID()
		if _, err := r.addresses.InsertOne(ctx, a); err != nil {
			return fmt.Errorf("user: insert address: %w", err)
		}
		return nil
	}
	_, err := r.addresses.ReplaceOne(ctx, bson.M{"_id": a.ID, "user_id": a.UserID}, a)
	if err != nil {
		return fmt.Errorf("user: replace address: %w", err)
	}
	return nil
}

// DeleteAddress retire une adresse du carnet.
func (r *Repository) DeleteAddress(ctx context.Context, userID, id primitive.ObjectID) (bool, error) {
	res, err := r.addresses.DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	if err != nil {
		return false, fmt.Errorf("user: delete address: %w", err)
	}
	return res.DeletedCount > 0, nil
}

// ClearDefaultAddress retire le drapeau « par défaut » de toutes les autres.
//
// UNE seule adresse par défaut, et c'est la base qui le garantit à chaque
// pose : deux enregistrements simultanés franchiraient tous les deux une
// vérification applicative, et l'application en présélectionnerait une au
// hasard.
func (r *Repository) ClearDefaultAddress(ctx context.Context, userID, except primitive.ObjectID) error {
	_, err := r.addresses.UpdateMany(ctx,
		bson.M{"user_id": userID, "_id": bson.M{"$ne": except}, "is_default": true},
		bson.M{"$set": bson.M{"is_default": false, "updated_at": time.Now().UTC()}})
	if err != nil {
		return fmt.Errorf("user: clear default address: %w", err)
	}
	return nil
}

// --- service ---

// ListAddresses rend le carnet d'adresses du compte.
func (s *Service) ListAddresses(ctx context.Context, userID string) ([]AddressResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	items, err := s.repo.ListAddresses(ctx, uid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]AddressResponse, 0, len(items))
	for i := range items {
		out = append(out, newAddressResponse(&items[i]))
	}
	return out, nil
}

// SaveAddress ajoute une adresse, ou remplace celle dont l'identifiant est
// donné.
func (s *Service) SaveAddress(ctx context.Context, userID, addressID string, req AddressRequest) (*AddressResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	now := time.Now().UTC()
	a := &Address{UserID: uid, CreatedAt: now}
	if addressID != "" {
		aid, err := primitive.ObjectIDFromHex(addressID)
		if err != nil {
			return nil, errAddressNotFound.WithCause(err)
		}
		existing, err := s.repo.FindAddress(ctx, uid, aid)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if existing == nil {
			return nil, errAddressNotFound
		}
		a = existing
	} else {
		n, err := s.repo.CountAddresses(ctx, uid)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if n >= MaxAddresses {
			return nil, errTooManyAddress
		}
		// La PREMIÈRE adresse est celle par défaut, sans qu'on le demande :
		// un carnet d'une seule adresse dont aucune n'est présélectionnée
		// n'aurait aucun sens.
		if n == 0 {
			req.IsDefault = true
		}
	}
	a.Label, a.Address, a.Geo, a.Details = req.Label, req.Address, req.Geo, req.Details
	a.IsDefault = req.IsDefault
	a.UpdatedAt = now
	if err := s.repo.SaveAddress(ctx, a); err != nil {
		return nil, apperr.Internal(err)
	}
	if a.IsDefault {
		if err := s.repo.ClearDefaultAddress(ctx, uid, a.ID); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	resp := newAddressResponse(a)
	return &resp, nil
}

// DeleteAddress retire une adresse du carnet.
//
// Supprimer l'adresse PAR DÉFAUT en promeut une autre : laisser un carnet non
// vide sans présélection ferait retomber le client sur une saisie manuelle
// alors qu'il a des adresses enregistrées.
func (s *Service) DeleteAddress(ctx context.Context, userID, addressID string) error {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return apperr.Validation("invalid user id").WithCause(err)
	}
	aid, err := primitive.ObjectIDFromHex(addressID)
	if err != nil {
		return errAddressNotFound.WithCause(err)
	}
	existing, err := s.repo.FindAddress(ctx, uid, aid)
	if err != nil {
		return apperr.Internal(err)
	}
	if existing == nil {
		return errAddressNotFound
	}
	ok, err := s.repo.DeleteAddress(ctx, uid, aid)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errAddressNotFound
	}
	if !existing.IsDefault {
		return nil
	}
	rest, err := s.repo.ListAddresses(ctx, uid)
	if err != nil || len(rest) == 0 {
		return nil
	}
	promoted := rest[0]
	promoted.IsDefault = true
	promoted.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveAddress(ctx, &promoted); err != nil {
		return apperr.Internal(err)
	}
	return nil
}
