package token

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Ce fichier sert les LECTURES du back-office : la liste des portefeuilles et
// le grand livre des mouvements.
//
// ⚠️ Ces lignes sortent BRUTES — un `owner_id`, pas un nom. Le socle ne sait
// pas ce qu'est un point de vente ; il stocke un identifiant opaque. C'est la
// verticale qui possède l'objet désigné, et c'est donc elle qui le nomme.
// Faire autrement obligerait le socle à connaître les collections de chaque
// verticale, et l'ajout d'une verticale à modifier le socle.

// WalletRow is one wallet, as the back-office reads it.
type WalletRow struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Type      string    `json:"type"`
	Balance   int       `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LedgerRow is one token movement.
type LedgerRow struct {
	ID        string    `json:"id"`
	WalletID  string    `json:"wallet_id"`
	Kind      string    `json:"kind"`
	Reason    string    `json:"reason"`
	Amount    int       `json:"amount"`
	RefID     string    `json:"ref_id,omitempty"`
	RefKind   string    `json:"ref_kind,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// pageLimit borne ce qu'une verticale peut demander d'un coup. Une page non
// bornée transformerait une vue d'administration en export complet de la
// table des mouvements.
func pageLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

// beforeCursor traduit un curseur opaque en borne sur `_id`. Un curseur
// ILLISIBLE est ignoré plutôt que refusé : il rend la première page, ce qui
// est le comportement attendu d'une pagination qu'on relance.
func beforeCursor(filter bson.M, cursor string) bson.M {
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": cid}
		}
	}
	return filter
}

// ListWallets pages wallets, newest first, filtered by type and/or owner.
//
// Le filtre par PROPRIÉTAIRE sert la fiche d'une personne : c'est un champ sur
// une requête existante plutôt qu'une route « solde de quelqu'un » de plus.
// Une surface de service se garde étroite en n'y ajoutant que ce qu'on ne peut
// pas exprimer avec ce qui existe.
func (r *Repository) ListWallets(ctx context.Context, walletType, ownerID, cursor string, limit int) ([]WalletRow, string, error) {
	limit = pageLimit(limit)
	filter := bson.M{}
	if walletType != "" {
		filter["type"] = walletType
	}
	if ownerID != "" {
		oid, err := primitive.ObjectIDFromHex(ownerID)
		if err != nil {
			return nil, "", apperr.Validation("invalid owner id").WithCause(err)
		}
		filter["owner_id"] = oid
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit + 1))
	cur, err := r.wallets.Find(ctx, beforeCursor(filter, cursor), opts)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var docs []struct {
		ID        primitive.ObjectID `bson:"_id"`
		OwnerID   primitive.ObjectID `bson:"owner_id"`
		Type      string             `bson:"type"`
		Balance   int                `bson:"balance"`
		UpdatedAt time.Time          `bson:"updated_at"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(docs) > limit {
		docs = docs[:limit]
		next = docs[limit-1].ID.Hex()
	}
	out := make([]WalletRow, 0, len(docs))
	for _, d := range docs {
		out = append(out, WalletRow{
			ID: d.ID.Hex(), OwnerID: d.OwnerID.Hex(), Type: d.Type,
			Balance: d.Balance, UpdatedAt: d.UpdatedAt,
		})
	}
	return out, next, nil
}

// ListLedger pages token movements, newest first, optionally for one wallet.
func (r *Repository) ListLedger(ctx context.Context, walletID, cursor string, limit int) ([]LedgerRow, string, error) {
	limit = pageLimit(limit)
	filter := bson.M{}
	if walletID != "" {
		wid, err := primitive.ObjectIDFromHex(walletID)
		if err != nil {
			return nil, "", apperr.Validation("invalid wallet id").WithCause(err)
		}
		filter["wallet_id"] = wid
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit + 1))
	cur, err := r.transactions.Find(ctx, beforeCursor(filter, cursor), opts)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var docs []struct {
		ID        primitive.ObjectID  `bson:"_id"`
		WalletID  primitive.ObjectID  `bson:"wallet_id"`
		Kind      string              `bson:"kind"`
		Reason    string              `bson:"reason"`
		Amount    int                 `bson:"amount"`
		RefID     *primitive.ObjectID `bson:"ref_id,omitempty"`
		RefKind   string              `bson:"ref_kind,omitempty"`
		CreatedAt time.Time           `bson:"created_at"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(docs) > limit {
		docs = docs[:limit]
		next = docs[limit-1].ID.Hex()
	}
	out := make([]LedgerRow, 0, len(docs))
	for _, d := range docs {
		row := LedgerRow{
			ID: d.ID.Hex(), WalletID: d.WalletID.Hex(), Kind: d.Kind,
			Reason: d.Reason, Amount: d.Amount, CreatedAt: d.CreatedAt,
		}
		row.RefKind = d.RefKind
		if d.RefID != nil {
			row.RefID = d.RefID.Hex()
		}
		out = append(out, row)
	}
	return out, next, nil
}
