package user

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Ce fichier sert l'ADMINISTRATION DES COMPTES : chercher une personne,
// l'ouvrir, la suspendre.
//
// Elle vit au socle parce qu'un compte est un compte : la console des repas et
// celle des courses cherchent la même personne, dans la même collection. La
// laisser dans une verticale aurait donné deux écrans d'administration pour un
// seul annuaire — et une suspension qui ne vaut que d'un côté.
//
// ⚠️ Ce que le socle ne compose PAS : le solde, le profil de livreur, les
// véhicules, le nombre de commandes. Ces objets appartiennent aux verticales,
// et la fiche complète d'un livreur se compose donc chez elles, à partir de ce
// que ce fichier rend.

var errAccountNotFound = apperr.NotFound("user_not_found", "user not found")

// AccountRow is one account as an administration screen reads it.
type AccountRow struct {
	ID    string             `bson:"-" json:"id"`
	OID   primitive.ObjectID `bson:"_id" json:"-"`
	Role  string             `bson:"role" json:"role"`
	Phone string             `bson:"phone" json:"phone"`
	Name  string             `bson:"name" json:"name"`
	Email string             `bson:"email,omitempty" json:"email,omitempty"`
	// AvatarURL : la photo du compte. La console la montre sur chaque
	// ligne et chaque fiche — sans elle ici, une photo posée (PATCH
	// /admin/users/{id}) restait invisible partout où l'on lit des comptes.
	AvatarURL string    `bson:"avatar_url,omitempty" json:"avatar_url,omitempty"`
	Status    string    `bson:"status" json:"status"`
	Country   string    `bson:"country,omitempty" json:"country,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

func (a *AccountRow) fill() { a.ID = a.OID.Hex() }

// AccountFilter narrows an account search.
type AccountFilter struct {
	Role   string
	Status string
	Query  string // nom ou téléphone, sous-chaîne insensible à la casse
}

// AudienceIDs rend les identifiants des comptes ACTIFS de ces rôles, après
// `after`, par ordre d'identifiant — la page suivante d'une campagne.
//
// Des identifiants seulement : une campagne à dix mille personnes n'a pas
// besoin de dix mille fiches en mémoire, elle a besoin de savoir à qui
// écrire, dans un ordre stable qu'on peut reprendre.
func (r *Repository) AudienceIDs(ctx context.Context, roles []string, after primitive.ObjectID, limit int) ([]primitive.ObjectID, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	// Bornée au PAYS de la requête : une campagne se publie depuis une
	// console qui regarde un pays, et « tous les clients » veut dire ceux de
	// ce pays.
	filter := country.Restrict(ctx, bson.M{"role": bson.M{"$in": roles}, "status": StatusActive})
	if !after.IsZero() {
		filter["_id"] = bson.M{"$gt": after}
	}
	cur, err := r.users.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)).SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, fmt.Errorf("user: audience ids: %w", err)
	}
	var rows []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("user: audience ids: %w", err)
	}
	out := make([]primitive.ObjectID, 0, len(rows))
	for _, x := range rows {
		out = append(out, x.ID)
	}
	return out, nil
}

// SearchAccountIDs rend les identifiants des comptes dont le nom ou le
// téléphone contient `q` — la sous-chaîne que quelqu'un tape, échappée —
// parmi des rôles, dans le pays de la requête. Des identifiants seulement,
// bornés à `limit` (200 au plus) : c'est ce qu'un filtre `$in` d'une
// verticale sait consommer.
func (r *Repository) SearchAccountIDs(ctx context.Context, q string, roles []string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	pattern := primitive.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
	// Un téléphone se tape avec des espaces (« 90 20 00 01 ») et se stocke
	// sans : les chiffres seuls cherchent aussi la forme compacte.
	digits := strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, q)
	or := bson.A{bson.M{"name": pattern}, bson.M{"phone": pattern}}
	if len(digits) >= 3 {
		or = append(or, bson.M{"phone": primitive.Regex{Pattern: regexp.QuoteMeta(digits)}})
	}
	filter := country.Restrict(ctx, bson.M{"$or": or})
	if len(roles) > 0 {
		filter["role"] = bson.M{"$in": roles}
	}
	cur, err := r.users.Find(ctx, filter, options.Find().SetLimit(int64(limit)).SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	var rows []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]string, 0, len(rows))
	for _, x := range rows {
		out = append(out, x.ID.Hex())
	}
	return out, nil
}

// ListAccounts pages accounts, oldest first, with optional filters.
//
// ⚠️ Le tri est ASCENDANT — l'inverse des autres listes. C'est celui d'un
// annuaire : on cherche une personne, pas les dernières inscriptions.
func (r *Repository) ListAccounts(ctx context.Context, f AccountFilter, cursor string, limit int) ([]AccountRow, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// ⚠️ La borne pays, ici comme dans toute liste d'exploitation : la
	// console de Lomé ne cherche pas parmi les comptes de Cotonou.
	filter := country.Restrict(ctx, bson.M{})
	if f.Role != "" {
		filter["role"] = f.Role
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Query != "" {
		// QuoteMeta : une recherche est un texte que quelqu'un tape, pas une
		// expression régulière. Sans cet échappement, un « ( » suffit à faire
		// échouer la requête, et un motif coûteux à la faire ramer.
		q := primitive.Regex{Pattern: regexp.QuoteMeta(f.Query), Options: "i"}
		filter["$or"] = bson.A{bson.M{"name": q}, bson.M{"phone": q}}
	}
	if cursor != "" {
		cid, err := primitive.ObjectIDFromHex(cursor)
		if err == nil {
			filter["_id"] = bson.M{"$gt": cid}
		}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit + 1))
	cur, err := r.users.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var rows []AccountRow
	if err := cur.All(ctx, &rows); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[limit-1].OID.Hex()
	}
	for i := range rows {
		rows[i].fill()
	}
	return rows, next, nil
}

// AccountByID loads one account.
func (r *Repository) AccountByID(ctx context.Context, id string) (*AccountRow, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errAccountNotFound.WithCause(err)
	}
	var row AccountRow
	if err := r.users.FindOne(ctx, bson.M{"_id": oid}).Decode(&row); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errAccountNotFound
		}
		return nil, apperr.Internal(err)
	}
	row.fill()
	return &row, nil
}

// AccountsByIDs loads several accounts in ONE query, for the listings a
// vertical decorates with an identity.
//
// ⚠️ Borné comme `UserNames` : une liste d'identifiants sans limite est une
// lecture de toute la table déguisée en résolution de noms.
func (r *Repository) AccountsByIDs(ctx context.Context, ids []string) ([]AccountRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		// Un identifiant illisible est IGNORÉ, pas refusé : la liste sert à
		// décorer un affichage, et une ligne sans nom vaut mieux qu'un écran
		// vide parce qu'une donnée ancienne est mal formée.
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			oids = append(oids, oid)
		}
	}
	cur, err := r.users.Find(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	var rows []AccountRow
	if err := cur.All(ctx, &rows); err != nil {
		return nil, apperr.Internal(err)
	}
	for i := range rows {
		rows[i].fill()
	}
	return rows, nil
}

// SetAccountStatus activates or suspends an account and returns the state
// BEFORE the change, so the caller can audit what actually moved.
func (r *Repository) SetAccountStatus(ctx context.Context, id, status string) (*AccountRow, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errAccountNotFound.WithCause(err)
	}
	var before AccountRow
	err = r.users.FindOneAndUpdate(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"status": status, "updated_at": time.Now().UTC()}},
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&before)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errAccountNotFound
		}
		return nil, apperr.Internal(err)
	}
	before.fill()
	return &before, nil
}
